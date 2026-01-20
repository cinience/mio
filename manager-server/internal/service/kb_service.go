package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"

	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
	"manager-server/internal/vectorstore"
)

const (
	// Knowledge base permission roles.
	KBRoleAdmin  = "admin"
	KBRoleEditor = "editor"
	KBRoleViewer = "viewer"
)

var kbValidRoles = map[string]struct{}{
	KBRoleAdmin:  {},
	KBRoleEditor: {},
	KBRoleViewer: {},
}

// KBEventSink publishes capability changes to interested systems.
type KBEventSink interface {
	AgentsCapabilitiesChanged(ctx context.Context, agentIDs []uint64)
	JobStatusChanged(ctx context.Context, job *models.KBJob)
}

// KBJobDispatcher is implemented by the asynchronous job runner.
type KBJobDispatcher interface {
	Enqueue(ctx context.Context, jobID uint64) error
	Pause()
	Resume()
	IsPaused() bool
}

// ActorContext captures the authenticated caller details used for authorization checks.
type ActorContext struct {
	UserID  uint64
	IsAdmin bool
}

// CreateProjectInput wraps the data required to create a knowledge base project.
type CreateProjectInput struct {
	Name       string
	Visibility string
	Metadata   map[string]any
}

// UpdateProjectInput wraps updateable fields for a project.
type UpdateProjectInput struct {
	Name       *string
	Visibility *string
	Metadata   map[string]any
}

// CreateKnowledgeBaseInput wraps the configuration for a new knowledge base.
type CreateKnowledgeBaseInput struct {
	ProjectID         string
	Name              string
	Description       string
	EmbeddingModel    string
	EmbeddingParams   map[string]any
	RetrievalStrategy string
	RetrievalParams   map[string]any
}

// UpdateKnowledgeBaseInput represents partial updates to a knowledge base.
type UpdateKnowledgeBaseInput struct {
	Name              *string
	Description       *string
	EmbeddingModel    *string
	EmbeddingParams   map[string]any
	RetrievalStrategy *string
	RetrievalParams   map[string]any
	Status            *string
	LastSyncedAt      *time.Time
}

// UploadDocumentInput represents a file payload for knowledge base ingestion.
type UploadDocumentInput struct {
	FileName    string
	Size        int64
	ContentType string
	Reader      io.ReadCloser
}

// SubmitDocumentURLInput captures a remote document reference to ingest.
type SubmitDocumentURLInput struct {
	KnowledgeBaseID uint64
	URL             string
	Title           string
	ContentType     string
	Metadata        map[string]any
}

// CreateDocumentInput defines payload for new documents.
type CreateDocumentInput struct {
	KnowledgeBaseID  uint64
	Title            string
	SourceType       string
	Source           string
	OriginID         string
	Connector        string
	StorageURI       string
	Checksum         string
	SizeBytes        int64
	Metadata         map[string]any
	EnqueueIngestion bool
	RawContent       string
}

// UpdateDocumentInput represents document updates.
type UpdateDocumentInput struct {
	Title       *string
	StorageURI  *string
	Checksum    *string
	SizeBytes   *int64
	ParseStatus *string
	ParseError  *string
	Metadata    map[string]any
}

// ChunkDraft contains chunk creation payload.
type ChunkDraft struct {
	DocumentID uint64
	ChunkKey   string
	Content    string
	Metadata   map[string]any
	ManualEdit bool
	QAQuestion string
	QAAnswer   string
}

// UpdateChunkInput represents mutable fields on KBChunk.
type UpdateChunkInput struct {
	Content           *string
	Metadata          map[string]any
	ManualEdit        *bool
	QAQuestion        *string
	QAAnswer          *string
	EmbeddingVectorID *string
}

