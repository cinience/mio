package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manager-server/internal/kb/ingest"
)

func TestGitHubConnectorParseAndStage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/git/trees/main":
			if r.Header.Get("Authorization") != "Bearer token123" {
				t.Fatalf("expected authorization header")
			}
			payload := map[string]any{
				"tree": []map[string]any{
					{
						"path": "docs",
						"type": "tree",
					},
					{
						"path": "docs/readme.md",
						"type": "blob",
						"size": 12,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		case r.Method == http.MethodGet && r.URL.Path == "/example/repo/main/docs/readme.md":
			_, _ = w.Write([]byte("# Readme"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	conn := newGitHubConnector().(*gitHubConnector)
	conn.apiBase = ts.URL
	conn.rawBase = ts.URL

	req := ingest.ParseRequest{
		Params: map[string]any{
			"owner":  "example",
			"repo":   "repo",
			"branch": "main",
		},
		Credentials: map[string]any{
			"token": "token123",
		},
	}
	parsed, err := conn.Parse(context.Background(), req)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(parsed.Root) != 1 {
		t.Fatalf("expected single root")
	}
	if len(parsed.Root[0].Children) == 0 {
		t.Fatalf("expected child nodes")
	}

	var fileNode *ingest.ParseNode
	for _, child := range parsed.Root[0].Children {
		if child.Selectable {
			fileNode = child
			break
		}
		for _, grand := range child.Children {
			if grand.Selectable {
				fileNode = grand
				break
			}
		}
		if fileNode != nil {
			break
		}
	}
	if fileNode == nil {
		t.Fatalf("file node not found")
	}

	stage, err := conn.Stage(context.Background(), ingest.ParseItem{
		NodeID: fileNode.ID,
		Metadata: map[string]any{
			"path":   "docs/readme.md",
			"owner":  "example",
			"repo":   "repo",
			"branch": "main",
		},
		Credentials: map[string]any{"token": "token123"},
	})
	if err != nil {
		t.Fatalf("stage error: %v", err)
	}
	if stage.RawContent != "# Readme" {
		t.Fatalf("unexpected content: %s", stage.RawContent)
	}
}
