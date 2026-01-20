package connectors

import (
	"context"

	"manager-server/internal/kb/ingest"
	"manager-server/internal/kb/ingest/clients"
)

type crawlerConnector struct {
	source string
	client clients.CrawlerClient
}

func newCrawlerConnector(source string, client clients.CrawlerClient) Connector {
	return &crawlerConnector{
		source: source,
		client: client,
	}
}

func (c *crawlerConnector) Source() string {
	return c.source
}

func (c *crawlerConnector) Parse(ctx context.Context, req ingest.ParseRequest) (*ingest.ParseTree, error) {
	return c.client.Parse(ctx, clients.ParseOptions{
		Connector:   c.source,
		Params:      req.Params,
		Credentials: req.Credentials,
		Metadata:    req.Metadata,
	})
}

func (c *crawlerConnector) Stage(ctx context.Context, item ingest.ParseItem) (*ingest.StageResult, error) {
	return c.client.Stage(ctx, clients.StageOptions{
		Connector: c.source,
		Item:      item,
	})
}
