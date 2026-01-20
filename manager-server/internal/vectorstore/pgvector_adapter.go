package vectorstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
	pgvector "github.com/pgvector/pgvector-go"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"manager-server/internal/config"
	"manager-server/internal/models"
)

const (
	pgVectorMetricCosine       = "cosine"
	pgVectorMetricEuclidean    = "euclidean"
	pgVectorMetricInnerProduct = "inner_product"
)

// PGVectorAdapter persists embeddings in PostgreSQL using the pgvector extension.
type PGVectorAdapter struct {
	db        *gorm.DB
	provider  EmbeddingConfigProvider
	metric    string
	batchSize int

	embedMu   sync.RWMutex
	embedders map[string]embedding.Embedder
}

// NewPGVectorAdapter constructs a pgvector-backed vector store.
func NewPGVectorAdapter(db *gorm.DB, provider EmbeddingConfigProvider, cfg config.PGVectorConfig) (*PGVectorAdapter, error) {
	if db == nil {
		return nil, errors.New("pgvector adapter requires a database handle")
	}
	metric := strings.TrimSpace(strings.ToLower(cfg.DistanceMetric))
	switch metric {
	case "", pgVectorMetricCosine:
		metric = pgVectorMetricCosine
	case pgVectorMetricEuclidean, "l2":
		metric = pgVectorMetricEuclidean
	case pgVectorMetricInnerProduct, "dot", "ip":
		metric = pgVectorMetricInnerProduct
	default:
		return nil, fmt.Errorf("unsupported pgvector distance metric: %s", cfg.DistanceMetric)
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 16
	}
	return &PGVectorAdapter{
		db:        db,
		provider:  provider,
		metric:    metric,
		batchSize: batchSize,
		embedders: make(map[string]embedding.Embedder),
	}, nil
}

// IndexChunks stores embeddings for the provided chunks.
func (a *PGVectorAdapter) IndexChunks(ctx context.Context, knowledgeBaseID uint64, chunks []*models.KBChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	if a.provider == nil {
		return ErrNotConfigured
	}
	cfg, err := a.provider.GetEmbeddingConfig(ctx, knowledgeBaseID)
	if err != nil {
		return fmt.Errorf("resolve embedding config: %w", err)
	}
	if cfg == nil {
		return ErrNotConfigured
	}
	embedder, err := a.embedderForConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("init embedder: %w", err)
	}

	vectors := make([][]float64, 0, len(chunks))
	for start := 0; start < len(chunks); start += a.batchSize {
		end := start + a.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]
		texts := make([]string, 0, len(batch))
		for _, chunk := range batch {
			if chunk == nil {
				continue
			}
			texts = append(texts, chunk.Content)
		}
		if len(texts) == 0 {
			continue
		}
		embedded, err := embedder.EmbedStrings(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed chunks: %w", err)
		}
		vectors = append(vectors, embedded...)
	}

	records := make([]*models.KBChunkEmbedding, 0, len(chunks))
	vectorIdx := 0
	for _, chunk := range chunks {
		if chunk == nil || chunk.ID == 0 {
			continue
		}
		if vectorIdx >= len(vectors) {
			break
		}
		vec := vectors[vectorIdx]
		vectorIdx++
		if len(vec) == 0 {
			continue
		}
		records = append(records, &models.KBChunkEmbedding{
			ChunkID:         chunk.ID,
			KnowledgeBaseID: knowledgeBaseID,
			Embedding:       pgvector.NewVector(float64ToFloat32(vec)),
			EmbeddingModel:  cfg.Model,
			Dimensions:      len(vec),
		})
	}
	if len(records) == 0 {
		return nil
	}

	return a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chunk_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"embedding", "embedding_model", "dimensions", "knowledge_base_id", "update_time"}),
	}).Create(&records).Error
}

// DeleteChunks removes embeddings for the given chunk IDs.
func (a *PGVectorAdapter) DeleteChunks(ctx context.Context, _ uint64, chunkIDs []uint64) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	return a.db.WithContext(ctx).Where("chunk_id IN ?", chunkIDs).Delete(&models.KBChunkEmbedding{}).Error
}

