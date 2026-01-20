package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"manager-server/internal/kb/ingest"
	"manager-server/internal/kb/ingest/connectors"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

// ParseSourceOutput bundles the parse tree and associated session token.
type ParseSourceOutput struct {
	SessionToken string            `json:"sessionToken"`
	Tree         *ingest.ParseTree `json:"tree"`
}

// ImportSelection represents a node selected for import.
type ImportSelection struct {
	NodeID   string         `json:"nodeId"`
	Metadata map[string]any `json:"metadata"`
	Title    string         `json:"title"`
}

// IngestionWebhookEvent captures job updates pushed by external connectors.
type IngestionWebhookEvent struct {
	KnowledgeBaseID uint64         `json:"knowledgeBaseId"`
	JobID           uint64         `json:"jobId"`
	Status          string         `json:"status"`
	Progress        *int           `json:"progress"`
	ErrorMessage    string         `json:"errorMessage"`
	ErrorType       string         `json:"errorType"`
	Payload         map[string]any `json:"payload"`
	Completed       bool           `json:"completed"`
	RemoteAddr      string         `json:"remoteAddr"`
}

type sessionPayload struct {
	Tree         *ingest.ParseTree `json:"tree"`
	Params       map[string]any    `json:"params"`
	Credentials  map[string]any    `json:"credentials"`
	Metadata     map[string]any    `json:"metadata"`
	Connector    string            `json:"connector"`
	SessionToken string            `json:"sessionToken"`
}

// KBIngestionService orchestrates multi-source ingestion flows.
type KBIngestionService struct {
	repo           repository.KBRepository
	kbService      *KBService
	registry       *connectors.Registry
	storage        storage.DocumentStorage
	sessionTTL     time.Duration
	now            func() time.Time
	connectors     map[string]ConnectorDefinition
	quotaConfig    IngestionQuotaConfig
	retryConfig    IngestionRetryConfig
	webhookEnabled bool
	webhookSecret  string
	webhookCIDRs   []netip.Prefix
}

// NewKBIngestionService constructs the ingestion service.
func NewKBIngestionService(
	repo repository.KBRepository,
	kbService *KBService,
	registry *connectors.Registry,
	storage storage.DocumentStorage,
	sessionTTL time.Duration,
	definitions map[string]ConnectorDefinition,
	quota IngestionQuotaConfig,
	retry IngestionRetryConfig,
	webhookEnabled bool,
	webhookSecret string,
	webhookCIDRs []string,
) *KBIngestionService {
	if sessionTTL <= 0 {
		sessionTTL = 30 * time.Minute
	}
	connectorMap := make(map[string]ConnectorDefinition, len(definitions))
	for name, def := range definitions {
		normalized := normalizeConnectorName(name)
		if normalized == "" {
			continue
		}
		if def.Name == "" {
			def.Name = normalized
		}
		connectorMap[normalized] = def
	}
	parsedCIDRs := parseCIDRs(webhookCIDRs)

	return &KBIngestionService{
		repo:           repo,
		kbService:      kbService,
		registry:       registry,
		storage:        storage,
		sessionTTL:     sessionTTL,
		now:            time.Now,
		connectors:     connectorMap,
		quotaConfig:    quota,
		retryConfig:    retry,
		webhookEnabled: webhookEnabled,
		webhookSecret:  stringsTrim(webhookSecret),
		webhookCIDRs:   parsedCIDRs,
	}
}

// ListConnectors returns the registered connector identifiers.
func (s *KBIngestionService) ListConnectors() []string {
	if s.registry == nil {
		return nil
	}
	names := append([]string(nil), s.registry.List()...)
	sort.Strings(names)
	return names
}

