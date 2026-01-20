package models

import (
	"time"

	pgvector "github.com/pgvector/pgvector-go"
	"gorm.io/datatypes"
)

// KBProject represents a knowledge base project with scoped ownership and visibility.
type KBProject struct {
	BaseModelUUID
	OwnerID    uint64         `gorm:"column:owner_id;not null;index:idx_kb_project_owner_name,priority:1" json:"ownerId"`
	Name       string         `gorm:"column:name;type:varchar(128);not null;index:idx_kb_project_owner_name,priority:2" json:"name"`
	Visibility string         `gorm:"column:visibility;type:varchar(32);default:'private';not null;index" json:"visibility"`
	Metadata   datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName overrides the table name for KBProject.
func (*KBProject) TableName() string {
	return "kb_projects"
}

// KBKnowledgeBase stores configuration for a specific knowledge base within a project.
type KBKnowledgeBase struct {
	BaseModel
	ProjectID         string         `gorm:"column:project_id;type:uuid;not null;index:idx_kb_project_name,priority:1" json:"projectId"`
	Name              string         `gorm:"column:name;type:varchar(128);not null;index:idx_kb_project_name,priority:2" json:"name"`
	Description       string         `gorm:"column:description;type:text" json:"description"`
	EmbeddingModel    string         `gorm:"column:embedding_model;type:varchar(128);not null" json:"embeddingModel"`
	EmbeddingParams   datatypes.JSON `gorm:"column:embedding_params;type:json" json:"embeddingParams"`
	RetrievalStrategy string         `gorm:"column:retrieval_strategy;type:varchar(64);default:'dense';not null" json:"retrievalStrategy"`
	RetrievalParams   datatypes.JSON `gorm:"column:retrieval_params;type:json" json:"retrievalParams"`
	Status            string         `gorm:"column:status;type:varchar(32);default:'active';not null;index" json:"status"`
	LastSyncedAt      *time.Time     `gorm:"column:last_synced_at;index" json:"lastSyncedAt,omitempty"`
}

// TableName overrides the table name for KBKnowledgeBase.
func (*KBKnowledgeBase) TableName() string {
	return "kb_knowledge_bases"
}

// KBDocument represents an uploaded document managed by a knowledge base.
type KBDocument struct {
	BaseModel
	KnowledgeBaseID uint64         `gorm:"column:knowledge_base_id;not null;index:idx_kb_doc_kb,priority:1;index:idx_kb_doc_kb_source,priority:1" json:"knowledgeBaseId"`
	Title           string         `gorm:"column:title;type:varchar(255);not null;index:idx_kb_doc_kb" json:"title"`
	SourceType      string         `gorm:"column:source_type;type:varchar(64);not null;index:idx_kb_doc_kb_source,priority:2" json:"sourceType"`
	Source          string         `gorm:"column:source;type:varchar(64);index:idx_kb_doc_kb_source,priority:3" json:"source"`
	OriginID        string         `gorm:"column:origin_id;type:varchar(255);index:idx_kb_doc_origin" json:"originId"`
	StorageURI      string         `gorm:"column:storage_uri;type:varchar(512);not null" json:"storageUri"`
	Checksum        string         `gorm:"column:checksum;type:varchar(128)" json:"checksum"`
	SizeBytes       int64          `gorm:"column:size_bytes;not null" json:"sizeBytes"`
	UploaderID      uint64         `gorm:"column:uploader_id;not null;index" json:"uploaderId"`
	ParseStatus     string         `gorm:"column:parse_status;type:varchar(32);default:'pending';not null;index:idx_kb_doc_status" json:"parseStatus"`
	ParseError      string         `gorm:"column:parse_error;type:text" json:"parseError"`
	Metadata        datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName overrides the table name for KBDocument.
func (*KBDocument) TableName() string {
	return "kb_documents"
}

// KBChunk stores chunked content for retrieval and editing workflows.
type KBChunk struct {
	BaseModel
	DocumentID        uint64         `gorm:"column:document_id;not null;index:idx_kb_chunk_doc_key,priority:1" json:"documentId"`
	ChunkKey          string         `gorm:"column:chunk_key;type:varchar(64);not null;index:idx_kb_chunk_doc_key,priority:2" json:"chunkKey"`
	Content           string         `gorm:"column:content;type:text;not null" json:"content"`
	Metadata          datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
	EmbeddingVectorID string         `gorm:"column:embedding_vector_id;type:varchar(128)" json:"embeddingVectorId"`
	ManualEdit        bool           `gorm:"column:manual_edit;not null;default:false;index" json:"manualEdit"`
	QAQuestion        string         `gorm:"column:qa_question;type:text" json:"qaQuestion"`
	QAAnswer          string         `gorm:"column:qa_answer;type:text" json:"qaAnswer"`
}

// TableName overrides the table name for KBChunk.
func (*KBChunk) TableName() string {
	return "kb_chunks"
}

// KBChunkEmbedding stores embedding vectors associated with chunks for pgvector queries.
type KBChunkEmbedding struct {
	ChunkID         uint64          `gorm:"column:chunk_id;primaryKey" json:"chunkId"`
	KnowledgeBaseID uint64          `gorm:"column:knowledge_base_id;not null;index:idx_kb_chunk_embedding_kb" json:"knowledgeBaseId"`
	Embedding       pgvector.Vector `gorm:"column:embedding;type:vector" json:"-"`
	EmbeddingModel  string          `gorm:"column:embedding_model;type:varchar(128);not null" json:"embeddingModel"`
	Dimensions      int             `gorm:"column:dimensions;not null" json:"dimensions"`
	CreateTime      time.Time       `gorm:"column:create_time;autoCreateTime" json:"createTime"`
	UpdateTime      time.Time       `gorm:"column:update_time;autoUpdateTime" json:"updateTime"`
}

// TableName overrides the table name for KBChunkEmbedding.
func (*KBChunkEmbedding) TableName() string {
	return "kb_chunk_embeddings"
}

// KBJob tracks asynchronous processing steps for knowledge base ingestion or maintenance.
type KBJob struct {
	BaseModel
	KnowledgeBaseID uint64         `gorm:"column:knowledge_base_id;not null;index:idx_kb_job_kb_status,priority:1" json:"knowledgeBaseId"`
	DocumentID      *uint64        `gorm:"column:document_id;index" json:"documentId,omitempty"`
	JobType         string         `gorm:"column:job_type;type:varchar(64);not null;index:idx_kb_job_type" json:"jobType"`
	Connector       string         `gorm:"column:connector;type:varchar(64);index" json:"connector"`
	Status          string         `gorm:"column:status;type:varchar(32);not null;index:idx_kb_job_kb_status,priority:2" json:"status"`
	Progress        int            `gorm:"column:progress;type:int;default:0" json:"progress"`
	ErrorMessage    string         `gorm:"column:error_message;type:text" json:"errorMessage"`
	ErrorType       string         `gorm:"column:error_type;type:varchar(64)" json:"errorType"`
    RetryCount      int            `gorm:"column:retry_count;type:int;default:0" json:"retryCount"`
    MaxRetries      int            `gorm:"column:max_retries;type:int;default:1" json:"maxRetries"`
    NextRetryAt     *time.Time     `gorm:"column:next_retry_at;index" json:"nextRetryAt,omitempty"`
    Payload         datatypes.JSON `gorm:"column:payload;type:json" json:"payload"`
	StartedAt       *time.Time     `gorm:"column:started_at;index" json:"startedAt,omitempty"`
	CompletedAt     *time.Time     `gorm:"column:completed_at;index" json:"completedAt,omitempty"`
}

// TableName overrides the table name for KBJob.
func (*KBJob) TableName() string {
    return "kb_jobs"
}

// KBJobWebhookEvent persists webhook callbacks for audit and debugging.
type KBJobWebhookEvent struct {
    BaseModel
    KnowledgeBaseID uint64         `gorm:"column:knowledge_base_id;not null;index" json:"knowledgeBaseId"`
    JobID           uint64         `gorm:"column:job_id;not null;index" json:"jobId"`
    Status          string         `gorm:"column:status;type:varchar(64)" json:"status"`
    Progress        *int           `gorm:"column:progress" json:"progress,omitempty"`
    ErrorMessage    string         `gorm:"column:error_message;type:text" json:"errorMessage"`
    ErrorType       string         `gorm:"column:error_type;type:varchar(64)" json:"errorType"`
    Payload         datatypes.JSON `gorm:"column:payload;type:json" json:"payload"`
    RemoteAddr      string         `gorm:"column:remote_addr;type:varchar(128)" json:"remoteAddr"`
    ReceivedAt      time.Time      `gorm:"column:received_at;index" json:"receivedAt"`
}

// TableName overrides the table name for KBJobWebhookEvent.
func (*KBJobWebhookEvent) TableName() string {
    return "kb_job_webhook_events"
}

// KBPermission grants additional access to users beyond project ownership.
type KBPermission struct {
	BaseModel
	KnowledgeBaseID uint64 `gorm:"column:knowledge_base_id;not null;uniqueIndex:idx_kb_permission_unique,priority:1" json:"knowledgeBaseId"`
	UserID          uint64 `gorm:"column:user_id;not null;uniqueIndex:idx_kb_permission_unique,priority:2" json:"userId"`
	Role            string `gorm:"column:role;type:varchar(32);not null" json:"role"`
}

// TableName overrides the table name for KBPermission.
func (*KBPermission) TableName() string {
	return "kb_permissions"
}

// KBIngestionSession stores cached parse trees for user selection before import.
type KBIngestionSession struct {
	BaseModelUUID
	KnowledgeBaseID uint64         `gorm:"column:knowledge_base_id;not null;index" json:"knowledgeBaseId"`
	Connector       string         `gorm:"column:connector;type:varchar(64);not null;index" json:"connector"`
	SessionToken    string         `gorm:"column:session_token;type:varchar(64);not null;uniqueIndex" json:"sessionToken"`
	Payload         datatypes.JSON `gorm:"column:payload;type:json;not null" json:"payload"`
	ExpiresAt       time.Time      `gorm:"column:expires_at;index" json:"expiresAt"`
}

// TableName overrides the table name for KBIngestionSession.
func (*KBIngestionSession) TableName() string {
	return "kb_ingestion_sessions"
}

// KBAgentProjectMount maps agents to projects and stores capability metadata.
type KBAgentProjectMount struct {
	BaseModel
	AgentID      uint64         `gorm:"column:agent_id;not null;uniqueIndex:idx_kb_agent_project_unique,priority:1" json:"agentId"`
	ProjectID    string         `gorm:"column:project_id;type:uuid;not null;uniqueIndex:idx_kb_agent_project_unique,priority:2" json:"projectId"`
	Capabilities datatypes.JSON `gorm:"column:capabilities;type:json" json:"capabilities"`
}

// TableName overrides the table name for KBAgentProjectMount.
func (*KBAgentProjectMount) TableName() string {
	return "kb_agent_project_mounts"
}
