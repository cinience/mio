package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manager-server/internal/kb/ingest"
)

func TestYuqueConnectorParseAndStage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/demo/docs":
			if r.Header.Get(yuqueAuthHeader) != "token" {
				t.Fatalf("expected yuque token header")
			}
			payload := map[string]any{
				"data": []map[string]any{
					{
						"title":  "Sample",
						"slug":   "sample",
						"status": 1,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		case "/repos/demo/docs/sample":
			payload := map[string]any{
				"data": map[string]any{
					"title":  "Sample",
					"body":   "# Yuque",
					"format": "markdown",
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	conn := newYuqueConnector().(*yuqueConnector)
	conn.apiBase = ts.URL

	parsed, err := conn.Parse(context.Background(), ingest.ParseRequest{
		Params: map[string]any{
			"namespace": "demo",
		},
		Credentials: map[string]any{
			"token": "token",
		},
	})
	if err != nil {
		t.Fatalf("yuque parse error: %v", err)
	}
	if len(parsed.Root) != 1 || len(parsed.Root[0].Children) != 1 {
		t.Fatalf("unexpected parse tree")
	}

	node := parsed.Root[0].Children[0]
	stage, err := conn.Stage(context.Background(), ingest.ParseItem{
		NodeID: node.ID,
		Metadata: map[string]any{
			"namespace": "demo",
			"slug":      "sample",
		},
		Credentials: map[string]any{
			"token": "token",
		},
	})
	if err != nil {
		t.Fatalf("yuque stage error: %v", err)
	}
	if stage.RawContent != "# Yuque" {
		t.Fatalf("unexpected raw content: %s", stage.RawContent)
	}
}