// JobStatusUpdateInput captures job status changes during ingestion.
type JobStatusUpdateInput struct {
	Status       *string
	Progress     *int
	ErrorMessage *string
	Payload      map[string]any
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

// PermissionGrantInput represents a permission upsert request.
type PermissionGrantInput struct {
	KnowledgeBaseID uint64
	UserID          uint64
	Role            string
}

// AgentProjectMountInput represents agent mount creation.
type AgentProjectMountInput struct {
	AgentID      uint64
	ProjectID    string
	Capabilities map[string]any
}

// InternalQueryOptions carries query options for backend-server calls.
type InternalQueryOptions struct {
	Query          string
	TopK           int
	ScoreThreshold float64
	RerankModel    string
	Filters        map[string]any
	TimeoutMs      int
	Debug          bool
}

// KBService coordinates business logic across projects, knowledge bases, documents, and mounts.
type KBService struct {
	repo        repository.KBRepository
	jobRunner   KBJobDispatcher
	vectorStore vectorstore.Adapter
	storage     storage.DocumentStorage
	maxUpload   int64
	formats     map[string]struct{}
	events      KBEventSink
	quotaConfig IngestionQuotaConfig
	retryConfig IngestionRetryConfig
}

// NewKBService constructs a KBService.
func NewKBService(
	repo repository.KBRepository,
	jobRunner KBJobDispatcher,
	adapter vectorstore.Adapter,
	storage storage.DocumentStorage,
	events KBEventSink,
	maxUpload int64,
	allowedFormats []string,
	quota IngestionQuotaConfig,
	retry IngestionRetryConfig,
) *KBService {
	formatSet := make(map[string]struct{}, len(allowedFormats))
	for _, ext := range allowedFormats {
		normalized := normalizeExtension(ext)
		if normalized != "" {
			formatSet[normalized] = struct{}{}
		}
	}
	if maxUpload < 0 {
		maxUpload = 0
	}
	return &KBService{
		repo:        repo,
		jobRunner:   jobRunner,
		vectorStore: adapter,
		storage:     storage,
		maxUpload:   maxUpload,
		formats:     formatSet,
		events:      events,
		quotaConfig: quota,
		retryConfig: retry,
	}
}

// CreateProject creates a project owned by the actor.
func (s *KBService) CreateProject(ctx context.Context, actor ActorContext, input CreateProjectInput) (*models.KBProject, error) {
	if actor.UserID == 0 {
		return nil, errors.New("missing actor userID")
	}
	if err := validateProjectName(input.Name); err != nil {
		return nil, err
	}
	visibility := normalizeVisibility(input.Visibility)
	project := &models.KBProject{
		OwnerID:    actor.UserID,
		Name:       input.Name,
		Visibility: visibility,
	}
	if input.Metadata != nil {
		jsonValue, err := mapToJSON(input.Metadata)
		if err != nil {
			return nil, err
		}
		project.Metadata = jsonValue
	}
	if err := s.repo.CreateProject(ctx, project); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

// UpdateProject mutates project fields if the actor is authorized.
func (s *KBService) UpdateProject(ctx context.Context, actor ActorContext, projectID string, input UpdateProjectInput) (*models.KBProject, error) {
	project, err := s.repo.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return nil, err
	}
	if input.Name != nil {
		if err := validateProjectName(*input.Name); err != nil {
			return nil, err
		}
		project.Name = *input.Name
	}
	if input.Visibility != nil {
		project.Visibility = normalizeVisibility(*input.Visibility)
	}
	if input.Metadata != nil {
		jsonValue, err := mapToJSON(input.Metadata)
		if err != nil {
			return nil, err
		}
		project.Metadata = jsonValue
	}
	if err := s.repo.UpdateProject(ctx, project); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return project, nil
}

// DeleteProject soft deletes a project when the actor has rights.
func (s *KBService) DeleteProject(ctx context.Context, actor ActorContext, projectID string) error {
	project, err := s.repo.GetProjectByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	if err := s.repo.SoftDeleteProject(ctx, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, projectID)
	return nil
}

// ListProjects returns paginated projects filtered and authorized for the actor.
func (s *KBService) ListProjects(ctx context.Context, actor ActorContext, filter repository.KBProjectFilter, limit, offset int) ([]*models.KBProject, int64, error) {
	if !actor.IsAdmin {
		filter.OwnerID = &actor.UserID
	}
	projects, total, err := s.repo.ListProjects(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	return projects, total, nil
}

// GetProject retrieves a project after enforcing read permissions.
func (s *KBService) GetProject(ctx context.Context, actor ActorContext, projectID string) (*models.KBProject, error) {
	project, err := s.repo.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectReadAccess(actor, project); err != nil {
		return nil, err
	}
	return project, nil
}

// CreateKnowledgeBase creates a knowledge base under a project if the actor has access.
func (s *KBService) CreateKnowledgeBase(ctx context.Context, actor ActorContext, input CreateKnowledgeBaseInput) (*models.KBKnowledgeBase, error) {
	project, err := s.repo.GetProjectByID(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return nil, err
	}
	if err := validateKnowledgeBaseName(input.Name); err != nil {
		return nil, err
	}
	kb := &models.KBKnowledgeBase{
		ProjectID:         input.ProjectID,
		Name:              input.Name,
		Description:       input.Description,
		EmbeddingModel:    input.EmbeddingModel,
		RetrievalStrategy: defaultRetrievalStrategy(input.RetrievalStrategy),
	}
	if input.EmbeddingParams != nil {
		jsonValue, err := mapToJSON(input.EmbeddingParams)
		if err != nil {
			return nil, err
		}
		kb.EmbeddingParams = jsonValue
	}
	if input.RetrievalParams != nil {
		jsonValue, err := mapToJSON(input.RetrievalParams)
		if err != nil {
			return nil, err
		}
		kb.RetrievalParams = jsonValue
	}
	if err := s.repo.CreateKnowledgeBase(ctx, kb); err != nil {
		return nil, fmt.Errorf("create knowledge base: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return kb, nil
}

// UpdateKnowledgeBase mutates knowledge base fields after permission checks.
func (s *KBService) UpdateKnowledgeBase(ctx context.Context, actor ActorContext, kbID uint64, input UpdateKnowledgeBaseInput) (*models.KBKnowledgeBase, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return nil, err
	}
	if input.Name != nil {
		if err := validateKnowledgeBaseName(*input.Name); err != nil {
			return nil, err
		}
		kb.Name = *input.Name
	}
	if input.Description != nil {
		kb.Description = *input.Description
	}
	if input.EmbeddingModel != nil {
		kb.EmbeddingModel = *input.EmbeddingModel
	}
	if input.EmbeddingParams != nil {
		jsonValue, err := mapToJSON(input.EmbeddingParams)
		if err != nil {
			return nil, err
		}
		kb.EmbeddingParams = jsonValue
	}
	if input.RetrievalStrategy != nil {
		kb.RetrievalStrategy = defaultRetrievalStrategy(*input.RetrievalStrategy)
	}
	if input.RetrievalParams != nil {
		jsonValue, err := mapToJSON(input.RetrievalParams)
		if err != nil {
			return nil, err
		}
		kb.RetrievalParams = jsonValue
	}
	if input.Status != nil {
		kb.Status = *input.Status
	}
	if input.LastSyncedAt != nil {
		kb.LastSyncedAt = input.LastSyncedAt
	}
	if err := s.repo.UpdateKnowledgeBase(ctx, kb); err != nil {
		return nil, fmt.Errorf("update knowledge base: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return kb, nil
}

// ArchiveKnowledgeBase sets the knowledge base status to archived.
func (s *KBService) ArchiveKnowledgeBase(ctx context.Context, actor ActorContext, kbID uint64) error {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return fmt.Errorf("fetch knowledge base: %w", err)
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	if err := s.repo.ArchiveKnowledgeBase(ctx, kbID, "archived"); err != nil {
		return fmt.Errorf("archive knowledge base: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, project.ID)
	return nil
}

// ListKnowledgeBases returns knowledge bases filtered for the actor.
func (s *KBService) ListKnowledgeBases(ctx context.Context, actor ActorContext, filter repository.KBKnowledgeBaseFilter, limit, offset int) ([]*models.KBKnowledgeBase, int64, error) {
	if !actor.IsAdmin {
		if strings.TrimSpace(filter.ProjectID) == "" {
			return nil, 0, errors.New("projectId is required for non-admin users")
		}
		project, err := s.repo.GetProjectByID(ctx, filter.ProjectID)
		if err != nil {
			return nil, 0, fmt.Errorf("fetch project: %w", err)
		}
		if err := s.ensureProjectReadAccess(actor, project); err != nil {
			return nil, 0, err
		}
	}
	kbs, total, err := s.repo.ListKnowledgeBases(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list knowledge bases: %w", err)
	}
	return kbs, total, nil
}

// GetKnowledgeBase fetches a single knowledge base after enforcing access.
func (s *KBService) GetKnowledgeBase(ctx context.Context, actor ActorContext, kbID uint64) (*models.KBKnowledgeBase, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	return kb, nil
}

// CreateDocument stores metadata and optionally enqueues ingestion.
func (s *KBService) CreateDocument(ctx context.Context, actor ActorContext, input CreateDocumentInput) (*models.KBDocument, *models.KBJob, error) {
	if input.KnowledgeBaseID == 0 {
		return nil, nil, errors.New("knowledgeBaseId is required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	return s.createDocumentInternal(ctx, actor, kb, input)
}

// UpdateDocument mutates document metadata or statuses.
func (s *KBService) UpdateDocument(ctx context.Context, actor ActorContext, documentID uint64, input UpdateDocumentInput) (*models.KBDocument, error) {
	doc, err := s.repo.GetDocumentByID(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("fetch document: %w", err)
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, doc.KnowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	if input.Title != nil {
		if strings.TrimSpace(*input.Title) == "" {
			return nil, errors.New("title cannot be empty")
		}
		doc.Title = *input.Title
	}
	if input.StorageURI != nil {
		doc.StorageURI = *input.StorageURI
	}
	if input.Checksum != nil {
		doc.Checksum = *input.Checksum
	}
	if input.SizeBytes != nil {
		doc.SizeBytes = *input.SizeBytes
	}
	if input.ParseStatus != nil {
		doc.ParseStatus = *input.ParseStatus
	}
	if input.ParseError != nil {
		doc.ParseError = *input.ParseError
	}
	if input.Metadata != nil {
		jsonValue, err := mapToJSON(input.Metadata)
		if err != nil {
			return nil, err
		}
		doc.Metadata = jsonValue
	}
	if err := s.repo.UpdateDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return doc, nil
}

// ListDocuments returns documents visible to the actor.
func (s *KBService) ListDocuments(ctx context.Context, actor ActorContext, kbID uint64, filter repository.KBDocumentFilter, limit, offset int) ([]*models.KBDocument, int64, error) {
	if filter.KnowledgeBaseID == 0 {
		filter.KnowledgeBaseID = kbID
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, filter.KnowledgeBaseID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, 0, err
	}
	docs, total, err := s.repo.ListDocuments(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list documents: %w", err)
	}
	return docs, total, nil
}

// CreateChunks persists chunk drafts for a document after validating access.
func (s *KBService) CreateChunks(ctx context.Context, actor ActorContext, knowledgeBaseID uint64, drafts []ChunkDraft) error {
	if len(drafts) == 0 {
		return errors.New("no chunks supplied")
	}
	doc, err := s.repo.GetDocumentByID(ctx, drafts[0].DocumentID)
	if err != nil {
		return fmt.Errorf("fetch document: %w", err)
	}
	if doc.KnowledgeBaseID != knowledgeBaseID {
		return errors.New("document does not belong to knowledge base")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return err
	}
	chunks := make([]*models.KBChunk, 0, len(drafts))
	for _, draft := range drafts {
		if draft.DocumentID != doc.ID {
			return errors.New("mixed document IDs in chunk drafts")
		}
		if strings.TrimSpace(draft.Content) == "" {
			return errors.New("chunk content cannot be empty")
		}
		meta := map[string]any{
			"documentId":      doc.ID,
			"documentTitle":   doc.Title,
			"storageUri":      doc.StorageURI,
			"sourceType":      doc.SourceType,
			"knowledgeBaseId": doc.KnowledgeBaseID,
		}
		for k, v := range draft.Metadata {
			meta[k] = v
		}
		chunk := &models.KBChunk{
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
		chunk.Metadata = jsonValue
		chunks = append(chunks, chunk)
	}
	if err := s.repo.CreateChunks(ctx, chunks); err != nil {
		return fmt.Errorf("create chunks: %w", err)
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return nil
}

// UpdateChunk applies modifications to a chunk when authorized.
func (s *KBService) UpdateChunk(ctx context.Context, actor ActorContext, chunkID uint64, input UpdateChunkInput) (*models.KBChunk, error) {
	chunk, err := s.repo.GetChunkByID(ctx, chunkID)
	if err != nil {
		return nil, fmt.Errorf("fetch chunk: %w", err)
	}
	doc, err := s.repo.GetDocumentByID(ctx, chunk.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("fetch document: %w", err)
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, doc.KnowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	if input.Content != nil {
		chunk.Content = *input.Content
	}
	if input.Metadata != nil {
		jsonValue, err := mapToJSON(input.Metadata)
		if err != nil {
			return nil, err
		}
		chunk.Metadata = jsonValue
	}
	if input.ManualEdit != nil {
		chunk.ManualEdit = *input.ManualEdit
	}
	if input.QAQuestion != nil {
		chunk.QAQuestion = *input.QAQuestion
	}
	if input.QAAnswer != nil {
		chunk.QAAnswer = *input.QAAnswer
	}
	if input.EmbeddingVectorID != nil {
		chunk.EmbeddingVectorID = *input.EmbeddingVectorID
	}
	if err := s.repo.UpdateChunk(ctx, chunk); err != nil {
		return nil, fmt.Errorf("update chunk: %w", err)
	}
	if s.vectorStore != nil {
		if err := s.vectorStore.IndexChunks(ctx, kb.ID, []*models.KBChunk{chunk}); err != nil {
			logger.Warnf("chunk reindex failed: %v", err)
		}
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return chunk, nil
}

// ListChunks returns chunks for a document when the actor has read access.
func (s *KBService) ListChunks(ctx context.Context, actor ActorContext, knowledgeBaseID uint64, filter repository.KBChunkFilter, limit, offset int) ([]*models.KBChunk, int64, error) {
	doc, err := s.repo.GetDocumentByID(ctx, filter.DocumentID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch document: %w", err)
	}
	if doc.KnowledgeBaseID != knowledgeBaseID {
		return nil, 0, errors.New("document does not belong to knowledge base")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, 0, err
	}
	chunks, total, err := s.repo.ListChunks(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list chunks: %w", err)
	}
	return chunks, total, nil
}

// EnqueueJob creates a KB job record and schedules asynchronous processing.
func (s *KBService) EnqueueJob(ctx context.Context, actor ActorContext, kbID uint64, jobType string, payload map[string]any, documentID *uint64) (*models.KBJob, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	maxRetries := s.retryConfig.MaxAttempts
	if maxRetries <= 0 {
		maxRetries = 1
	}
	job := &models.KBJob{
		KnowledgeBaseID: kbID,
		JobType:         jobType,
		Status:          "queued",
		MaxRetries:      maxRetries,
	}
	if documentID != nil {
		job.DocumentID = documentID
	}
	if payload != nil {
		jsonValue, err := mapToJSON(payload)
		if err != nil {
			return nil, err
		}
		job.Payload = jsonValue
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	s.emitJobStatus(ctx, job)
	if s.jobRunner != nil {
		if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
			return nil, fmt.Errorf("enqueue job: %w", err)
		}
	}
	return job, nil
}

// RetryJob resets a failed ingestion job and re-enqueues it for processing.
func (s *KBService) RetryJob(ctx context.Context, actor ActorContext, kbID, jobID uint64, force bool) (*models.KBJob, error) {
	if kbID == 0 {
		return nil, errors.New("knowledgeBaseId is required")
	}
	if jobID == 0 {
		return nil, errors.New("jobId is required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	if job.KnowledgeBaseID != kb.ID {
		return nil, errors.New("job does not belong to knowledge base")
	}
	status := strings.ToLower(stringsTrim(job.Status))
	if status != "failed" && status != "skipped" {
		return nil, errors.New("仅支持重试失败的任务")
	}
	if !force && job.MaxRetries > 0 && job.RetryCount >= job.MaxRetries {
		return nil, fmt.Errorf("重试次数已达上限 (%d)", job.MaxRetries)
	}

	job.Status = "queued"
	job.Progress = 0
	job.ErrorMessage = ""
	job.ErrorType = ""
	job.StartedAt = nil
	job.CompletedAt = nil
	job.NextRetryAt = nil
	if err := s.repo.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("更新任务状态失败: %w", err)
	}
	s.emitJobStatus(ctx, job)

	if job.DocumentID != nil {
		if doc, err := s.repo.GetDocumentByID(ctx, *job.DocumentID); err == nil {
			doc.ParseStatus = "queued"
			doc.ParseError = ""
			_ = s.repo.UpdateDocument(ctx, doc)
		}
	}

	if s.jobRunner != nil {
		if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
			return nil, fmt.Errorf("重试任务入队失败: %w", err)
		}
	}

	return job, nil
}

// UpdateJob mutates job progress or payload fields.
func (s *KBService) UpdateJob(ctx context.Context, actor ActorContext, jobID uint64, input JobStatusUpdateInput) (*models.KBJob, error) {
	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, job.KnowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	if input.Status != nil {
		job.Status = *input.Status
	}
	if input.Progress != nil {
		job.Progress = *input.Progress
	}
	if input.ErrorMessage != nil {
		job.ErrorMessage = *input.ErrorMessage
	}
	if input.Payload != nil {
		jsonValue, err := mapToJSON(input.Payload)
		if err != nil {
			return nil, err
		}
		job.Payload = jsonValue
	}
	if input.StartedAt != nil {
		job.StartedAt = input.StartedAt
	}
	if input.CompletedAt != nil {
		job.CompletedAt = input.CompletedAt
	}
	if err := s.repo.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("update job: %w", err)
	}
	return job, nil
}

// ListJobs returns jobs for a knowledge base that the actor can access.
func (s *KBService) ListJobs(ctx context.Context, actor ActorContext, filter repository.KBJobFilter, limit, offset int) ([]*models.KBJob, int64, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, filter.KnowledgeBaseID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, 0, err
	}
	jobs, total, err := s.repo.ListJobs(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, total, nil
}

// QueryKnowledgeBase executes a retrieval query via the configured vector store.
func (s *KBService) QueryKnowledgeBase(ctx context.Context, actor ActorContext, knowledgeBaseID uint64, query string, topK int) ([]vectorstore.SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query cannot be empty")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	strategy := defaultRetrievalStrategy(kb.RetrievalStrategy)
	switch strategy {
	case "keyword":
		return s.keywordSearch(ctx, kb, query, topK)
	case "hybrid":
		var vectorResults []vectorstore.SearchResult
		if s.vectorStore != nil {
			vr, err := s.vectorStore.Query(ctx, knowledgeBaseID, query, topK)
			if err != nil {
				return nil, fmt.Errorf("vector query: %w", err)
			}
			vectorResults = vr
		}
		chunks, err := s.repo.SearchChunksByContent(ctx, knowledgeBaseID, query, topK)
		if err != nil {
			return nil, fmt.Errorf("keyword search: %w", err)
		}
		results := mergeHybridResults(vectorResults, chunks, query, topK)
		return s.enrichSearchResults(ctx, results), nil
	default:
		if s.vectorStore == nil {
			return nil, vectorstore.ErrNotConfigured
		}
		results, err := s.vectorStore.Query(ctx, knowledgeBaseID, query, topK)
		if err != nil {
			return nil, fmt.Errorf("vector query: %w", err)
		}
		return s.enrichSearchResults(ctx, results), nil
	}
}

// InternalQueryKnowledgeBase executes a retrieval query for backend-server using server-secret auth.
func (s *KBService) InternalQueryKnowledgeBase(ctx context.Context, knowledgeBaseID uint64, opts InternalQueryOptions) ([]vectorstore.SearchResult, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, errors.New("query cannot be empty")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if strings.EqualFold(kb.Status, "archived") {
		return nil, errors.New("knowledge base archived")
	}
	if s.vectorStore == nil {
		return nil, vectorstore.ErrNotConfigured
	}
	topK := opts.TopK
	if topK <= 0 {
		topK = 8
	}
	results, err := s.vectorStore.Query(ctx, knowledgeBaseID, opts.Query, topK)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	return s.enrichSearchResults(ctx, results), nil
}

// KnowledgeBaseMetadata exposes minimal metadata for backend warmup.
type KnowledgeBaseMetadata struct {
	ID                uint64         `json:"id"`
	ProjectID         string         `json:"projectId"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	EmbeddingModel    string         `json:"embeddingModel"`
	RetrievalStrategy string         `json:"retrievalStrategy"`
	RetrievalParams   map[string]any `json:"retrievalParams,omitempty"`
}

// GetKnowledgeBaseMetadata returns metadata without permission checks (internal only).
func (s *KBService) GetKnowledgeBaseMetadata(ctx context.Context, knowledgeBaseID uint64) (*KnowledgeBaseMetadata, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	meta := &KnowledgeBaseMetadata{
		ID:                kb.ID,
		ProjectID:         kb.ProjectID,
		Name:              kb.Name,
		Description:       kb.Description,
		EmbeddingModel:    kb.EmbeddingModel,
		RetrievalStrategy: kb.RetrievalStrategy,
	}
	if len(kb.RetrievalParams) > 0 {
		params := map[string]any{}
		if err := json.Unmarshal(kb.RetrievalParams, &params); err == nil {
			meta.RetrievalParams = params
		}
	}
	return meta, nil
}

const (
	meetingMinutesProjectName = "Meeting Minutes"
	meetingMinutesKBName      = "Meeting Minutes"
)

// EnsureMeetingMinutesKnowledgeBase resolves or creates the meeting minutes knowledge base for the user.
func (s *KBService) EnsureMeetingMinutesKnowledgeBase(ctx context.Context, actor ActorContext, agentID uint64) (*models.KBKnowledgeBase, error) {
	if actor.UserID == 0 {
		return nil, errors.New("missing actor userID")
	}

	project, err := s.findMeetingMinutesProject(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		project, err = s.CreateProject(ctx, actor, CreateProjectInput{
			Name:       meetingMinutesProjectName,
			Visibility: "private",
			Metadata: map[string]any{
				"type": "meeting_minutes",
			},
		})
		if err != nil {
			return nil, err
		}
	}

	kb, err := s.findMeetingMinutesKnowledgeBase(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		kb, err = s.CreateKnowledgeBase(ctx, actor, CreateKnowledgeBaseInput{
			ProjectID:   project.ID,
			Name:        meetingMinutesKBName,
			Description: "Meeting minutes generated from voice sessions.",
		})
		if err != nil {
			return nil, err
		}
	}

	if agentID > 0 {
		if err := s.ensureMeetingMinutesMount(ctx, actor, project.ID, agentID); err != nil {
			return nil, err
		}
	}

	return kb, nil
}

func (s *KBService) findMeetingMinutesProject(ctx context.Context, ownerID uint64) (*models.KBProject, error) {
	limit := 50
	offset := 0
	for {
		projects, total, err := s.repo.ListProjects(ctx, repository.KBProjectFilter{
			OwnerID:  &ownerID,
			NameLike: meetingMinutesProjectName,
		}, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		for _, project := range projects {
			if project != nil && strings.EqualFold(project.Name, meetingMinutesProjectName) {
				return project, nil
			}
		}
		if int64(offset+limit) >= total {
			return nil, nil
		}
		offset += limit
	}
}

func (s *KBService) findMeetingMinutesKnowledgeBase(ctx context.Context, projectID string) (*models.KBKnowledgeBase, error) {
	limit := 50
	offset := 0
	for {
		kbs, total, err := s.repo.ListKnowledgeBases(ctx, repository.KBKnowledgeBaseFilter{
			ProjectID: projectID,
			NameLike:  meetingMinutesKBName,
		}, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("list knowledge bases: %w", err)
		}
		for _, kb := range kbs {
			if kb != nil && strings.EqualFold(kb.Name, meetingMinutesKBName) {
				return kb, nil
			}
		}
		if int64(offset+limit) >= total {
			return nil, nil
		}
		offset += limit
	}
}

func (s *KBService) ensureMeetingMinutesMount(ctx context.Context, actor ActorContext, projectID string, agentID uint64) error {
	mounts, err := s.repo.ListProjectAgentMounts(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list project mounts: %w", err)
	}
	for _, mount := range mounts {
		if mount != nil && mount.AgentID == agentID {
			return nil
		}
	}
	return s.MountAgentProject(ctx, actor, AgentProjectMountInput{
		AgentID:   agentID,
		ProjectID: projectID,
	})
}

// GrantPermission creates or updates a knowledge base permission.
func (s *KBService) GrantPermission(ctx context.Context, actor ActorContext, input PermissionGrantInput) error {
	if _, ok := kbValidRoles[input.Role]; !ok {
		return fmt.Errorf("invalid role: %s", input.Role)
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, input.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("fetch knowledge base: %w", err)
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	permission := &models.KBPermission{
		KnowledgeBaseID: input.KnowledgeBaseID,
		UserID:          input.UserID,
		Role:            input.Role,
	}
	if err := s.repo.UpsertPermission(ctx, permission); err != nil {
		return fmt.Errorf("upsert permission: %w", err)
	}
	return nil
}

// RevokePermission removes a permission entry.
func (s *KBService) RevokePermission(ctx context.Context, actor ActorContext, knowledgeBaseID, userID uint64) error {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return fmt.Errorf("fetch knowledge base: %w", err)
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	if err := s.repo.RemovePermission(ctx, knowledgeBaseID, userID); err != nil {
		return fmt.Errorf("remove permission: %w", err)
	}
	return nil
}

// ListPermissions returns permissions after verifying project ownership.
func (s *KBService) ListPermissions(ctx context.Context, actor ActorContext, knowledgeBaseID uint64) ([]*models.KBPermission, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return nil, err
	}
	perms, err := s.repo.ListPermissions(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	return perms, nil
}

// MountAgentProject links an agent to a project when authorized.
func (s *KBService) MountAgentProject(ctx context.Context, actor ActorContext, input AgentProjectMountInput) error {
	project, err := s.repo.GetProjectByID(ctx, input.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	mount := &models.KBAgentProjectMount{
		AgentID:   input.AgentID,
		ProjectID: input.ProjectID,
	}
	if input.Capabilities != nil {
		jsonValue, err := mapToJSON(input.Capabilities)
		if err != nil {
			return err
		}
		mount.Capabilities = jsonValue
	}
	if err := s.repo.MountAgentProject(ctx, mount); err != nil {
		return fmt.Errorf("mount agent project: %w", err)
	}
	s.notifyAgents(ctx, []uint64{mount.AgentID})
	return nil
}

// UnmountAgentProject removes an agent-project association.
func (s *KBService) UnmountAgentProject(ctx context.Context, actor ActorContext, agentID uint64, projectID string) error {
	project, err := s.repo.GetProjectByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if err := s.ensureProjectAccess(actor, project); err != nil {
		return err
	}
	if err := s.repo.UnmountAgentProject(ctx, agentID, projectID); err != nil {
		return fmt.Errorf("unmount agent project: %w", err)
	}
	s.notifyAgents(ctx, []uint64{agentID})
	return nil
}

// ListAgentProjectMounts lists mounts; currently admin-only.
func (s *KBService) ListAgentProjectMounts(ctx context.Context, actor ActorContext, agentID uint64) ([]*models.KBAgentProjectMount, error) {
	if !actor.IsAdmin {
		return nil, errors.New("forbidden: admin access required to list mounts")
	}
	mounts, err := s.repo.ListAgentProjectMounts(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent project mounts: %w", err)
	}
	return mounts, nil
}

// scheduleIngestionJob persists an ingestion job and notifies the dispatcher.
func (s *KBService) scheduleIngestionJob(ctx context.Context, kb *models.KBKnowledgeBase, doc *models.KBDocument, rawContent string) (*models.KBJob, error) {
	payload := map[string]any{
		"documentId": doc.ID,
		"sourceType": doc.SourceType,
		"connector":  doc.Source,
		"originId":   doc.OriginID,
	}
	if strings.TrimSpace(rawContent) != "" {
		payload["rawContent"] = rawContent
	}
	if strings.TrimSpace(doc.StorageURI) != "" {
		payload["storageUri"] = doc.StorageURI
	}
	payloadJSON, err := mapToJSON(payload)
	if err != nil {
		return nil, err
	}
	maxRetries := s.retryConfig.MaxAttempts
	if maxRetries <= 0 {
		maxRetries = 1
	}
	job := &models.KBJob{
		KnowledgeBaseID: kb.ID,
		DocumentID:      &doc.ID,
		JobType:         "ingestion",
		Connector:       doc.Source,
		Status:          "queued",
		Payload:         payloadJSON,
		MaxRetries:      maxRetries,
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	if s.jobRunner != nil {
		if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
			return nil, fmt.Errorf("enqueue job: %w", err)
		}
	}
	return job, nil
}

// helper: ensure actor can manage project (write access).
func (s *KBService) ensureProjectAccess(actor ActorContext, project *models.KBProject) error {
	if actor.IsAdmin {
		return nil
	}
	if project.OwnerID != actor.UserID {
		return errors.New("forbidden: no access to project")
	}
	return nil
}

// helper: ensure actor can at least read project.
func (s *KBService) ensureProjectReadAccess(actor ActorContext, project *models.KBProject) error {
	if actor.IsAdmin {
		return nil
	}
	if project.OwnerID == actor.UserID {
		return nil
	}
	if project.Visibility == "shared" {
		return nil
	}
	return errors.New("forbidden: no access to project")
}

// helper: ensure actor can read knowledge base.
func (s *KBService) ensureKnowledgeBaseReadAccess(ctx context.Context, actor ActorContext, kb *models.KBKnowledgeBase) error {
	if actor.IsAdmin {
		return nil
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if project.OwnerID == actor.UserID {
		return nil
	}
	if project.Visibility == "shared" {
		return nil
	}
	permission, err := s.repo.GetPermission(ctx, kb.ID, actor.UserID)
	if err != nil {
		return err
	}
	if permission == nil {
		return errors.New("forbidden: no access to knowledge base")
	}
	return nil
}

// helper: ensure actor can mutate knowledge base.
func (s *KBService) ensureKnowledgeBaseWriteAccess(ctx context.Context, actor ActorContext, kb *models.KBKnowledgeBase) error {
	if actor.IsAdmin {
		return nil
	}
	project, err := s.repo.GetProjectByID(ctx, kb.ProjectID)
	if err != nil {
		return fmt.Errorf("fetch project: %w", err)
	}
	if project.OwnerID == actor.UserID {
		return nil
	}
	permission, err := s.repo.GetPermission(ctx, kb.ID, actor.UserID)
	if err != nil {
		return err
	}
	if permission == nil {
		return errors.New("forbidden: no access to knowledge base")
	}
	if permission.Role == KBRoleViewer {
		return errors.New("forbidden: write access requires editor role")
	}
	return nil
}

func validateProjectName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("project name is required")
	}
	return nil
}

func validateKnowledgeBaseName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("knowledge base name is required")
	}
	return nil
}

func normalizeVisibility(value string) string {
	switch strings.ToLower(value) {
	case "shared", "system":
		return strings.ToLower(value)
	default:
		return "private"
	}
}

func defaultRetrievalStrategy(strategy string) string {
	if strings.TrimSpace(strategy) == "" {
		return "dense"
	}
	return strings.ToLower(strategy)
}

func (s *KBService) notifyAgents(ctx context.Context, agentIDs []uint64) {
	if s.events == nil || len(agentIDs) == 0 {
		return
	}
	s.events.AgentsCapabilitiesChanged(ctx, agentIDs)
}

func (s *KBService) emitProjectAgentsChanged(ctx context.Context, projectID string) {
	if s.events == nil {
		return
	}
	mounts, err := s.repo.ListProjectAgentMounts(ctx, projectID)
	if err != nil {
		logger.Warnf("project_id=%s err=%v msg=kb_project_emit_failed", projectID, err)
		return
	}
	if len(mounts) == 0 {
		return
	}
	seen := make(map[uint64]struct{}, len(mounts))
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
	s.events.AgentsCapabilitiesChanged(ctx, agentIDs)
}

func (s *KBService) emitJobStatus(ctx context.Context, job *models.KBJob) {
	if s.events == nil || job == nil {
		return
	}
	s.events.JobStatusChanged(ctx, job)
}

func (s *KBService) checkIngestionQuota(ctx context.Context, kb *models.KBKnowledgeBase, input CreateDocumentInput) error {
	quota := s.quotaConfig
	if kb == nil || (quota.MaxDocumentsPerDay <= 0 && quota.MaxBytesPerDay <= 0 && quota.MaxConcurrentJobs <= 0) {
		return nil
	}

	now := time.Now()
	windowStart := now.Add(-24 * time.Hour)

	if quota.MaxDocumentsPerDay > 0 {
		count, err := s.repo.CountDocumentsSince(ctx, kb.ID, windowStart)
		if err != nil {
			return fmt.Errorf("quota check failed: %w", err)
		}
		if count >= int64(quota.MaxDocumentsPerDay) {
			return fmt.Errorf("超出每日导入数量上限（%d）", quota.MaxDocumentsPerDay)
		}
	}

	sizeEstimate := input.SizeBytes
	if sizeEstimate <= 0 && strings.TrimSpace(input.RawContent) != "" {
		sizeEstimate = int64(len([]byte(input.RawContent)))
	}
	if quota.MaxBytesPerDay > 0 && sizeEstimate > 0 {
		bytesUsed, err := s.repo.SumDocumentBytesSince(ctx, kb.ID, windowStart)
		if err != nil {
			return fmt.Errorf("quota check failed: %w", err)
		}
		if bytesUsed+sizeEstimate > quota.MaxBytesPerDay {
			return fmt.Errorf("超出每日导入容量上限（%.2f MB）", float64(quota.MaxBytesPerDay)/1024.0/1024.0)
		}
	}

	if quota.MaxConcurrentJobs > 0 {
		activeJobs, err := s.repo.CountActiveJobs(ctx, kb.ID)
		if err != nil {
			return fmt.Errorf("quota check failed: %w", err)
		}
		if activeJobs >= int64(quota.MaxConcurrentJobs) {
			return fmt.Errorf("当前有 %d 个任务运行，超过并发上限 %d，请稍后重试", activeJobs, quota.MaxConcurrentJobs)
		}
	}
	return nil
}

func mapToJSON(m map[string]any) (datatypes.JSON, error) {
	if m == nil {
		return nil, nil
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(bytes), nil
}

func normalizeExtension(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.ToLower(ext)
}

func deriveDocumentTitle(name, ext string) string {
	base := filepath.Base(name)
	if ext != "" && strings.HasSuffix(strings.ToLower(base), ext) {
		base = base[:len(base)-len(ext)]
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = strings.TrimSpace(filepath.Base(name))
	}
	if base == "" {
		base = "Document"
	}
	runes := []rune(base)
	if len(runes) > 255 {
		runes = runes[:255]
	}
	return string(runes)
}

func sourceTypeFromExtension(ext string) string {
	if ext == "" {
		return "file"
	}
	return "file:" + strings.TrimPrefix(ext, ".")
}

func (s *KBService) isFormatAllowed(ext string) bool {
	if len(s.formats) == 0 {
		return true
	}
	_, ok := s.formats[ext]
	return ok
}

func (s *KBService) createDocumentInternal(ctx context.Context, actor ActorContext, kb *models.KBKnowledgeBase, input CreateDocumentInput) (*models.KBDocument, *models.KBJob, error) {
	if kb == nil {
		return nil, nil, errors.New("knowledge base context required")
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, nil, err
	}
	input.KnowledgeBaseID = kb.ID
	if strings.TrimSpace(input.Title) == "" {
		return nil, nil, errors.New("document title is required")
	}
	if input.EnqueueIngestion && strings.TrimSpace(input.RawContent) == "" && strings.TrimSpace(input.StorageURI) == "" {
		return nil, nil, errors.New("rawContent or storageUri required when enqueueIngestion is true")
	}
	if strings.TrimSpace(input.SourceType) == "" {
		input.SourceType = "manual"
	}
	if strings.TrimSpace(input.Source) == "" {
		input.Source = input.SourceType
	}
	if strings.TrimSpace(input.Connector) == "" {
		input.Connector = input.Source
	}
	if strings.TrimSpace(input.OriginID) == "" {
		input.OriginID = input.StorageURI
	}

	if input.EnqueueIngestion {
		if err := s.checkIngestionQuota(ctx, kb, input); err != nil {
			return nil, nil, err
		}
	}

	doc := &models.KBDocument{
		KnowledgeBaseID: input.KnowledgeBaseID,
		Title:           input.Title,
		SourceType:      input.SourceType,
		Source:          input.Source,
		OriginID:        input.OriginID,
		StorageURI:      input.StorageURI,
		Checksum:        input.Checksum,
		SizeBytes:       input.SizeBytes,
		UploaderID:      actor.UserID,
	}
	if input.EnqueueIngestion {
		doc.ParseStatus = "queued"
	}
	meta := make(map[string]any)
	for k, v := range input.Metadata {
		meta[k] = v
	}
	if _, ok := meta["connector"]; !ok && strings.TrimSpace(input.Connector) != "" {
		meta["connector"] = input.Connector
	}
	if _, ok := meta["source"]; !ok && strings.TrimSpace(input.Source) != "" {
		meta["source"] = input.Source
	}
	if _, ok := meta["originId"]; !ok && strings.TrimSpace(input.OriginID) != "" {
		meta["originId"] = input.OriginID
	}
	if len(meta) > 0 {
		jsonValue, err := mapToJSON(meta)
		if err != nil {
			return nil, nil, err
		}
		doc.Metadata = jsonValue
	}
	if err := s.repo.CreateDocument(ctx, doc); err != nil {
		return nil, nil, fmt.Errorf("create document: %w", err)
	}

	var job *models.KBJob
	if input.EnqueueIngestion {
		var err error
		job, err = s.scheduleIngestionJob(ctx, kb, doc, input.RawContent)
		if err != nil {
			return nil, nil, err
		}
	}
	s.emitProjectAgentsChanged(ctx, kb.ProjectID)
	return doc, job, nil
}

// UploadDocuments saves uploaded files to storage and schedules ingestion jobs.
func (s *KBService) UploadDocuments(ctx context.Context, actor ActorContext, kbID uint64, files []UploadDocumentInput) ([]*models.KBDocument, []*models.KBJob, error) {
	if len(files) == 0 {
		return nil, nil, errors.New("no files provided")
	}
	if s.storage == nil {
		return nil, nil, errors.New("file storage not configured")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch knowledge base: %w", err)
	}

	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, nil, err
	}

	documents := make([]*models.KBDocument, 0, len(files))
	jobs := make([]*models.KBJob, 0, len(files))
	for _, file := range files {
		if file.Reader == nil {
			return nil, nil, errors.New("file reader is nil")
		}
		ext := normalizeExtension(filepath.Ext(file.FileName))
		if len(s.formats) > 0 {
			if ext == "" {
				_ = file.Reader.Close()
				return nil, nil, fmt.Errorf("文件 %s 缺少扩展名", file.FileName)
			}
			if !s.isFormatAllowed(ext) {
				_ = file.Reader.Close()
				return nil, nil, fmt.Errorf("不支持的文件类型: %s", ext)
			}
		}
		if s.maxUpload > 0 && file.Size > s.maxUpload {
			_ = file.Reader.Close()
			return nil, nil, fmt.Errorf("文件过大: %s", file.FileName)
		}

		saveRes, err := s.storage.Save(ctx, kb.ID, &storage.DocumentFile{
			Name:        file.FileName,
			Size:        file.Size,
			ContentType: file.ContentType,
			Reader:      file.Reader,
		})
		closeErr := file.Reader.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("保存文件失败: %w", err)
		}
		if closeErr != nil {
			logger.Warnf("关闭上传文件失败: %v", closeErr)
		}

		metadata := map[string]any{
			"originalFileName": file.FileName,
			"checksum":         saveRes.Checksum,
		}
		if file.ContentType != "" {
			metadata["contentType"] = file.ContentType
		}
		if ext != "" {
			metadata["extension"] = ext
		}

		title := deriveDocumentTitle(file.FileName, ext)
		doc, job, err := s.createDocumentInternal(ctx, actor, kb, CreateDocumentInput{
			KnowledgeBaseID:  kb.ID,
			Title:            title,
			SourceType:       sourceTypeFromExtension(ext),
			Source:           "file",
			OriginID:         file.FileName,
			Connector:        "file",
			StorageURI:       saveRes.URI,
			Checksum:         saveRes.Checksum,
			SizeBytes:        saveRes.Size,
			Metadata:         metadata,
			EnqueueIngestion: true,
		})
		if err != nil {
			_ = s.storage.Delete(ctx, saveRes.URI)
			return nil, nil, err
		}
		documents = append(documents, doc)
		if job != nil {
			jobs = append(jobs, job)
		}
	}
	return documents, jobs, nil
}

// SubmitDocumentURL enqueues ingestion for a remote document reference.
func (s *KBService) SubmitDocumentURL(ctx context.Context, actor ActorContext, input SubmitDocumentURLInput) (*models.KBDocument, *models.KBJob, error) {
	if input.KnowledgeBaseID == 0 {
		return nil, nil, errors.New("knowledgeBaseId is required")
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return nil, nil, errors.New("url is required")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid url: %w", err)
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, nil, err
	}

	ext := normalizeExtension(filepath.Ext(parsed.Path))
	if len(s.formats) > 0 && ext != "" && !s.isFormatAllowed(ext) {
		return nil, nil, fmt.Errorf("不支持的文件类型: %s", ext)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = deriveDocumentTitle(parsed.Path, ext)
		if title == "" {
			title = "Remote Document"
		}
	}

	metadata := map[string]any{
		"originalUrl": parsed.String(),
	}
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	if ext != "" {
		metadata["extension"] = ext
	}
	if strings.TrimSpace(input.ContentType) != "" {
		metadata["contentType"] = input.ContentType
	}

	return s.createDocumentInternal(ctx, actor, kb, CreateDocumentInput{
		KnowledgeBaseID:  kb.ID,
		Title:            title,
		SourceType:       "url",
		Source:           "url",
		OriginID:         parsed.String(),
		Connector:        "url",
		StorageURI:       parsed.String(),
		Metadata:         metadata,
		EnqueueIngestion: true,
	})
}

// JobRunnerStatus provides a snapshot of the ingestion runner.
type JobRunnerStatus struct {
	Paused bool `json:"paused"`
}

// JobStatusSummary aggregates job counts by status.
type JobStatusSummary struct {
	StatusCounts map[string]int64 `json:"statusCounts"`
}

// PauseJobRunner temporarily stops ingestion workers.
func (s *KBService) PauseJobRunner(ctx context.Context, actor ActorContext) error {
	if !actor.IsAdmin {
		return errors.New("forbidden: admin access required")
	}
	if s.jobRunner == nil {
		return errors.New("job runner not configured")
	}
	s.jobRunner.Pause()
	return nil
}

// ResumeJobRunner resumes ingestion workers.
func (s *KBService) ResumeJobRunner(ctx context.Context, actor ActorContext) error {
	if !actor.IsAdmin {
		return errors.New("forbidden: admin access required")
	}
	if s.jobRunner == nil {
		return errors.New("job runner not configured")
	}
	s.jobRunner.Resume()
	return nil
}

// JobRunnerStatus queries the current runner state.
func (s *KBService) JobRunnerStatus(ctx context.Context, actor ActorContext) (JobRunnerStatus, error) {
	if !actor.IsAdmin {
		return JobRunnerStatus{}, errors.New("forbidden: admin access required")
	}
	if s.jobRunner == nil {
		return JobRunnerStatus{}, errors.New("job runner not configured")
	}
	return JobRunnerStatus{Paused: s.jobRunner.IsPaused()}, nil
}

// JobStatusSummary returns counts of jobs grouped by status for a knowledge base.
func (s *KBService) JobStatusSummary(ctx context.Context, actor ActorContext, kbID uint64) (JobStatusSummary, error) {
	if kbID == 0 {
		return JobStatusSummary{}, errors.New("knowledgeBaseId is required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return JobStatusSummary{}, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return JobStatusSummary{}, err
	}
	counts, err := s.repo.JobStatusSummary(ctx, kbID)
	if err != nil {
		return JobStatusSummary{}, fmt.Errorf("job status summary: %w", err)
	}
	return JobStatusSummary{StatusCounts: counts}, nil
}

func (s *KBService) keywordSearch(ctx context.Context, kb *models.KBKnowledgeBase, query string, topK int) ([]vectorstore.SearchResult, error) {
	chunks, err := s.repo.SearchChunksByContent(ctx, kb.ID, query, topK)
	if err != nil {
		return nil, err
	}
	results := make([]vectorstore.SearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		results = append(results, chunkToSearchResult(chunk, keywordChunkScore(chunk, query)))
	}
	return s.enrichSearchResults(ctx, results), nil
}

func mergeHybridResults(vectorResults []vectorstore.SearchResult, keywordChunks []*models.KBChunk, query string, topK int) []vectorstore.SearchResult {
	resultMap := make(map[uint64]vectorstore.SearchResult)
	for _, vr := range vectorResults {
		if vr.ChunkID == 0 {
			continue
		}
		if vr.Score <= 0 {
			vr.Score = 0.5
		}
		resultMap[vr.ChunkID] = vr
	}
	for _, chunk := range keywordChunks {
		if chunk == nil {
			continue
		}
		score := keywordChunkScore(chunk, query)
		if existing, ok := resultMap[chunk.ID]; ok {
			existing.Score += score
			existing.DocumentID = chunk.DocumentID
			existing.DocumentTitle = stringFromMeta(existing.Meta, "documentTitle")
			existing.StorageURI = stringFromMeta(existing.Meta, "storageUri")
			existing.SourceType = stringFromMeta(existing.Meta, "sourceType")
			if existing.Content == "" {
				existing.Content = chunk.Content
			}
			if existing.Meta == nil {
				existing.Meta = map[string]any{}
			}
			if len(existing.Meta) == 0 {
				existing.Meta = parseChunkMetadata(chunk.Metadata)
			}
			existing.Meta["documentId"] = chunk.DocumentID
			existing.Meta["chunkId"] = chunk.ID
			existing.Meta["manualEdit"] = chunk.ManualEdit
			existing.Meta["documentTitle"] = stringFromMeta(existing.Meta, "documentTitle")
			existing.Meta["storageUri"] = stringFromMeta(existing.Meta, "storageUri")
			existing.Meta["sourceType"] = stringFromMeta(existing.Meta, "sourceType")
			if existing.Chunk == nil {
				existing.Chunk = chunk
			}
			existing.DocumentTitle = stringFromMeta(existing.Meta, "documentTitle")
			existing.StorageURI = stringFromMeta(existing.Meta, "storageUri")
			existing.SourceType = stringFromMeta(existing.Meta, "sourceType")
			resultMap[chunk.ID] = existing
			continue
		}
		resultMap[chunk.ID] = chunkToSearchResult(chunk, score)
	}
	results := make([]vectorstore.SearchResult, 0, len(resultMap))
	for _, res := range resultMap {
		results = append(results, res)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].Score > results[j].Score
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

func chunkToSearchResult(chunk *models.KBChunk, score float64) vectorstore.SearchResult {
	meta := parseChunkMetadata(chunk.Metadata)
	meta["documentId"] = chunk.DocumentID
	meta["chunkId"] = chunk.ID
	meta["manualEdit"] = chunk.ManualEdit
	return vectorstore.SearchResult{
		DocumentID:    chunk.DocumentID,
		DocumentTitle: stringFromMeta(meta, "documentTitle"),
		StorageURI:    stringFromMeta(meta, "storageUri"),
		SourceType:    stringFromMeta(meta, "sourceType"),
		ChunkID:       chunk.ID,
		Score:         score,
		Content:       chunk.Content,
		Meta:          meta,
		Chunk:         chunk,
	}
}

func keywordChunkScore(chunk *models.KBChunk, query string) float64 {
	if chunk == nil {
		return 0.5
	}
	lowerContent := strings.ToLower(chunk.Content)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return 0.5
	}
	occurrences := strings.Count(lowerContent, lowerQuery)
	if occurrences == 0 && strings.Contains(lowerContent, lowerQuery) {
		occurrences = 1
	}
	score := 0.6 + float64(occurrences)*0.2
	if score > 1.5 {
		score = 1.5
	}
	return score
}

func parseChunkMetadata(data datatypes.JSON) map[string]any {
	meta := make(map[string]any)
	if len(data) == 0 {
		return meta
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta
	}
	return meta
}

func (s *KBService) enrichSearchResults(ctx context.Context, results []vectorstore.SearchResult) []vectorstore.SearchResult {
	if len(results) == 0 {
		return results
	}
	cache := make(map[uint64]*models.KBDocument)
	for i := range results {
		res := &results[i]
		if res.Meta == nil {
			res.Meta = map[string]any{}
		}
		res.Meta["documentId"] = res.DocumentID
		res.Meta["chunkId"] = res.ChunkID
		res.Meta["score"] = res.Score
		if res.DocumentID == 0 {
			continue
		}
		doc, ok := cache[res.DocumentID]
		if !ok {
			dbDoc, err := s.repo.GetDocumentByID(ctx, res.DocumentID)
			if err != nil {
				logger.Warnf("msg=kb_search_enrich_failed doc_id=%d err=%v", res.DocumentID, err)
			}
			doc = dbDoc
			cache[res.DocumentID] = doc
		}
		if doc == nil {
			continue
		}
		if res.DocumentTitle == "" {
			res.DocumentTitle = doc.Title
		}
		if res.StorageURI == "" {
			res.StorageURI = doc.StorageURI
		}
		if res.SourceType == "" {
			res.SourceType = doc.SourceType
		}
		res.Meta["documentTitle"] = res.DocumentTitle
		res.Meta["storageUri"] = res.StorageURI
		res.Meta["sourceType"] = res.SourceType
	}
	return results
}

func stringFromMeta(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if val, ok := meta[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case json.Number:
			return v.String()
		default:
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}
