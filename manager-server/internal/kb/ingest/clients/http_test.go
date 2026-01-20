package clients

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manager-server/internal/kb/ingest"
)

func TestHTTPCrawlerParse(t *testing.T) {
	tree := &ingest.ParseTree{
		Connector:   "url",
		GeneratedAt: time.Now(),
		Root: []*ingest.ParseNode{
			{
				ID:         "node-1",
				Title:      "Example",
				Type:       "document",
				URI:        "https://example.com",
				Selectable: true,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connectors/url/parse" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tree": tree,
		})
	}))
	defer server.Close()

	client := NewHTTPCrawler(server.URL,
		WithHTTPCrawlerTimeout(5*time.Second),
		WithHTTPCrawlerAPIKey("test-key"),
	)

	result, err := client.Parse(context.Background(), ParseOptions{Connector: "url"})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.Root) != 1 || result.Root[0].ID != "node-1" {
		t.Fatalf("unexpected parse tree: %+v", result)
	}
}

func TestHTTPCrawlerStageWithRawContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connectors/url/stage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":       "Doc",
			"originId":    "origin-1",
			"sourceUri":   "https://example.com/doc",
			"contentType": "text/markdown",
			"rawContent":  "# Hello",
			"metadata": map[string]any{
				"language": "en",
			},
		})
	}))
	defer server.Close()

	client := NewHTTPCrawler(server.URL)
	stage, err := client.Stage(context.Background(), StageOptions{
		Connector: "url",
		Item: ingest.ParseItem{
			NodeID: "node-1",
			URI:    "https://example.com/doc",
			Metadata: map[string]any{
				"title": "Fallback",
			},
		},
	})
	if err != nil {
		t.Fatalf("stage failed: %v", err)
	}
	defer stage.Reader.Close()

	body, err := io.ReadAll(stage.Reader)
	if err != nil {
		t.Fatalf("read staged content: %v", err)
	}

	if string(body) != "# Hello" {
		t.Fatalf("unexpected content: %s", body)
	}
	if stage.Title != "Doc" {
		t.Fatalf("unexpected title: %s", stage.Title)
	}
	if stage.OriginID != "origin-1" {
		t.Fatalf("unexpected origin id: %s", stage.OriginID)
	}
	if stage.SizeBytes != int64(len(body)) {
		t.Fatalf("unexpected size bytes: %d", stage.SizeBytes)
	}
}

func TestHTTPCrawlerStageWithDownloadURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/connectors/url/stage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":       "",
			"originId":    "",
			"sourceUri":   "https://example.com/doc",
			"contentType": "text/plain",
			"downloadUrl": "/downloads/doc-1",
		})
	})
	mux.HandleFunc("/downloads/doc-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer download-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("downloaded content"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewHTTPCrawler(server.URL,
		WithHTTPCrawlerAPIKey("download-key"),
	)

	stage, err := client.Stage(context.Background(), StageOptions{
		Connector: "url",
		Item: ingest.ParseItem{
			NodeID: "node-2",
			URI:    "https://example.com/doc",
		},
	})
	if err != nil {
		t.Fatalf("stage failed: %v", err)
	}
	defer stage.Reader.Close()

	body, err := io.ReadAll(stage.Reader)
	if err != nil {
		t.Fatalf("read download content: %v", err)
	}
	if string(body) != "downloaded content" {
		t.Fatalf("unexpected download content: %s", body)
	}
	if stage.Title == "" {
		t.Fatalf("expected title fallback to node id, got empty string")
	}
	if stage.SizeBytes != int64(len(body)) {
		t.Fatalf("unexpected size bytes: %d", stage.SizeBytes)
	}
}

func TestHTTPCrawlerStageMissingContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": "Doc",
		})
	}))
	defer server.Close()

	client := NewHTTPCrawler(server.URL)
	_, err := client.Stage(context.Background(), StageOptions{
		Connector: "url",
		Item: ingest.ParseItem{
			NodeID: "node-1",
			URI:    "https://example.com/doc",
		},
	})
	if !errors.Is(err, errCrawlerMissingContent) {
		t.Fatalf("expected missing content error, got %v", err)
	}
}
