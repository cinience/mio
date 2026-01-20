package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"manager-server/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// KBProjectFilter encapsulates filters for querying knowledge base projects.
type KBProjectFilter struct {
	OwnerID    *uint64
	Visibility []string
	NameLike   string
}

// KBKnowledgeBaseFilter encapsulates filters for knowledge base lookups.
type KBKnowledgeBaseFilter struct {
	ProjectID string
	Status    []string
	NameLike  string
}

// KBDocumentFilter encapsulates filters for documents within a knowledge base.
type KBDocumentFilter struct {
	KnowledgeBaseID uint64
	ParseStatus     []string
	UploaderID      *uint64
	SourceTypes     []string
	Sources         []string
	OriginIDs       []string
}

// KBChunkFilter encapsulates filters for chunk queries.
type KBChunkFilter struct {
	DocumentID uint64
	ManualEdit *bool
}

// KBJobFilter encapsulates filters for job queries.
type KBJobFilter struct {
	KnowledgeBaseID uint64
	JobTypes        []string
	Status          []string
}

// KBRepository defines access methods for the knowledge base domain.
type KBRepository interface {
	// Project operations
	CreateProject(ctx context.Context, project *models.KBProject) error
	UpdateProject(ctx context.Context, project *models.KBProject) error
	SoftDeleteProject(ctx context.Context, id string) error
	GetProjectByID(ctx context.Context, id string) (*models.KBProject, error)
	ListProjects(ctx context.Context, filter KBProjectFilter, limit, offset int) ([]*models.KBProject, int64, error)

	// Knowledge base operations
	CreateKnowledgeBase(ctx context.Context, kb *models.KBKnowledgeBase) error
	UpdateKnowledgeBase(ctx context.Context, kb *models.KBKnowledgeBase) error
	GetKnowledgeBaseByID(ctx context.Context, id uint64) (*models.KBKnowledgeBase, error)
	ListKnowledgeBases(ctx context.Context, filter KBKnowledgeBaseFilter, limit, offset int) ([]*models.KBKnowledgeBase, int64, error)
	ArchiveKnowledgeBase(ctx context.Context, id uint64, status string) error

	// Document operations
	CreateDocument(ctx context.Context, doc *models.KBDocument) error
	GetDocumentByID(ctx context.Context, id uint64) (*models.KBDocument, error)
	UpdateDocument(ctx context.Context, doc *models.KBDocument) error
	ListDocuments(ctx context.Context, filter KBDocumentFilter, limit, offset int) ([]*models.KBDocument, int64, error)
	CountDocumentsSince(ctx context.Context, knowledgeBaseID uint64, since time.Time) (int64, error)
	SumDocumentBytesSince(ctx context.Context, knowledgeBaseID uint64, since time.Time) (int64, error)

	// Chunk operations
	CreateChunks(ctx context.Context, chunks []*models.KBChunk) error
	GetChunkByID(ctx context.Context, id uint64) (*models.KBChunk, error)
	UpdateChunk(ctx context.Context, chunk *models.KBChunk) error
	ListChunks(ctx context.Context, filter KBChunkFilter, limit, offset int) ([]*models.KBChunk, int64, error)
	SearchChunksByContent(ctx context.Context, knowledgeBaseID uint64, query string, limit int) ([]*models.KBChunk, error)

	// Job operations
	CreateJob(ctx context.Context, job *models.KBJob) error
	GetJobByID(ctx context.Context, id uint64) (*models.KBJob, error)
	UpdateJob(ctx context.Context, job *models.KBJob) error
	ListJobs(ctx context.Context, filter KBJobFilter, limit, offset int) ([]*models.KBJob, int64, error)
	JobStatusSummary(ctx context.Context, knowledgeBaseID uint64) (map[string]int64, error)
	CountActiveJobs(ctx context.Context, knowledgeBaseID uint64) (int64, error)
	CreateWebhookEvent(ctx context.Context, event *models.KBJobWebhookEvent) error
	ListWebhookEvents(ctx context.Context, jobID uint64, limit int) ([]*models.KBJobWebhookEvent, error)

	// Ingestion session operations
	CreateIngestionSession(ctx context.Context, session *models.KBIngestionSession) error
	GetIngestionSessionByToken(ctx context.Context, token string) (*models.KBIngestionSession, error)
	DeleteIngestionSession(ctx context.Context, id string) error
	CleanupExpiredIngestionSessions(ctx context.Context, now time.Time) error

	// Permission operations
	UpsertPermission(ctx context.Context, permission *models.KBPermission) error
	RemovePermission(ctx context.Context, knowledgeBaseID, userID uint64) error
	ListPermissions(ctx context.Context, knowledgeBaseID uint64) ([]*models.KBPermission, error)
	GetPermission(ctx context.Context, knowledgeBaseID, userID uint64) (*models.KBPermission, error)

	// Agent mount operations
	MountAgentProject(ctx context.Context, mount *models.KBAgentProjectMount) error
	UnmountAgentProject(ctx context.Context, agentID uint64, projectID string) error
	ListAgentProjectMounts(ctx context.Context, agentID uint64) ([]*models.KBAgentProjectMount, error)
	ListProjectAgentMounts(ctx context.Context, projectID string) ([]*models.KBAgentProjectMount, error)
}

