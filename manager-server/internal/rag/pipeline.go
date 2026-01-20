package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino-ext/components/indexer/es8"
	esretriever "github.com/cloudwego/eino-ext/components/retriever/es8"
	"github.com/cloudwego/eino-ext/components/retriever/es8/search_mode"
	"github.com/cloudwego/eino/components/indexer"
	er "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

// Pipeline provides indexing, retrieval, and deletion helpers backed by Elasticsearch.
type Pipeline struct {
	indexer   indexer.Indexer
	retriever er.Retriever
	client    *elasticsearch.Client
	cfg       *Config
}

func New(ctx context.Context, cfg *Config) (*Pipeline, error) {
	if cfg == nil {
		return nil, errors.New("rag config required")
	}
	if cfg.Client == nil {
		return nil, errors.New("elasticsearch client required")
	}
	if cfg.IndexName == "" {
		return nil, errors.New("index name required")
	}

	dims := cfg.EmbeddingDims
	if dims <= 0 {
		dims = DefaultEmbeddingDims
	}
	cfg.EmbeddingDims = dims

	if err := ensureIndex(ctx, cfg.Client, cfg.IndexName, dims); err != nil {
		return nil, err
	}

	embedder, err := newEmbedding(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("init embedding: %w", err)
	}

	idxCfg := &es8.IndexerConfig{
		Client:    cfg.Client,
		Index:     cfg.IndexName,
		BatchSize: 16,
		DocumentToFields: func(ctx context.Context, doc *schema.Document) (map[string]es8.FieldValue, error) {
			if doc == nil {
				return nil, errors.New("document must not be nil")
			}
			if doc.MetaData == nil {
				return nil, errors.New("document metadata required")
			}
			rawKnowledge, ok := doc.MetaData[FieldKnowledgeName]
			if !ok {
				return nil, errors.New("missing knowledge name")
			}
			knowledgeName, _ := rawKnowledge.(string)
			if knowledgeName == "" {
				return nil, errors.New("knowledge name required")
			}

			ext := extractExt(doc.MetaData)
			extBytes, _ := json.Marshal(ext)

			return map[string]es8.FieldValue{
				FieldContent: {
					Value:    doc.Content,
					EmbedKey: FieldContentVector,
				},
				FieldExtra: {
					Value: string(extBytes),
				},
				FieldKnowledgeName: {
					Value: knowledgeName,
				},
			}, nil
		},
	}
	idxCfg.Embedding = embedder
	idx, err := es8.NewIndexer(ctx, idxCfg)
	if err != nil {
		return nil, fmt.Errorf("init indexer: %w", err)
	}

	rCfg := &esretriever.RetrieverConfig{
		Client: cfg.Client,
		Index:  cfg.IndexName,
		SearchMode: search_mode.SearchModeDenseVectorSimilarity(
			search_mode.DenseVectorSimilarityTypeCosineSimilarity,
			FieldContentVector,
		),
		ResultParser: parseEsHit,
	}
	rCfg.Embedding = embedder
	rtr, err := esretriever.NewRetriever(ctx, rCfg)
	if err != nil {
		return nil, fmt.Errorf("init retriever: %w", err)
	}

	return &Pipeline{
		indexer:   idx,
		retriever: rtr,
		client:    cfg.Client,
		cfg:       cfg.clone(),
	}, nil
}

func (p *Pipeline) IndexDocuments(ctx context.Context, knowledgeName string, docs []*schema.Document) ([]string, error) {
	if knowledgeName == "" {
		return nil, errors.New("knowledge name required")
	}
	if len(docs) == 0 {
		return nil, nil
	}
	for _, doc := range docs {
		if doc == nil {
			return nil, errors.New("nil document provided")
		}
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}
		doc.MetaData[FieldKnowledgeName] = knowledgeName
	}
	return p.indexer.Store(ctx, docs)
}

func (p *Pipeline) Retrieve(ctx context.Context, knowledgeName, query string, topK int) ([]*schema.Document, error) {
	if knowledgeName == "" {
		return nil, errors.New("knowledge name required")
	}
	if topK <= 0 {
		topK = 5
	}
	filters := []types.Query{
		{
			Bool: &types.BoolQuery{
				Must: []types.Query{
					{Match: map[string]types.MatchQuery{
						FieldKnowledgeName: {Query: knowledgeName},
					}},
				},
			},
		},
	}
	docs, err := p.retriever.Retrieve(
		ctx,
		query,
		er.WithTopK(topK),
		esretriever.WithFilters(filters),
	)
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Score() > docs[j].Score()
	})
	if len(docs) > topK {
		docs = docs[:topK]
	}
	return docs, nil
}

func (p *Pipeline) DeleteDocument(ctx context.Context, docID string) error {
	return deleteDocument(ctx, p.client, p.cfg.IndexName, docID)
}

func extractExt(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	ext := make(map[string]any, len(meta))
	for key, value := range meta {
		if key == FieldKnowledgeName || key == FieldExtra {
			continue
		}
		ext[key] = value
	}
	return ext
}

func parseEsHit(_ context.Context, hit types.Hit) (*schema.Document, error) {
	doc := &schema.Document{
		MetaData: map[string]any{},
	}
	if hit.Id_ != nil {
		doc.ID = *hit.Id_
	}

	var src map[string]any
	if err := sonic.Unmarshal(hit.Source_, &src); err != nil {
		return nil, err
	}

	if v, ok := src[FieldContent]; ok {
		if content, ok := v.(string); ok {
			doc.Content = content
		}
	}
	if v, ok := src[FieldExtra]; ok {
		switch vv := v.(type) {
		case string:
			doc.MetaData[FieldExtra] = vv
		case []byte:
			doc.MetaData[FieldExtra] = string(vv)
		default:
			if data, err := json.Marshal(vv); err == nil {
				doc.MetaData[FieldExtra] = string(data)
			}
		}
	}
	if v, ok := src[FieldKnowledgeName]; ok {
		doc.MetaData[FieldKnowledgeName] = v
	}
	if hit.Score_ != nil {
		doc.WithScore(float64(*hit.Score_))
	}
	return doc, nil
}
