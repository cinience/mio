package vectorstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"

	"manager-server/internal/config"
	"manager-server/internal/models"
	"manager-server/internal/rag"
)

// ES8Adapter provides a vectorstore backed by the go-rag pipeline on Elasticsearch 8.
type ES8Adapter struct {
	client           *elasticsearch.Client
	indexName        string
	provider         EmbeddingConfigProvider
	defaultEmbedding EmbeddingConfig

	mu        sync.RWMutex
	pipelines map[string]*rag.Pipeline

	scoreThreshold float64
	defaultTopK    int
}

// NewES8Adapter constructs an ES8Adapter when Elasticsearch support is enabled.
func NewES8Adapter(ctx context.Context, cfg config.ES8Config, provider EmbeddingConfigProvider) (*ES8Adapter, error) {
	if !cfg.Enabled {
		return nil, errors.New("rag disabled")
	}
	if strings.TrimSpace(cfg.Index) == "" {
		return nil, errors.New("rag index not configured")
	}

	esCfg := elasticsearch.Config{
		Addresses: cfg.Elasticsearch.Addresses,
		Username:  cfg.Elasticsearch.Username,
		Password:  cfg.Elasticsearch.Password,
		APIKey:    cfg.Elasticsearch.APIKey,
	}
	if cfg.Elasticsearch.CACertPath != "" {
		cert, err := os.ReadFile(cfg.Elasticsearch.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("load elasticsearch CA cert: %w", err)
		}
		esCfg.CACert = cert
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("create elasticsearch client: %w", err)
	}

	defaultEmbedding := EmbeddingConfig{
		APIKey:     cfg.Models.APIKey,
		BaseURL:    cfg.Models.BaseURL,
		Model:      cfg.Models.EmbeddingModel,
		Dimensions: 0,
	}

	if provider == nil {
		provider = &staticEmbeddingProvider{defaultCfg: defaultEmbedding}
	}

	threshold := cfg.ScoreThreshold
	if threshold <= 0 {
		threshold = 1.1
	}
	topK := cfg.DefaultTopK
	if topK <= 0 {
		topK = 5
	}

	adapter := &ES8Adapter{
		client:           client,
		indexName:        cfg.Index,
		provider:         provider,
		defaultEmbedding: defaultEmbedding,
		pipelines:        make(map[string]*rag.Pipeline),
		scoreThreshold:   threshold,
		defaultTopK:      topK,
	}

	// Ensure the default configuration is ready so that index mappings exist.
	if _, err := adapter.pipelineForConfig(ctx, defaultEmbedding); err != nil {
		return nil, err
	}

	return adapter, nil
}

// IndexChunks pushes chunk content into the RAG pipeline for a knowledge base.
func (a *ES8Adapter) IndexChunks(ctx context.Context, knowledgeBaseID uint64, chunks []*models.KBChunk) error {
	if len(chunks) == 0 {
		return nil
	}

	pipeline, err := a.pipelineForKnowledgeBase(ctx, knowledgeBaseID)
	if err != nil {
		return err
	}

	docs := make([]*schema.Document, 0, len(chunks))
	knowledgeName := a.knowledgeName(knowledgeBaseID)

	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		meta := map[string]any{}
		extension := map[string]any{
			"chunkId":         chunk.ID,
			"documentId":      chunk.DocumentID,
			"knowledgeBaseId": knowledgeBaseID,
			"manualEdit":      chunk.ManualEdit,
		}
		if len(chunk.Metadata) > 0 {
			var stored map[string]any
			if err := json.Unmarshal(chunk.Metadata, &stored); err == nil {
				extension["metadata"] = stored
				if title, ok := stored["documentTitle"].(string); ok && strings.TrimSpace(title) != "" {
					extension["documentTitle"] = title
				}
				if uri, ok := stored["storageUri"].(string); ok && strings.TrimSpace(uri) != "" {
					extension["storageUri"] = uri
				}
				if source, ok := stored["sourceType"].(string); ok && strings.TrimSpace(source) != "" {
					extension["sourceType"] = source
				}
			}
		}
		if chunk.QAQuestion != "" || chunk.QAAnswer != "" {
			extension["qaSeed"] = map[string]string{
				"question": chunk.QAQuestion,
				"answer":   chunk.QAAnswer,
			}
		}
		extBytes, _ := json.Marshal(extension)
		meta[rag.FieldExtra] = string(extBytes)
		meta["_source"] = fmt.Sprintf("kb:%d", knowledgeBaseID)

		doc := &schema.Document{
			ID:       strconv.FormatUint(chunk.ID, 10),
			Content:  chunk.Content,
			MetaData: meta,
		}
		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return nil
	}

	if _, err := pipeline.IndexDocuments(ctx, knowledgeName, docs); err != nil {
		return fmt.Errorf("rag index: %w", err)
	}
	return nil
}

// DeleteChunks removes chunk embeddings from the vector store.
func (a *ES8Adapter) DeleteChunks(ctx context.Context, knowledgeBaseID uint64, chunkIDs []uint64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	pipeline, err := a.pipelineForKnowledgeBase(ctx, knowledgeBaseID)
	if err != nil {
		return err
	}

	for _, id := range chunkIDs {
		if id == 0 {
			continue
		}
		if err := pipeline.DeleteDocument(ctx, strconv.FormatUint(id, 10)); err != nil {
			return fmt.Errorf("delete document %d: %w", id, err)
		}
	}
	return nil
}

