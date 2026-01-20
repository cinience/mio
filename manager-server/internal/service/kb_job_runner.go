package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"manager-server/internal/kb/ingest"
	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
	"manager-server/internal/vectorstore"
)

const (
	defaultChunkMaxChars       = 800
	defaultChunkOverlap        = 120
	defaultRetryBackoffSeconds = 30
)

type ingestionPayload struct {
	DocumentID uint64         `json:"documentId"`
	RawContent string         `json:"rawContent,omitempty"`
	SourceType string         `json:"sourceType,omitempty"`
	Connector  string         `json:"connector,omitempty"`
	OriginID   string         `json:"originId,omitempty"`
	StorageURI string         `json:"storageUri,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Chunker splits raw content into chunk drafts.
type Chunker interface {
	Chunk(doc *models.KBDocument, raw string) ([]ChunkDraft, error)
}

// simpleChunker implements a naive paragraph-based chunker.
type simpleChunker struct {
	maxChars int
	overlap  int
}

// newSimpleChunker creates a chunker using sensible defaults.
func newSimpleChunker(maxChars, overlap int) Chunker {
	return &simpleChunker{
		maxChars: maxChars,
		overlap:  overlap,
	}
}

func (c *simpleChunker) Chunk(doc *models.KBDocument, raw string) ([]ChunkDraft, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("raw content is empty")
	}
	segments := paragraphSplit(raw)
	if len(segments) == 0 {
		return nil, errors.New("no content segments produced")
	}

	var drafts []ChunkDraft
	var current strings.Builder
	last := ""
	index := 0

	appendDraft := func(text string) {
		chunkKey := fmt.Sprintf("%d-%d", doc.ID, index)
		index++
		drafts = append(drafts, ChunkDraft{
			DocumentID: doc.ID,
			ChunkKey:   chunkKey,
			Content:    strings.TrimSpace(text),
			Metadata: map[string]any{
				"source":     doc.SourceType,
				"connector":  doc.Source,
				"originId":   doc.OriginID,
				"documentId": doc.ID,
			},
		})
	}

	for _, segment := range segments {
		if current.Len() == 0 {
			current.WriteString(segment)
			last = segment
			continue
		}
		if current.Len()+len(segment) <= c.maxChars {
			current.WriteString("\n\n")
			current.WriteString(segment)
			last = segment
			continue
		}

		appendDraft(current.String())

		current.Reset()
		if c.overlap > 0 && last != "" {
			trimmed := last
			if len(trimmed) > c.overlap {
				trimmed = trimmed[len(trimmed)-c.overlap:]
			}
			current.WriteString(trimmed)
			current.WriteString("\n\n")
		}
		current.WriteString(segment)
		last = segment
	}
	if current.Len() > 0 {
		appendDraft(current.String())
	}

	return drafts, nil
}

func paragraphSplit(raw string) []string {
	lines := strings.Split(raw, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// KBJobRunner processes knowledge base ingestion jobs asynchronously.
type KBJobRunner struct {
	repo        repository.KBRepository
	vectorStore vectorstore.Adapter
	chunker     Chunker
	storage     storage.DocumentStorage
	pipeline    ingest.DocumentPipeline
	defaults    ingest.Config
	retryConfig IngestionRetryConfig

	queue chan uint64
	wg    sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}

	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    bool

	events KBEventSink
}

// NewKBJobRunner constructs a new ingestion runner.
func NewKBJobRunner(repo repository.KBRepository, storage storage.DocumentStorage, pipeline ingest.DocumentPipeline, adapter vectorstore.Adapter, events KBEventSink, defaults ingest.Config, retry IngestionRetryConfig) *KBJobRunner {
	if defaults.ChunkSize <= 0 {
		defaults.ChunkSize = defaultChunkMaxChars
	}
	if defaults.ChunkOverlap < 0 {
		defaults.ChunkOverlap = defaultChunkOverlap
	}
	runner := &KBJobRunner{
		repo:        repo,
		vectorStore: adapter,
		chunker:     newSimpleChunker(defaults.ChunkSize, defaults.ChunkOverlap),
		storage:     storage,
		pipeline:    pipeline,
		defaults:    defaults,
		retryConfig: retry,
		queue:       make(chan uint64, 64),
		stopCh:      make(chan struct{}),
		events:      events,
	}
	runner.pauseCond = sync.NewCond(&runner.pauseMu)
	return runner
}

// WithChunker overrides the default chunker implementation.
func (r *KBJobRunner) WithChunker(chunker Chunker) *KBJobRunner {
	if chunker != nil {
		r.chunker = chunker
	}
	return r
}

// Start launches worker goroutines. Safe to call multiple times.
func (r *KBJobRunner) Start(workerCount int) {
	if workerCount <= 0 {
		workerCount = 1
	}
	r.startOnce.Do(func() {
		for i := 0; i < workerCount; i++ {
			r.wg.Add(1)
			go r.worker()
		}
	})
}

// Stop gracefully stops all workers.
func (r *KBJobRunner) Stop() {
	r.stopOnce.Do(func() {
		r.Resume()
		close(r.stopCh)
	})
	r.wg.Wait()
}

// Enqueue schedules a job id for processing.
func (r *KBJobRunner) Enqueue(ctx context.Context, jobID uint64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.stopCh:
		return errors.New("job runner stopped")
	case r.queue <- jobID:
		return nil
	}
}

// Pause temporarily halts job processing.
func (r *KBJobRunner) Pause() {
	r.pauseMu.Lock()
	r.paused = true
	r.pauseMu.Unlock()
}

// Resume continues job processing after a pause.
func (r *KBJobRunner) Resume() {
	r.pauseMu.Lock()
	if r.paused {
		r.paused = false
		r.pauseCond.Broadcast()
	}
	r.pauseMu.Unlock()
}

// IsPaused reports whether the runner is currently paused.
func (r *KBJobRunner) IsPaused() bool {
	r.pauseMu.Lock()
	paused := r.paused
	r.pauseMu.Unlock()
	return paused
}

func (r *KBJobRunner) waitIfPaused() {
	r.pauseMu.Lock()
	for r.paused {
		r.pauseCond.Wait()
	}
	r.pauseMu.Unlock()
}

func (r *KBJobRunner) worker() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stopCh:
			return
		case jobID := <-r.queue:
			r.waitIfPaused()
			if err := r.processJob(context.Background(), jobID); err != nil {
				// Best-effort logging; fallback to fmt since logger not injected.
				fmt.Printf("[kb_job_runner] job %d process error: %v\n", jobID, err)
			}
		}
	}
}

func (r *KBJobRunner) processJob(ctx context.Context, jobID uint64) error {
	job, err := r.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("fetch job %d: %w", jobID, err)
	}

	if job.JobType != "ingestion" {
		job.Status = "skipped"
		now := time.Now()
		job.CompletedAt = &now
		if err := r.repo.UpdateJob(ctx, job); err != nil {
			return fmt.Errorf("mark job skipped: %w", err)
		}
		r.emitJobEvent(ctx, job)
		return nil
	}

	var payload ingestionPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			if updateErr := r.failJob(ctx, job, fmt.Errorf("parse payload: %w", err)); updateErr != nil {
				return updateErr
			}
			return nil
		}
	}
	if payload.DocumentID == 0 && job.DocumentID != nil {
		payload.DocumentID = *job.DocumentID
	}
	if payload.DocumentID == 0 {
		if updateErr := r.failJob(ctx, job, errors.New("missing documentId in payload")); updateErr != nil {
			return updateErr
		}
		return nil
	}
	doc, err := r.repo.GetDocumentByID(ctx, payload.DocumentID)
	if err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("fetch document: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}
	kb, err := r.repo.GetKnowledgeBaseByID(ctx, job.KnowledgeBaseID)
	if err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("fetch knowledge base: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}

	if payload.SourceType == "" {
		payload.SourceType = doc.SourceType
	}
	if payload.Connector == "" {
		payload.Connector = doc.Source
	}
	if payload.OriginID == "" {
		payload.OriginID = doc.OriginID
	}
	if payload.StorageURI == "" {
		payload.StorageURI = doc.StorageURI
	}

	doc.ParseStatus = "parsing"
	doc.ParseError = ""
	if err := r.repo.UpdateDocument(ctx, doc); err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("update document status: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}

	job.Status = "parsing"
	job.Progress = 10
	now := time.Now()
	job.StartedAt = &now
	job.RetryCount++
	if err := r.repo.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}
	r.emitJobEvent(ctx, job)

	logger.Infof("msg=kb_job_started job_id=%d kb_id=%d doc_id=%d source=%s connector=%s", job.ID, job.KnowledgeBaseID, doc.ID, payload.SourceType, payload.Connector)

	var drafts []ChunkDraft
	if strings.TrimSpace(payload.RawContent) != "" {
		job.Status = "ingesting"
		job.Progress = 45
		if err := r.repo.UpdateJob(ctx, job); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
		r.emitJobEvent(ctx, job)
		drafts, err = r.chunker.Chunk(doc, payload.RawContent)
	} else {
		sourceURI, err := r.resolveSourceURI(ctx, doc, payload.StorageURI)
		if err != nil {
			if updateErr := r.failJob(ctx, job, fmt.Errorf("resolve storage uri: %w", err)); updateErr != nil {
				return updateErr
			}
			return nil
		}
		job.Status = "staging"
		job.Progress = 35
		if err := r.repo.UpdateJob(ctx, job); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
		r.emitJobEvent(ctx, job)
		job.Status = "ingesting"
		job.Progress = 55
		if err := r.repo.UpdateJob(ctx, job); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
		r.emitJobEvent(ctx, job)
		cfg := r.chunkConfig(kb)
		drafts, err = r.generateChunksFromPipeline(ctx, doc, sourceURI, cfg)
	}
	if err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("chunk content: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}

	job.Status = "indexing"
	job.Progress = 70
	if err := r.repo.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}
	r.emitJobEvent(ctx, job)

	if err := r.persistChunks(ctx, kb, doc, drafts); err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("persist chunks: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}

	doc.ParseStatus = "completed"
	doc.ParseError = ""
	if err := r.repo.UpdateDocument(ctx, doc); err != nil {
		if updateErr := r.failJob(ctx, job, fmt.Errorf("update document complete: %w", err)); updateErr != nil {
			return updateErr
		}
		return nil
	}

	job.Status = "completed"
	job.Progress = 100
	completed := time.Now()
	job.CompletedAt = &completed
	if err := r.repo.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("finalize job: %w", err)
	}
	r.emitJobEvent(ctx, job)

	logger.Infof("msg=kb_job_completed job_id=%d kb_id=%d doc_id=%d connector=%s chunks=%d", job.ID, job.KnowledgeBaseID, doc.ID, payload.Connector, len(drafts))

	r.notifyProjectAgents(ctx, kb.ProjectID)

	return nil
}

func (r *KBJobRunner) resolveSourceURI(ctx context.Context, doc *models.KBDocument, uri string) (string, error) {
	if strings.TrimSpace(uri) == "" {
		return "", fmt.Errorf("document %d storage uri missing", doc.ID)
	}
	if strings.HasPrefix(uri, storage.SchemeLocal) {
		if r.storage == nil {
			return "", errors.New("local storage not configured")
		}
		path, err := r.storage.ResolvePath(uri)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return uri, nil
}

func (r *KBJobRunner) generateChunksFromPipeline(ctx context.Context, doc *models.KBDocument, uri string, cfg ingest.Config) ([]ChunkDraft, error) {
	if r.pipeline == nil {
		return nil, errors.New("ingestion pipeline not configured")
	}
	docs, err := r.pipeline.Process(ctx, uri, cfg)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, errors.New("pipeline produced no chunks")
	}
	drafts := make([]ChunkDraft, 0, len(docs))
	for idx, chunkDoc := range docs {
		if chunkDoc == nil {
			continue
		}
		content := strings.TrimSpace(chunkDoc.Content)
		if content == "" {
			continue
		}
		metadata := map[string]any{
			"sourceType": doc.SourceType,
			"storageUri": doc.StorageURI,
			"chunkIndex": idx,
			"connector":  doc.Source,
			"originId":   doc.OriginID,
		}
		if chunkDoc.MetaData != nil {
			for key, value := range chunkDoc.MetaData {
				metadata[key] = value
			}
		}
		chunkKey := fmt.Sprintf("%d-%d", doc.ID, idx)
		drafts = append(drafts, ChunkDraft{
			DocumentID: doc.ID,
			ChunkKey:   chunkKey,
			Content:    content,
			Metadata:   metadata,
		})
	}
	if len(drafts) == 0 {
		return nil, errors.New("no valid chunks produced")
	}
	return drafts, nil
}

func (r *KBJobRunner) chunkConfig(kb *models.KBKnowledgeBase) ingest.Config {
	cfg := r.defaults
	if kb == nil || len(kb.EmbeddingParams) == 0 {
		return cfg
	}
	var params map[string]any
	if err := json.Unmarshal(kb.EmbeddingParams, &params); err != nil {
		return cfg
	}
	if size, ok := getNumber(params["chunkSize"]); ok && size > 0 {
		cfg.ChunkSize = int(size)
	}
	if overlap, ok := getNumber(params["chunkOverlap"]); ok && overlap >= 0 {
		cfg.ChunkOverlap = int(overlap)
	}
	return cfg
}

func getNumber(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	case json.Number:
		if f, err := val.Float64(); err == nil {
			return f, true
		}
	case string:
		if strings.TrimSpace(val) == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func (r *KBJobRunner) persistChunks(ctx context.Context, kb *models.KBKnowledgeBase, doc *models.KBDocument, drafts []ChunkDraft) error {
	if len(drafts) == 0 {
		return errors.New("no drafts supplied")
	}

	modelChunks := make([]*models.KBChunk, 0, len(drafts))
	for _, draft := range drafts {
		meta := map[string]any{
			"documentId":      doc.ID,
			"documentTitle":   doc.Title,
			"storageUri":      doc.StorageURI,
			"sourceType":      doc.SourceType,
			"connector":       doc.Source,
			"originId":        doc.OriginID,
			"knowledgeBaseId": kb.ID,
		}
		for k, v := range draft.Metadata {
			meta[k] = v
		}
		modelChunk := &models.KBChunk{
			DocumentID: draft.DocumentID,
			ChunkKey:   draft.ChunkKey,
			Content:    draft.Content,
			ManualEdit: draft.ManualEdit,
			QAQuestion: draft.QAQuestion,
			QAAnswer:   draft.QAAnswer,
		}
		jsonValue, err := mapToJSON(meta)
		if err != nil {
			return err
		}
		modelChunk.Metadata = jsonValue
		modelChunks = append(modelChunks, modelChunk)
	}

	if err := r.repo.CreateChunks(ctx, modelChunks); err != nil {
		return err
	}

	if r.vectorStore != nil {
		if err := r.vectorStore.IndexChunks(ctx, kb.ID, modelChunks); err != nil {
			return fmt.Errorf("index chunks: %w", err)
		}
	}

	return nil
}

func (r *KBJobRunner) failJob(ctx context.Context, job *models.KBJob, failure error) error {
	if job == nil {
		return nil
	}
	if failure == nil {
		failure = errors.New("ingestion failure")
	}
	msg := failure.Error()
	shouldRetry := r.shouldRetry(job)
	status := "failed"
	var completedAt *time.Time
	if shouldRetry {
		status = "retrying"
	} else {
		now := time.Now()
		completedAt = &now
	}
	job.Status = status
	job.ErrorMessage = msg
	job.ErrorType = categorizeIngestionError(failure)
	job.CompletedAt = completedAt
	var retryDelay time.Duration
	if !shouldRetry {
		job.Progress = 0
		job.NextRetryAt = nil
	} else {
		retryDelay = r.retryDelay(job.RetryCount)
		next := time.Now().Add(retryDelay)
		job.NextRetryAt = &next
	}
	if err := r.repo.UpdateJob(ctx, job); err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	r.emitJobEvent(ctx, job)
	if job.DocumentID != nil {
		doc, err := r.repo.GetDocumentByID(ctx, *job.DocumentID)
		if err == nil {
			if shouldRetry {
				doc.ParseStatus = "queued"
			} else {
				doc.ParseStatus = "failed"
			}
			doc.ParseError = msg
			_ = r.repo.UpdateDocument(ctx, doc)
		}
	}
	if shouldRetry {
		logger.Warnf("msg=kb_job_retrying job_id=%d attempt=%d max=%d delay=%s err=%v", job.ID, job.RetryCount, r.resolveMaxRetries(job), retryDelay, failure)
		r.enqueueRetry(job.ID, retryDelay)
	} else {
		logger.Warnf("msg=kb_job_failed job_id=%d attempt=%d err=%v", job.ID, job.RetryCount, failure)
	}
	return nil
}

func (r *KBJobRunner) notifyProjectAgents(ctx context.Context, projectID string) {
	if r.events == nil {
		return
	}
	mounts, err := r.repo.ListProjectAgentMounts(ctx, projectID)
	if err != nil {
		logger.Warnf("project_id=%s err=%v msg=kb_job_notify_mounts_failed", projectID, err)
		return
	}
	if len(mounts) == 0 {
		return
	}

	seen := make(map[uint64]struct{})
	agentIDs := make([]uint64, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		if _, ok := seen[mount.AgentID]; ok {
			continue
		}
		seen[mount.AgentID] = struct{}{}
		agentIDs = append(agentIDs, mount.AgentID)
	}
	if len(agentIDs) == 0 {
		return
	}
	r.events.AgentsCapabilitiesChanged(ctx, agentIDs)
}

func (r *KBJobRunner) emitJobEvent(ctx context.Context, job *models.KBJob) {
	if r.events == nil || job == nil {
		return
	}
	r.events.JobStatusChanged(ctx, job)
}

func categorizeIngestionError(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "permission"):
		return "auth"
	case strings.Contains(msg, "not found"), strings.Contains(msg, "missing"):
		return "not_found"
	case strings.Contains(msg, "network"), strings.Contains(msg, "connection"), strings.Contains(msg, "dial"):
		return "network"
	default:
		return "internal"
	}
}

func (r *KBJobRunner) resolveMaxRetries(job *models.KBJob) int {
	if job == nil {
		return 0
	}
	if job.MaxRetries > 0 {
		return job.MaxRetries
	}
	if r.retryConfig.MaxAttempts > 0 {
		return r.retryConfig.MaxAttempts
	}
	return 0
}

func (r *KBJobRunner) shouldRetry(job *models.KBJob) bool {
	maxRetries := r.resolveMaxRetries(job)
	if maxRetries <= 0 {
		return false
	}
	return job.RetryCount < maxRetries
}

func (r *KBJobRunner) retryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	if len(r.retryConfig.BackoffSeconds) == 0 {
		return time.Duration(defaultRetryBackoffSeconds) * time.Second
	}
	idx := attempt - 1
	if idx >= len(r.retryConfig.BackoffSeconds) {
		idx = len(r.retryConfig.BackoffSeconds) - 1
	}
	seconds := r.retryConfig.BackoffSeconds[idx]
	if seconds <= 0 {
		seconds = defaultRetryBackoffSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (r *KBJobRunner) enqueueRetry(jobID uint64, delay time.Duration) {
	if delay <= 0 {
		delay = time.Duration(defaultRetryBackoffSeconds) * time.Second
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-r.stopCh:
			return
		case <-timer.C:
		}
		ctx := context.Background()
		job, err := r.repo.GetJobByID(ctx, jobID)
		if err != nil {
			logger.Warnf("msg=kb_job_retry_fetch_failed job_id=%d err=%v", jobID, err)
			return
		}
		job.Status = "queued"
		job.Progress = 0
		job.StartedAt = nil
		job.CompletedAt = nil
		job.NextRetryAt = nil
		if err := r.repo.UpdateJob(ctx, job); err != nil {
			logger.Warnf("msg=kb_job_retry_update_failed job_id=%d err=%v", jobID, err)
			return
		}
		r.emitJobEvent(ctx, job)
		if job.DocumentID != nil {
			if doc, err := r.repo.GetDocumentByID(ctx, *job.DocumentID); err == nil {
				doc.ParseStatus = "queued"
				_ = r.repo.UpdateDocument(ctx, doc)
			}
		}
		retryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.Enqueue(retryCtx, jobID); err != nil {
			logger.Warnf("msg=kb_job_retry_enqueue_failed job_id=%d err=%v", jobID, err)
		}
	}()
}
