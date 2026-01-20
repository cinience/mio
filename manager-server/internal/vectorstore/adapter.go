package vectorstore

import (
	"context"
	"errors"

	"manager-server/internal/models"
)

// ErrNotConfigured is returned when a vector store operation cannot be fulfilled.
var ErrNotConfigured = errors.New("vector store not configured")

// SearchResult represents a vector query hit.
type SearchResult struct {
	DocumentID    uint64          `json:"documentId"`
	DocumentTitle string          `json:"documentTitle,omitempty"`
	StorageURI    string          `json:"storageUri,omitempty"`
	SourceType    string          `json:"sourceType,omitempty"`
	ChunkID       uint64          `json:"chunkId"`
	Score         float64         `json:"score"`
	Content       string          `json:"content"`
	Meta          map[string]any  `json:"metadata,omitempty"`
	Chunk         *models.KBChunk `json:"-"`
}

// EmbeddingConfig captures runtime model configuration for embeddings.
type EmbeddingConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	Dimensions int
}

// EmbeddingConfigProvider resolves embedding settings for a knowledge base.
type EmbeddingConfigProvider interface {
	GetEmbeddingConfig(ctx context.Context, knowledgeBaseID uint64) (*EmbeddingConfig, error)
}

// Adapter defines CRUD operations for embeddings.
type Adapter interface {
	IndexChunks(ctx context.Context, knowledgeBaseID uint64, chunks []*models.KBChunk) error
	DeleteChunks(ctx context.Context, knowledgeBaseID uint64, chunkIDs []uint64) error
	Query(ctx context.Context, knowledgeBaseID uint64, query string, topK int) ([]SearchResult, error)
}
