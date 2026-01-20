package clients

import (
	"context"

	"manager-server/internal/kb/ingest"
)

// ParseOptions defines parameters for crawler parse operations.
type ParseOptions struct {
	Connector   string
	Params      map[string]any
	Credentials map[string]any
	Metadata    map[string]any
}

// StageOptions defines parameters for staging/export operations.
type StageOptions struct {
	Connector string
	Item      ingest.ParseItem
}

// CrawlerClient describes the behaviour required to parse and stage remote sources.
type CrawlerClient interface {
	Parse(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error)
	Stage(ctx context.Context, opts StageOptions) (*ingest.StageResult, error)
}