type kbRepository struct {
	db *gorm.DB
}

// NewKBRepository creates a new knowledge base repository backed by GORM.
func NewKBRepository(db *gorm.DB) KBRepository {
	return &kbRepository{db: db}
}

// CreateProject inserts a new knowledge base project.
func (r *kbRepository) CreateProject(ctx context.Context, project *models.KBProject) error {
	return r.db.WithContext(ctx).Create(project).Error
}

// UpdateProject updates an existing project.
func (r *kbRepository) UpdateProject(ctx context.Context, project *models.KBProject) error {
	return r.db.WithContext(ctx).Save(project).Error
}

// SoftDeleteProject soft deletes a project by ID.
func (r *kbRepository) SoftDeleteProject(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.KBProject{}).Error
}

// GetProjectByID fetches a project by its ID.
func (r *kbRepository) GetProjectByID(ctx context.Context, id string) (*models.KBProject, error) {
	var project models.KBProject
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// ListProjects returns projects using the supplied filter and pagination.
func (r *kbRepository) ListProjects(ctx context.Context, filter KBProjectFilter, limit, offset int) ([]*models.KBProject, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.KBProject{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if len(filter.Visibility) > 0 {
		query = query.Where("visibility IN ?", filter.Visibility)
	}
	if filter.NameLike != "" {
		query = query.Where("name LIKE ?", "%"+filter.NameLike+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var projects []*models.KBProject
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&projects).Error
	return projects, total, err
}

// CreateKnowledgeBase inserts a new knowledge base.
func (r *kbRepository) CreateKnowledgeBase(ctx context.Context, kb *models.KBKnowledgeBase) error {
	return r.db.WithContext(ctx).Create(kb).Error
}

// UpdateKnowledgeBase persists changes to a knowledge base.
func (r *kbRepository) UpdateKnowledgeBase(ctx context.Context, kb *models.KBKnowledgeBase) error {
	return r.db.WithContext(ctx).Save(kb).Error
}

// GetKnowledgeBaseByID fetches a knowledge base by ID.
func (r *kbRepository) GetKnowledgeBaseByID(ctx context.Context, id uint64) (*models.KBKnowledgeBase, error) {
	var kb models.KBKnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&kb).Error; err != nil {
		return nil, err
	}
	return &kb, nil
}

// ListKnowledgeBases returns knowledge bases using the supplied filter and pagination.
func (r *kbRepository) ListKnowledgeBases(ctx context.Context, filter KBKnowledgeBaseFilter, limit, offset int) ([]*models.KBKnowledgeBase, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.KBKnowledgeBase{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.ProjectID != "" {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}
	if filter.NameLike != "" {
		query = query.Where("name LIKE ?", "%"+filter.NameLike+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var result []*models.KBKnowledgeBase
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&result).Error
	return result, total, err
}

// ArchiveKnowledgeBase updates the status of a knowledge base (e.g., archived).
func (r *kbRepository) ArchiveKnowledgeBase(ctx context.Context, id uint64, status string) error {
	return r.db.WithContext(ctx).
		Model(&models.KBKnowledgeBase{}).
		Where("id = ?", id).
		Update("status", status).Error
}

// CreateDocument inserts a new document linked to a knowledge base.
func (r *kbRepository) CreateDocument(ctx context.Context, doc *models.KBDocument) error {
	return r.db.WithContext(ctx).Create(doc).Error
}

// GetDocumentByID fetches a document by primary key.
func (r *kbRepository) GetDocumentByID(ctx context.Context, id uint64) (*models.KBDocument, error) {
	var doc models.KBDocument
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

// UpdateDocument updates metadata of an existing document.
func (r *kbRepository) UpdateDocument(ctx context.Context, doc *models.KBDocument) error {
	return r.db.WithContext(ctx).Save(doc).Error
}

// ListDocuments returns documents for a knowledge base using the supplied filter.
func (r *kbRepository) ListDocuments(ctx context.Context, filter KBDocumentFilter, limit, offset int) ([]*models.KBDocument, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.KBDocument{}).Where("knowledge_base_id = ?", filter.KnowledgeBaseID)

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if len(filter.ParseStatus) > 0 {
		query = query.Where("parse_status IN ?", filter.ParseStatus)
	}
	if filter.UploaderID != nil {
		query = query.Where("uploader_id = ?", *filter.UploaderID)
	}
	if len(filter.SourceTypes) > 0 {
		query = query.Where("source_type IN ?", filter.SourceTypes)
	}
	if len(filter.Sources) > 0 {
		query = query.Where("source IN ?", filter.Sources)
	}
	if len(filter.OriginIDs) > 0 {
		query = query.Where("origin_id IN ?", filter.OriginIDs)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var docs []*models.KBDocument
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&docs).Error
	return docs, total, err
}

func (r *kbRepository) CountDocumentsSince(ctx context.Context, knowledgeBaseID uint64, since time.Time) (int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.KBDocument{}).
		Where("knowledge_base_id = ?", knowledgeBaseID)
	if !since.IsZero() {
		query = query.Where("create_time >= ?", since)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *kbRepository) SumDocumentBytesSince(ctx context.Context, knowledgeBaseID uint64, since time.Time) (int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.KBDocument{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("knowledge_base_id = ?", knowledgeBaseID)
	if !since.IsZero() {
		query = query.Where("create_time >= ?", since)
	}
	var total sql.NullInt64
	if err := query.Row().Scan(&total); err != nil {
		return 0, err
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}

// CreateChunks performs a bulk insert of chunks.
func (r *kbRepository) CreateChunks(ctx context.Context, chunks []*models.KBChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&chunks).Error
}

// GetChunkByID fetches a chunk by its ID.
func (r *kbRepository) GetChunkByID(ctx context.Context, id uint64) (*models.KBChunk, error) {
	var chunk models.KBChunk
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&chunk).Error; err != nil {
		return nil, err
	}
	return &chunk, nil
}

// UpdateChunk updates an individual chunk.
func (r *kbRepository) UpdateChunk(ctx context.Context, chunk *models.KBChunk) error {
	return r.db.WithContext(ctx).Save(chunk).Error
}

// ListChunks retrieves chunks based on supplied filters.
func (r *kbRepository) ListChunks(ctx context.Context, filter KBChunkFilter, limit, offset int) ([]*models.KBChunk, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.KBChunk{}).Where("document_id = ?", filter.DocumentID)
	if filter.ManualEdit != nil {
		query = query.Where("manual_edit = ?", *filter.ManualEdit)
	}

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var chunks []*models.KBChunk
	err := query.Order("create_time ASC").Limit(limit).Offset(offset).Find(&chunks).Error
	return chunks, total, err
}

func (r *kbRepository) SearchChunksByContent(ctx context.Context, knowledgeBaseID uint64, query string, limit int) ([]*models.KBChunk, error) {
	if knowledgeBaseID == 0 {
		return nil, errors.New("knowledge base id is required")
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	pattern := "%" + trimmed + "%"

	var chunks []*models.KBChunk
	err := r.db.WithContext(ctx).
		Table("kb_chunks AS c").
		Joins("JOIN kb_documents d ON c.document_id = d.id").
		Where("d.knowledge_base_id = ?", knowledgeBaseID).
		Where("c.content LIKE ?", pattern).
		Order("c.id ASC").
		Limit(limit).
		Find(&chunks).Error
	if err != nil {
		return nil, err
	}
	return chunks, nil
}

// CreateJob creates a new processing job.
func (r *kbRepository) CreateJob(ctx context.Context, job *models.KBJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

// GetJobByID fetches a job by its ID.
func (r *kbRepository) GetJobByID(ctx context.Context, id uint64) (*models.KBJob, error) {
	var jb models.KBJob
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&jb).Error; err != nil {
		return nil, err
	}
	return &jb, nil
}

// UpdateJob updates job details (status, progress, timestamps).
func (r *kbRepository) UpdateJob(ctx context.Context, job *models.KBJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

// ListJobs lists jobs for a knowledge base.
func (r *kbRepository) ListJobs(ctx context.Context, filter KBJobFilter, limit, offset int) ([]*models.KBJob, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.KBJob{}).Where("knowledge_base_id = ?", filter.KnowledgeBaseID)
	if len(filter.JobTypes) > 0 {
		query = query.Where("job_type IN ?", filter.JobTypes)
	}
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var jobs []*models.KBJob
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&jobs).Error
	return jobs, total, err
}

func (r *kbRepository) JobStatusSummary(ctx context.Context, knowledgeBaseID uint64) (map[string]int64, error) {
	if knowledgeBaseID == 0 {
		return nil, errors.New("knowledge base id is required")
	}
	rows := []struct {
		Status string
		Count  int64
	}{}
	if err := r.db.WithContext(ctx).
		Model(&models.KBJob{}).
		Select("status, COUNT(*) AS count").
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.Status] = row.Count
	}
	return result, nil
}

func (r *kbRepository) CountActiveJobs(ctx context.Context, knowledgeBaseID uint64) (int64, error) {
	activeStatuses := []string{"queued", "parsing", "staging", "ingesting", "indexing", "retrying"}
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.KBJob{}).
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Where("status IN ?", activeStatuses).
		Count(&count).Error
	return count, err
}