// Query performs a similarity search against pgvector.
func (a *PGVectorAdapter) Query(ctx context.Context, knowledgeBaseID uint64, query string, topK int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if a.provider == nil {
		return nil, ErrNotConfigured
	}
	cfg, err := a.provider.GetEmbeddingConfig(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding config: %w", err)
	}
	if cfg == nil {
		return nil, ErrNotConfigured
	}
	embedder, err := a.embedderForConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("init embedder: %w", err)
	}
	embeddings, err := embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, errors.New("query embedding unavailable")
	}
	queryVec := pgvector.NewVector(float64ToFloat32(embeddings[0]))
	if topK <= 0 {
		topK = 5
	}

	type resultRow struct {
		ChunkID    uint64
		DocumentID uint64
		Content    string
		Metadata   datatypes.JSON
		ManualEdit bool
		Distance   float64
	}

	distanceExpr := a.distanceExpression()
	sql := fmt.Sprintf(`
		SELECT c.id AS chunk_id,
		       c.document_id AS document_id,
		       c.content AS content,
		       c.metadata AS metadata,
		       c.manual_edit AS manual_edit,
		       %s AS distance
		FROM kb_chunk_embeddings e
		JOIN kb_chunks c ON c.id = e.chunk_id
		WHERE e.knowledge_base_id = ? AND c.deleted_at IS NULL
		ORDER BY distance %s
		LIMIT ?
	`, distanceExpr.expression, distanceExpr.order)

	rows := make([]resultRow, 0, topK)
	if err := a.db.WithContext(ctx).Raw(sql, queryVec, knowledgeBaseID, topK).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		score := a.distanceToScore(row.Distance)
		meta := jsonToMap(row.Metadata)
		meta["documentId"] = row.DocumentID
		meta["chunkId"] = row.ChunkID
		meta["score"] = score
		title := stringFromAny(meta["documentTitle"])
		storageURI := stringFromAny(meta["storageUri"])
		sourceType := stringFromAny(meta["sourceType"])
		chunk := &models.KBChunk{
			DocumentID: row.DocumentID,
			Content:    row.Content,
			Metadata:   row.Metadata,
			ManualEdit: row.ManualEdit,
		}
		chunk.BaseModel.ID = row.ChunkID

		results = append(results, SearchResult{
			DocumentID:    row.DocumentID,
			DocumentTitle: title,
			StorageURI:    storageURI,
			SourceType:    sourceType,
			ChunkID:       row.ChunkID,
			Score:         score,
			Content:       row.Content,
			Meta:          meta,
			Chunk:         chunk,
		})
	}
	return results, nil
}

type distanceSQL struct {
	expression string
	order      string
}

func (a *PGVectorAdapter) distanceExpression() distanceSQL {
	switch a.metric {
	case pgVectorMetricInnerProduct:
		return distanceSQL{
			expression: "e.embedding <#> ?",
			order:      "ASC",
		}
	case pgVectorMetricEuclidean:
		return distanceSQL{
			expression: "e.embedding <-> ?",
			order:      "ASC",
		}
	default:
		return distanceSQL{
			expression: "e.embedding <=> ?",
			order:      "ASC",
		}
	}
}

func (a *PGVectorAdapter) distanceToScore(distance float64) float64 {
	switch a.metric {
	case pgVectorMetricInnerProduct:
		return -distance
	case pgVectorMetricEuclidean:
		return 1 / (1 + distance)
	default:
		return 1 - distance
	}
}

func (a *PGVectorAdapter) embedderForConfig(ctx context.Context, cfg *EmbeddingConfig) (embedding.Embedder, error) {
	if cfg == nil {
		return nil, errors.New("embedding config required")
	}
	key := embeddingConfigCacheKey(cfg)
	a.embedMu.RLock()
	if emb, ok := a.embedders[key]; ok {
		a.embedMu.RUnlock()
		return emb, nil
	}
	a.embedMu.RUnlock()

	a.embedMu.Lock()
	defer a.embedMu.Unlock()
	if emb, ok := a.embedders[key]; ok {
		return emb, nil
	}
	emb, err := newOpenAIEmbedder(ctx, cfg)
	if err != nil {
		return nil, err
	}
	a.embedders[key] = emb
	return emb, nil
}

func newOpenAIEmbedder(ctx context.Context, cfg *EmbeddingConfig) (embedding.Embedder, error) {
	conf := &openai.EmbeddingConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
	}
	if cfg.Dimensions > 0 {
		d := cfg.Dimensions
		conf.Dimensions = &d
	}
	return openai.NewEmbedder(ctx, conf)
}

func embeddingConfigCacheKey(cfg *EmbeddingConfig) string {
	model := strings.TrimSpace(cfg.Model)
	base := strings.TrimSpace(cfg.BaseURL)
	key := strings.TrimSpace(cfg.APIKey)
	return fmt.Sprintf("%s|%s|%s|%d", model, base, key, cfg.Dimensions)
}

func float64ToFloat32(values []float64) []float32 {
	result := make([]float32, len(values))
	for i, v := range values {
		result[i] = float32(v)
	}
	return result
}

func jsonToMap(data datatypes.JSON) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// Ensure interface compliance.
var _ Adapter = (*PGVectorAdapter)(nil)
