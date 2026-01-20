package functions

import (
	"net/url"
	"testing"
)

func TestParseSubsonicURLPreservesExtraParams(t *testing.T) {
	raw := "https://example.com/xiaozhi/audio/subsonic/rest?username=agent&password=secret&projectId=abc&token=xyz"

	cfg, err := parseSubsonicURL(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Username != "agent" || cfg.Password != "secret" {
		t.Fatalf("unexpected credentials: %+v", cfg)
	}

	if cfg.ExtraParams.Get("projectId") != "abc" {
		t.Fatalf("projectId not preserved: %+v", cfg.ExtraParams)
	}

	if cfg.ExtraParams.Get("token") != "xyz" {
		t.Fatalf("token not preserved: %+v", cfg.ExtraParams)
	}

	streamURL := buildStreamURL(cfg, "song123")
	parsed, err := url.Parse(streamURL)
	if err != nil {
		t.Fatalf("failed to parse stream url: %v", err)
	}

	query := parsed.Query()
	if query.Get("projectId") != "abc" || query.Get("token") != "xyz" {
		t.Fatalf("extra params missing from stream url: %s", parsed.RawQuery)
	}

	values := url.Values{}
	cfg.applyExtraParams(values)
	if values.Get("projectId") != "abc" {
		t.Fatalf("applyExtraParams should preserve projectId")
	}
}
