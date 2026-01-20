package rag

import (
	"context"
	"os"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
)

func newEmbedding(ctx context.Context, cfg *Config) (embedding.Embedder, error) {
	conf := &openai.EmbeddingConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.EmbeddingModel,
	}
	dims := cfg.EmbeddingDims
	if dims <= 0 {
		dims = DefaultEmbeddingDims
	}
	conf.Dimensions = ptr(dims)
	if conf.APIKey == "" {
		conf.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if conf.BaseURL == "" {
		conf.BaseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if conf.Model == "" {
		conf.Model = "text-embedding-3-large"
	}
	return openai.NewEmbedder(ctx, conf)
}