// Describe aggregates connector definitions and ingestion policy for a knowledge base.
func (s *KBIngestionService) Describe(ctx context.Context, actor ActorContext, kbID uint64) (*IngestionOverview, error) {
	if kbID == 0 {
		return nil, errors.New("knowledge base id required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.kbService.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}

	defs := s.resolveConnectorDefinitions()
	policy, err := s.buildPolicy(ctx, kb)
	if err != nil {
		return nil, err
	}

	return &IngestionOverview{
		Connectors: defs,
		Policy:     policy,
	}, nil
}

func (s *KBIngestionService) WebhookEnabled() bool {
	return s != nil && s.webhookEnabled
}

func (s *KBIngestionService) VerifyWebhookSecret(secret string) bool {
	if s == nil || !s.webhookEnabled {
		return true
	}
	expected := stringsTrim(s.webhookSecret)
	if expected == "" {
		return true
	}
	provided := stringsTrim(secret)
	if len(provided) == 0 || len(provided) != len(expected) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1 {
		return true
	}
	return false
}

func (s *KBIngestionService) ValidateWebhookSource(remoteAddr string) error {
	if s == nil || len(s.webhookCIDRs) == 0 {
		return nil
	}
	trimmed := stringsTrim(remoteAddr)
	if trimmed == "" {
		return errors.New("webhook 请求缺少来源地址")
	}
	addr, err := parseRemoteAddr(trimmed)
	if err != nil {
		return fmt.Errorf("解析来源地址失败: %w", err)
	}
	for _, prefix := range s.webhookCIDRs {
		if prefix.Contains(addr) {
			return nil
		}
	}
	return fmt.Errorf("Webhook 来源 %s 不在允许的 CIDR 列表内", addr.String())
}

func (s *KBIngestionService) HandleWebhookEvent(ctx context.Context, event IngestionWebhookEvent) (*models.KBJob, error) {
	if s == nil || !s.webhookEnabled {
		return nil, errors.New("webhook disabled")
	}
	if event.JobID == 0 {
		return nil, errors.New("jobId required")
	}
	job, err := s.repo.GetJobByID(ctx, event.JobID)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	if event.KnowledgeBaseID != 0 && job.KnowledgeBaseID != event.KnowledgeBaseID {
		return nil, errors.New("knowledge base mismatch")
	}
	if status := stringsTrim(event.Status); status != "" {
		job.Status = status
	}
	if event.Progress != nil {
		job.Progress = *event.Progress
	}
	if event.ErrorMessage != "" {
		job.ErrorMessage = event.ErrorMessage
	}
	if event.ErrorType != "" {
		job.ErrorType = event.ErrorType
	}
	if event.Completed {
		now := s.now()
		job.CompletedAt = &now
		job.NextRetryAt = nil
	}
	if event.Payload != nil {
		if jsonValue, err := mapToJSON(event.Payload); err == nil {
			job.Payload = jsonValue
		}
	}
	if err := s.repo.UpdateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("update job: %w", err)
	}
	s.kbService.emitJobStatus(ctx, job)
	s.updateDocumentFromJob(ctx, job)
	_ = s.recordWebhookLog(ctx, job, event)
	return job, nil
}

func (s *KBIngestionService) ListWebhookEvents(ctx context.Context, actor ActorContext, kbID, jobID uint64, limit int) ([]KBJobWebhookLog, error) {
	if kbID == 0 || jobID == 0 {
		return nil, errors.New("knowledgeBaseId/jobId required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.kbService.ensureKnowledgeBaseReadAccess(ctx, actor, kb); err != nil {
		return nil, err
	}
	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("fetch job: %w", err)
	}
	if job.KnowledgeBaseID != kb.ID {
		return nil, errors.New("job does not belong to knowledge base")
	}
	rows, err := s.repo.ListWebhookEvents(ctx, jobID, limit)
	if err != nil {
		return nil, fmt.Errorf("list webhook events: %w", err)
	}
	logs := make([]KBJobWebhookLog, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		payload := map[string]any{}
		if len(row.Payload) > 0 {
			if decoded, err := jsonToMap(row.Payload); err == nil {
				payload = decoded
			}
		}
		var progress *int
		if row.Progress != nil {
			value := *row.Progress
			progress = &value
		}
		logs = append(logs, KBJobWebhookLog{
			ID:              row.ID,
			JobID:           row.JobID,
			KnowledgeBaseID: row.KnowledgeBaseID,
			Status:          row.Status,
			Progress:        progress,
			ErrorMessage:    row.ErrorMessage,
			ErrorType:       row.ErrorType,
			Payload:         payload,
			RemoteAddr:      row.RemoteAddr,
			ReceivedAt:      row.ReceivedAt,
		})
	}
	return logs, nil
}

// ParseSource triggers connector parsing and stores a temporary session.
func (s *KBIngestionService) ParseSource(ctx context.Context, actor ActorContext, kbID uint64, connectorName string, params, credentials, metadata map[string]any) (*ParseSourceOutput, error) {
	if kbID == 0 {
		return nil, errors.New("knowledge base id required")
	}
	if s.registry == nil {
		return nil, errors.New("connector registry not configured")
	}
	connector, err := s.registry.Get(connectorName)
	if err != nil {
		return nil, err
	}

	if def, ok := s.connectorDefinition(connectorName); ok {
		params = mergeWithDefaults(def.DefaultParams, params)
		metadata = mergeWithDefaults(def.DefaultMetadata, metadata)
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.kbService.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, err
	}

	_ = s.repo.CleanupExpiredIngestionSessions(ctx, s.now())

	tree, err := connector.Parse(ctx, ingest.ParseRequest{
		KnowledgeBaseID: kb.ID,
		Params:          params,
		Credentials:     credentials,
		Metadata:        metadata,
		ActorID:         actor.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}
	if tree == nil {
		return nil, errors.New("connector returned empty parse tree")
	}

	token := uuid.NewString()
	payload := sessionPayload{
		Tree:         tree,
		Params:       params,
		Credentials:  credentials,
		Metadata:     metadata,
		Connector:    connectorName,
		SessionToken: token,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal session payload: %w", err)
	}

	session := &models.KBIngestionSession{
		KnowledgeBaseID: kb.ID,
		Connector:       connectorName,
		SessionToken:    token,
		Payload:         payloadJSON,
		ExpiresAt:       s.now().Add(s.sessionTTL),
	}
	if err := s.repo.CreateIngestionSession(ctx, session); err != nil {
		return nil, fmt.Errorf("store session: %w", err)
	}

	return &ParseSourceOutput{
		SessionToken: token,
		Tree:         tree,
	}, nil
}

// ImportSelections stages selected nodes into the knowledge base.
func (s *KBIngestionService) ImportSelections(ctx context.Context, actor ActorContext, kbID uint64, sessionToken string, selections []ImportSelection) ([]*models.KBDocument, []*models.KBJob, error) {
	if len(selections) == 0 {
		return nil, nil, errors.New("no selections provided")
	}
	if s.storage == nil {
		return nil, nil, errors.New("document storage not configured")
	}
	session, err := s.repo.GetIngestionSessionByToken(ctx, sessionToken)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch session: %w", err)
	}
	if session.KnowledgeBaseID != kbID {
		return nil, nil, errors.New("session knowledge base mismatch")
	}
	if session.ExpiresAt.Before(s.now()) {
		return nil, nil, errors.New("session expired")
	}

	var payload sessionPayload
	if err := json.Unmarshal(session.Payload, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode session payload: %w", err)
	}

	connector, err := s.registry.Get(payload.Connector)
	if err != nil {
		return nil, nil, err
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if err := s.kbService.ensureKnowledgeBaseWriteAccess(ctx, actor, kb); err != nil {
		return nil, nil, err
	}
	policy, err := s.buildPolicy(ctx, kb)
	if err != nil {
		return nil, nil, err
	}
	quota := policy.Quota
	remainingDocs := int64(quota.MaxDocumentsPerDay)
	if remainingDocs > 0 {
		remainingDocs -= quota.DocumentsUsedToday
		if remainingDocs < 0 {
			remainingDocs = 0
		}
		if int64(len(selections)) > remainingDocs {
			return nil, nil, fmt.Errorf("超出每日文档导入上限，今日还可导入 %d 个", remainingDocs)
		}
	}
	remainingJobs := int64(quota.MaxConcurrentJobs)
	if remainingJobs > 0 {
		remainingJobs -= quota.ActiveJobs
		if remainingJobs < 0 {
			remainingJobs = 0
		}
		if int64(len(selections)) > remainingJobs {
			return nil, nil, fmt.Errorf("当前有过多任务在运行，请稍后再试 (可用额度 %d)", remainingJobs)
		}
	}
	bytesBudget := quota.MaxBytesPerDay
	if bytesBudget < 0 {
		bytesBudget = 0
	}
	if bytesBudget > 0 {
		if quota.BytesUsedToday >= bytesBudget {
			return nil, nil, errors.New("今日文档体积配额已用完")
		}
		bytesBudget -= quota.BytesUsedToday
	}
	var bytesAllocated int64

	nodeIndex := make(map[string]*ingest.ParseNode)
	for _, root := range payload.Tree.Root {
		s.walkParseNodes(root, nodeIndex)
	}

	var documents []*models.KBDocument
	var jobs []*models.KBJob
	for _, selection := range selections {
		node, ok := nodeIndex[selection.NodeID]
		if !ok {
			return nil, nil, fmt.Errorf("node %s not found in session", selection.NodeID)
		}
		if stringsTrim(node.URI) == "" {
			return nil, nil, fmt.Errorf("node %s missing URI", selection.NodeID)
		}

		stageResult, err := connector.Stage(ctx, ingest.ParseItem{
			NodeID:      node.ID,
			URI:         node.URI,
			Params:      payload.Params,
			Metadata:    mergeMetadata(node.Metadata, selection.Metadata),
			Credentials: payload.Credentials,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("stage node %s: %w", selection.NodeID, err)
		}
		if stageResult == nil {
			return nil, nil, fmt.Errorf("stage node %s returned nil result", selection.NodeID)
		}

		estimatedSize := stageResult.SizeBytes
		if estimatedSize <= 0 && len(stageResult.RawContent) > 0 {
			estimatedSize = int64(len(stageResult.RawContent))
		}
		if estimatedSize <= 0 && node.SizeBytes > 0 {
			estimatedSize = node.SizeBytes
		}
		if bytesBudget > 0 && estimatedSize > 0 {
			if estimatedSize > bytesBudget-bytesAllocated {
				return nil, nil, errors.New("导入的文档体积超过剩余配额")
			}
		}

		reader := stageResult.Reader
		if reader == nil {
			if stringsTrim(stageResult.RawContent) == "" {
				return nil, nil, fmt.Errorf("stage node %s returned empty content", selection.NodeID)
			}
			reader = io.NopCloser(strings.NewReader(stageResult.RawContent))
		}

		saveRes, err := s.storage.Save(ctx, kb.ID, &storage.DocumentFile{
			Name:        deriveStageFileName(stageResult),
			Size:        stageResult.SizeBytes,
			ContentType: stageResult.ContentType,
			Reader:      reader,
		})
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return nil, nil, fmt.Errorf("persist staged document: %w", err)
		}
		if reader != nil {
			_ = reader.Close()
		}
		if bytesBudget > 0 {
			bytesAllocated += saveRes.Size
			if bytesAllocated > bytesBudget {
				_ = s.storage.Delete(ctx, saveRes.URI)
				return nil, nil, errors.New("导入的文档体积超过剩余配额")
			}
		}

		title := selection.Title
		if stringsTrim(title) == "" {
			title = stageResult.Title
		}
		if stringsTrim(title) == "" {
			title = node.Title
		}
		if stringsTrim(title) == "" {
			title = "Document"
		}

		docMeta := mergeMetadata(stageResult.Metadata, map[string]any{
			"originalUri": node.URI,
			"sourceUri":   stageResult.SourceURI,
		})

		doc, job, err := s.kbService.createDocumentInternal(ctx, actor, kb, CreateDocumentInput{
			KnowledgeBaseID:  kb.ID,
			Title:            title,
			SourceType:       payload.Connector,
			Source:           payload.Connector,
			OriginID:         stageResult.OriginID,
			Connector:        payload.Connector,
			StorageURI:       saveRes.URI,
			Checksum:         saveRes.Checksum,
			SizeBytes:        saveRes.Size,
			Metadata:         docMeta,
			EnqueueIngestion: true,
			RawContent:       stageResult.RawContent,
		})
		if err != nil {
			_ = s.storage.Delete(ctx, saveRes.URI)
			return nil, nil, err
		}

		documents = append(documents, doc)
		if job != nil {
			jobs = append(jobs, job)
			if remainingJobs > 0 {
				remainingJobs--
			}
		}
		if remainingDocs > 0 {
			remainingDocs--
		}
	}

	_ = s.repo.DeleteIngestionSession(ctx, session.ID)

	return documents, jobs, nil
}

func (s *KBIngestionService) resolveConnectorDefinitions() []ConnectorDefinition {
	if s.registry == nil {
		return nil
	}
	names := append([]string(nil), s.registry.List()...)
	sort.Strings(names)
	defs := make([]ConnectorDefinition, 0, len(names))
	for _, name := range names {
		normalized := normalizeConnectorName(name)
		definition, ok := s.connectors[normalized]
		if ok {
			cloned := cloneConnectorDefinition(definition)
			cloned.Name = name
			if strings.TrimSpace(cloned.DisplayName) == "" {
				cloned.DisplayName = displayNameFromKey(name)
			}
			defs = append(defs, cloned)
			continue
		}
		defs = append(defs, ConnectorDefinition{
			Name:        name,
			DisplayName: displayNameFromKey(name),
		})
	}
	return defs
}

func (s *KBIngestionService) updateDocumentFromJob(ctx context.Context, job *models.KBJob) {
	if job == nil || job.DocumentID == nil {
		return
	}
	doc, err := s.repo.GetDocumentByID(ctx, *job.DocumentID)
	if err != nil {
		return
	}
	status := strings.ToLower(stringsTrim(job.Status))
	switch status {
	case "failed":
		doc.ParseStatus = "failed"
		doc.ParseError = job.ErrorMessage
	case "completed", "success":
		doc.ParseStatus = "completed"
		doc.ParseError = ""
	case "queued", "parsing", "staging", "ingesting", "indexing":
		doc.ParseStatus = "processing"
		doc.ParseError = ""
	}
	_ = s.repo.UpdateDocument(ctx, doc)
}

func (s *KBIngestionService) connectorDefinition(name string) (ConnectorDefinition, bool) {
	if s == nil || s.connectors == nil {
		return ConnectorDefinition{}, false
	}
	def, ok := s.connectors[normalizeConnectorName(name)]
	if !ok {
		return ConnectorDefinition{}, false
	}
	return cloneConnectorDefinition(def), true
}

func (s *KBIngestionService) buildPolicy(ctx context.Context, kb *models.KBKnowledgeBase) (IngestionPolicy, error) {
	policy := IngestionPolicy{
		Quota: IngestionQuotaStatus{
			MaxDocumentsPerDay: s.quotaConfig.MaxDocumentsPerDay,
			MaxBytesPerDay:     s.quotaConfig.MaxBytesPerDay,
			MaxConcurrentJobs:  s.quotaConfig.MaxConcurrentJobs,
		},
		Retry: IngestionRetryStatus{
			MaxAttempts:    s.retryConfig.MaxAttempts,
			BackoffSeconds: append([]int(nil), s.retryConfig.BackoffSeconds...),
		},
	}

	if kb == nil {
		return policy, nil
	}

	now := s.now()
	windowStart := now.Add(-24 * time.Hour)

	if s.quotaConfig.MaxDocumentsPerDay > 0 {
		count, err := s.repo.CountDocumentsSince(ctx, kb.ID, windowStart)
		if err != nil {
			return policy, fmt.Errorf("compute document quota usage: %w", err)
		}
		policy.Quota.DocumentsUsedToday = count
	}
	if s.quotaConfig.MaxBytesPerDay > 0 {
		bytesUsed, err := s.repo.SumDocumentBytesSince(ctx, kb.ID, windowStart)
		if err != nil {
			return policy, fmt.Errorf("compute storage quota usage: %w", err)
		}
		policy.Quota.BytesUsedToday = bytesUsed
	}
	if s.quotaConfig.MaxConcurrentJobs > 0 {
		activeJobs, err := s.repo.CountActiveJobs(ctx, kb.ID)
		if err != nil {
			return policy, fmt.Errorf("compute active jobs: %w", err)
		}
		policy.Quota.ActiveJobs = activeJobs
	}

	return policy, nil
}

func normalizeConnectorName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func displayNameFromKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Connector"
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		for j := 1; j < len(runes); j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func cloneConnectorDefinition(def ConnectorDefinition) ConnectorDefinition {
	cloned := def
	cloned.Categories = cloneStringSlice(def.Categories)
	cloned.ParamsSchema = cloneFieldDefinitions(def.ParamsSchema)
	cloned.CredentialSchema = cloneFieldDefinitions(def.CredentialSchema)
	cloned.MetadataSchema = cloneFieldDefinitions(def.MetadataSchema)
	cloned.DefaultParams = cloneMap(def.DefaultParams)
	cloned.DefaultMetadata = cloneMap(def.DefaultMetadata)
	return cloned
}

func cloneFieldDefinitions(fields []ConnectorFieldDefinition) []ConnectorFieldDefinition {
	if len(fields) == 0 {
		return nil
	}
	result := make([]ConnectorFieldDefinition, len(fields))
	for i, field := range fields {
		result[i] = field
		if len(field.Options) > 0 {
			result[i].Options = cloneFieldOptions(field.Options)
		}
	}
	return result
}

func cloneFieldOptions(options []ConnectorFieldOption) []ConnectorFieldOption {
	if len(options) == 0 {
		return nil
	}
	result := make([]ConnectorFieldOption, len(options))
	copy(result, options)
	return result
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dup := make(map[string]any, len(src))
	for k, v := range src {
		dup[k] = v
	}
	return dup
}

func mergeWithDefaults(defaults, overrides map[string]any) map[string]any {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}
	result := make(map[string]any, len(defaults)+len(overrides))
	for k, v := range defaults {
		result[k] = v
	}
	for k, v := range overrides {
		result[k] = v
	}
	return result
}

func (s *KBIngestionService) walkParseNodes(node *ingest.ParseNode, index map[string]*ingest.ParseNode) {
	if node == nil {
		return
	}
	index[node.ID] = node
	for _, child := range node.Children {
		s.walkParseNodes(child, index)
	}
}

func (s *KBIngestionService) recordWebhookLog(ctx context.Context, job *models.KBJob, event IngestionWebhookEvent) error {
	if s == nil || s.repo == nil || job == nil {
		return nil
	}
	logEntry := &models.KBJobWebhookEvent{
		KnowledgeBaseID: job.KnowledgeBaseID,
		JobID:           job.ID,
		Status:          stringsTrim(event.Status),
		ErrorMessage:    event.ErrorMessage,
		ErrorType:       event.ErrorType,
		RemoteAddr:      stringsTrim(event.RemoteAddr),
		ReceivedAt:      s.now(),
	}
	if event.Progress != nil {
		value := *event.Progress
		logEntry.Progress = &value
	}
	if event.Payload != nil {
		if payloadJSON, err := mapToJSON(event.Payload); err == nil {
			logEntry.Payload = payloadJSON
		}
	}
	return s.repo.CreateWebhookEvent(ctx, logEntry)
}

func parseRemoteAddr(value string) (netip.Addr, error) {
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr, nil
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return netip.ParseAddr(value)
	}
	return netip.ParseAddr(host)
}

func parseCIDRs(values []string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, raw := range values {
		trimmed := stringsTrim(raw)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			return nil
		}
		if strings.Contains(trimmed, "/") {
			if prefix, err := netip.ParsePrefix(trimmed); err == nil {
				prefixes = append(prefixes, prefix)
			}
			continue
		}
		if addr, err := netip.ParseAddr(trimmed); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return prefixes
}

func mergeMetadata(primary map[string]any, overrides map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range primary {
		result[k] = v
	}
	for k, v := range overrides {
		result[k] = v
	}
	return result
}

func deriveStageFileName(res *ingest.StageResult) string {
	if res == nil {
		return uuid.NewString()
	}
	name := res.Title
	if stringsTrim(name) == "" {
		name = res.OriginID
	}
	if stringsTrim(name) == "" {
		return uuid.NewString()
	}
	return name
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
