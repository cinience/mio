package service

import (
	"testing"
	"time"

	"manager-server/internal/kb/ingest"
)

func TestMergeMetadata(t *testing.T) {
	base := map[string]any{
		"a": 1,
		"b": "keep",
	}
	override := map[string]any{
		"b": "override",
		"c": true,
	}
	result := mergeMetadata(base, override)
	if result["a"] != 1 {
		t.Fatalf("expected base value preserved")
	}
	if result["b"] != "override" {
		t.Fatalf("expected override to replace base")
	}
	if result["c"] != true {
		t.Fatalf("expected override new value")
	}
}

func TestDeriveStageFileName(t *testing.T) {
	name := deriveStageFileName(&ingest.StageResult{
		Title: "Example.md",
	})
	if name != "Example.md" {
		t.Fatalf("expected title to be used, got %s", name)
	}

	name = deriveStageFileName(&ingest.StageResult{
		Title:    "",
		OriginID: "origin-id",
	})
	if name != "origin-id" {
		t.Fatalf("expected origin id fallback, got %s", name)
	}

	name = deriveStageFileName(nil)
	if name == "" {
		t.Fatalf("expected generated name when result nil")
	}
}

func TestStringsTrim(t *testing.T) {
	if stringsTrim("  value ") != "value" {
		t.Fatalf("expected trimming whitespace")
	}
	if stringsTrim("") != "" {
		t.Fatalf("expected empty string unchanged")
	}
}

func TestKBIngestionServiceVerifyWebhookSecret(t *testing.T) {
	svc := NewKBIngestionService(nil, nil, nil, nil, time.Minute, nil, IngestionQuotaConfig{}, IngestionRetryConfig{}, true, "token-123", nil)
	if !svc.VerifyWebhookSecret("token-123") {
		t.Fatalf("expected matching secret to pass")
	}
	if svc.VerifyWebhookSecret("mismatch") {
		t.Fatalf("expected mismatched secret to fail")
	}
}

func TestKBIngestionServiceVerifyWebhookSecretDisabled(t *testing.T) {
	svc := NewKBIngestionService(nil, nil, nil, nil, time.Minute, nil, IngestionQuotaConfig{}, IngestionRetryConfig{}, false, "", nil)
	if !svc.VerifyWebhookSecret("") {
		t.Fatalf("expected webhook disabled to skip validation")
	}
	if !svc.VerifyWebhookSecret("any-value") {
		t.Fatalf("expected webhook disabled to accept any secret")
	}
}
