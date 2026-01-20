package rag

import (
	"github.com/elastic/go-elasticsearch/v8"
)

// Config contains the runtime settings required for the RAG pipeline.
type Config struct {
	Client         *elasticsearch.Client
	IndexName      string
	APIKey         string
	BaseURL        string
	EmbeddingModel string
	EmbeddingDims  int
}

func (c *Config) clone() *Config {
	if c == nil {
		return nil
	}
	cc := *c
	return &cc
}