func (r *kbRepository) CreateWebhookEvent(ctx context.Context, event *models.KBJobWebhookEvent) error {
	if event == nil {
		return errors.New("webhook event is nil")
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *kbRepository) ListWebhookEvents(ctx context.Context, jobID uint64, limit int) ([]*models.KBJobWebhookEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []*models.KBJobWebhookEvent
	err := r.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("received_at DESC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// CreateIngestionSession persists a new ingestion session for multi-step imports.
func (r *kbRepository) CreateIngestionSession(ctx context.Context, session *models.KBIngestionSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

// GetIngestionSessionByToken fetches an ingestion session by its session token.
func (r *kbRepository) GetIngestionSessionByToken(ctx context.Context, token string) (*models.KBIngestionSession, error) {
	var session models.KBIngestionSession
	if err := r.db.WithContext(ctx).Where("session_token = ?", token).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteIngestionSession removes a cached ingestion session.
func (r *kbRepository) DeleteIngestionSession(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.KBIngestionSession{}).Error
}

// CleanupExpiredIngestionSessions deletes sessions whose TTL has elapsed.
func (r *kbRepository) CleanupExpiredIngestionSessions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&models.KBIngestionSession{}).Error
}

// UpsertPermission creates or updates a knowledge base permission.
func (r *kbRepository) UpsertPermission(ctx context.Context, permission *models.KBPermission) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "knowledge_base_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"role": permission.Role, "update_time": time.Now()}),
		}).
		Create(permission).Error
}

