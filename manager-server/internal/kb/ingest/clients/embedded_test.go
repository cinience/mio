package clients

import (
	"context"
	"testing"
	"time"
)

func TestEmbeddedCrawlerParseRSS(t *testing.T) {
	crawler := NewEmbeddedCrawler(10 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tree, err := crawler.Parse(ctx, ParseOptions{
		Connector: "rss",
		Params:    map[string]any{"url": "https://sspai.com/feed"},
	})
	if err != nil {
		t.Fatalf("parse rss: %v", err)
	}
	if len(tree.Root) == 0 {
		t.Fatalf("expected root node")
	}
	total := 0
	for _, root := range tree.Root {
		total += len(root.Children)
	}
	if total == 0 {
		t.Fatalf("expected rss entries")
	}
}
