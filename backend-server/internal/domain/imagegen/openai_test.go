package imagegen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIProviderGenerate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}

		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		_ = r.ParseForm()

		response := map[string]any{
			"created": time.Now().Unix(),
			"data": []map[string]any{
				{"b64_json": "ZGF0YQ=="},
			},
			"usage": map[string]any{
				"total_tokens":  10,
				"input_tokens":  6,
				"output_tokens": 4,
				"input_tokens_details": map[string]any{
					"text_tokens":  5,
					"image_tokens": 1,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := GetProvider("openai", map[string]interface{}{
		"type":     "openai",
		"api_key":  "test-key",
		"base_url": server.URL + "/v1",
		"model":    "gpt-image-1",
	})
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}

	resp, err := provider.Generate(context.Background(), &GenerateRequest{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 image, got %d", len(resp.Data))
	}

	if resp.Data[0].B64JSON == "" {
		t.Fatalf("expected base64 payload")
	}

	if resp.Usage == nil || resp.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestOpenAIProviderPromptValidation(t *testing.T) {
	provider, err := GetProvider("openai", map[string]interface{}{
		"type":    "openai",
		"api_key": "test-key",
		"model":   "gpt-image-1",
	})
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}

	if _, err := provider.Generate(context.Background(), &GenerateRequest{Prompt: "  "}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}