// Query runs a semantic retrieval against the knowledge base.
func (a *ES8Adapter) Query(ctx context.Context, knowledgeBaseID uint64, query string, topK int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query cannot be empty")
	}
	if topK <= 0 {
		topK = a.defaultTopK
	}

	pipeline, err := a.pipelineForKnowledgeBase(ctx, knowledgeBaseID)
	if err != nil {
		return nil, err
	}

	docs, err := pipeline.Retrieve(ctx, a.knowledgeName(knowledgeBaseID), query, topK)
	if err != nil {
		return nil, fmt.Errorf("rag retrieve: %w", err)
	}

	results := make([]SearchResult, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		if doc.Score() < a.scoreThreshold {
			continue
		}
		chunkID, err := strconv.ParseUint(doc.ID, 10, 64)
		if err != nil {
			chunkID = 0
		}
		meta := parseMetadata(doc.MetaData[rag.FieldExtra])
		result := SearchResult{
			ChunkID:       chunkID,
			DocumentID:    uint64FromAny(meta["documentId"]),
			DocumentTitle: stringFromAny(meta["documentTitle"]),
			StorageURI:    stringFromAny(meta["storageUri"]),
			SourceType:    stringFromAny(meta["sourceType"]),
			Score:         doc.Score(),
			Content:       doc.Content,
			Meta:          meta,
		}
		results = append(results, result)
	}
	return results, nil
}

func (a *ES8Adapter) pipelineForKnowledgeBase(ctx context.Context, knowledgeBaseID uint64) (*rag.Pipeline, error) {
	override, err := a.provider.GetEmbeddingConfig(ctx, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	cfg := a.mergeEmbeddingConfig(override)
	return a.pipelineForConfig(ctx, cfg)
}

func (a *ES8Adapter) mergeEmbeddingConfig(override *EmbeddingConfig) EmbeddingConfig {
	cfg := a.defaultEmbedding
	if override == nil {
		return cfg
	}
	if strings.TrimSpace(override.APIKey) != "" {
		cfg.APIKey = override.APIKey
	}
	if strings.TrimSpace(override.BaseURL) != "" {
		cfg.BaseURL = override.BaseURL
	}
	if strings.TrimSpace(override.Model) != "" {
		cfg.Model = override.Model
	}
	if override.Dimensions > 0 {
		cfg.Dimensions = override.Dimensions
	}
	return cfg
}

func (a *ES8Adapter) pipelineForConfig(ctx context.Context, embedCfg EmbeddingConfig) (*rag.Pipeline, error) {
	if embedCfg.Dimensions <= 0 {
		embedCfg.Dimensions = rag.DefaultEmbeddingDims
	}
	key := embeddingConfigKey(embedCfg)

	a.mu.RLock()
	pipeline, ok := a.pipelines[key]
	a.mu.RUnlock()
	if ok && pipeline != nil {
		return pipeline, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if pipeline, ok = a.pipelines[key]; ok && pipeline != nil {
		return pipeline, nil
	}

	cfg := &rag.Config{
		Client:         a.client,
		IndexName:      a.indexName,
		APIKey:         embedCfg.APIKey,
		BaseURL:        embedCfg.BaseURL,
		EmbeddingModel: embedCfg.Model,
		EmbeddingDims:  embedCfg.Dimensions,
	}

	pipeline, err := rag.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialise rag pipeline: %w", err)
	}
	if a.pipelines == nil {
		a.pipelines = make(map[string]*rag.Pipeline)
	}
	a.pipelines[key] = pipeline
	return pipeline, nil
}

func embeddingConfigKey(cfg EmbeddingConfig) string {
	return fmt.Sprintf("%s|%s|%s|%d",
		strings.TrimSpace(cfg.Model),
		strings.TrimSpace(cfg.BaseURL),
		strings.TrimSpace(cfg.APIKey),
		cfg.Dimensions,
	)
}

func (a *ES8Adapter) knowledgeName(kbID uint64) string {
	return fmt.Sprintf("manager_kb_%d", kbID)
}

func parseMetadata(raw any) map[string]any {
	meta := make(map[string]any)
	rawStr, ok := raw.(string)
	if !ok || strings.TrimSpace(rawStr) == "" {
		return meta
	}
	var ext map[string]any
	if err := json.Unmarshal([]byte(rawStr), &ext); err != nil {
		return meta
	}
	for key, value := range ext {
		if key == "_extension" {
			if nested, ok := value.(map[string]any); ok {
				for nk, nv := range nested {
					meta[nk] = coerceNumber(nv)
				}
				continue
			}
		}
		meta[key] = coerceNumber(value)
	}
	return meta
}

func coerceNumber(v any) any {
	switch val := v.(type) {
	case float64:
		if val == float64(uint64(val)) {
			return uint64(val)
		}
		return val
	case map[string]any:
		for k, nested := range val {
			val[k] = coerceNumber(nested)
		}
	case []any:
		for i, nested := range val {
			val[i] = coerceNumber(nested)
		}
	}
	return v
}

func uint64FromAny(v any) uint64 {
	switch val := v.(type) {
	case uint64:
		return val
	case float64:
		return uint64(val)
	case float32:
		return uint64(val)
	case int:
		return uint64(val)
	case int64:
		return uint64(val)
	case json.Number:
		if parsed, err := val.Int64(); err == nil {
			return uint64(parsed)
		}
	case string:
		if parsed, err := strconv.ParseUint(val, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func stringFromAny(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case json.Number:
		return val.String()
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 64)
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint32, uint64:
		return fmt.Sprintf("%d", val)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

type staticEmbeddingProvider struct {
	defaultCfg EmbeddingConfig
}

func (s *staticEmbeddingProvider) GetEmbeddingConfig(_ context.Context, _ uint64) (*EmbeddingConfig, error) {
	return &s.defaultCfg, nil
}