// RemovePermission deletes a permission entry.
func (r *kbRepository) RemovePermission(ctx context.Context, knowledgeBaseID, userID uint64) error {
	return r.db.WithContext(ctx).
		Where("knowledge_base_id = ? AND user_id = ?", knowledgeBaseID, userID).
		Delete(&models.KBPermission{}).Error
}

// ListPermissions returns all permissions for a knowledge base.
func (r *kbRepository) ListPermissions(ctx context.Context, knowledgeBaseID uint64) ([]*models.KBPermission, error) {
	var permissions []*models.KBPermission
	err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Find(&permissions).Error
	return permissions, err
}

// GetPermission retrieves a permission entry for a user, returning nil when absent.
func (r *kbRepository) GetPermission(ctx context.Context, knowledgeBaseID, userID uint64) (*models.KBPermission, error) {
	var permission models.KBPermission
	err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ? AND user_id = ?", knowledgeBaseID, userID).
		First(&permission).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &permission, nil
}

// MountAgentProject inserts a new agent-project mount.
func (r *kbRepository) MountAgentProject(ctx context.Context, mount *models.KBAgentProjectMount) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "agent_id"}, {Name: "project_id"}},
			DoNothing: true,
		}).
		Create(mount).Error
}

// UnmountAgentProject removes an agent-project mount.
func (r *kbRepository) UnmountAgentProject(ctx context.Context, agentID uint64, projectID string) error {
	return r.db.WithContext(ctx).
		Where("agent_id = ? AND project_id = ?", agentID, projectID).
		Delete(&models.KBAgentProjectMount{}).Error
}

// ListAgentProjectMounts lists all project mounts for an agent.
func (r *kbRepository) ListAgentProjectMounts(ctx context.Context, agentID uint64) ([]*models.KBAgentProjectMount, error) {
	var mounts []*models.KBAgentProjectMount
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Find(&mounts).Error
	return mounts, err
}

// ListProjectAgentMounts lists all mounts for a given project.
func (r *kbRepository) ListProjectAgentMounts(ctx context.Context, projectID string) ([]*models.KBAgentProjectMount, error) {
	var mounts []*models.KBAgentProjectMount
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Find(&mounts).Error
	return mounts, err
}
