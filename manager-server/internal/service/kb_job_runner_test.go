package service

import (
	"errors"
	"testing"
	"time"

	"manager-server/internal/kb/ingest"
	"manager-server/internal/models"
)

func TestCategorizeIngestionError(t *testing.T) {
	tests := []struct {
		errMsg   string
		expected string
	}{
		{"request timeout", "timeout"},
		{"Unauthorized access", "auth"},
		{"resource not found", "not_found"},
		{"network connection reset", "network"},
		{"unexpected failure", "internal"},
	}
	for _, tc := range tests {
		category := categorizeIngestionError(errors.New(tc.errMsg))
		if category != tc.expected {
			t.Fatalf("expected %s got %s for %q", tc.expected, category, tc.errMsg)
		}
	}
	if categorizeIngestionError(nil) != "unknown" {
		t.Fatalf("expected unknown for nil error")
	}
}

func TestKBJobRunnerShouldRetry(t *testing.T) {
	retryCfg := IngestionRetryConfig{MaxAttempts: 3, BackoffSeconds: []int{5, 10, 30}}
	runner := NewKBJobRunner(nil, nil, nil, nil, nil, ingest.Config{}, retryCfg)
	job := &models.KBJob{MaxRetries: 3}
	job.RetryCount = 1
	if !runner.shouldRetry(job) {
		t.Fatalf("expected retry allowed for attempt 1")
	}
	job.RetryCount = 3
	if runner.shouldRetry(job) {
		t.Fatalf("expected retry disallowed after reaching max attempts")
	}
	job.MaxRetries = 0
	job.RetryCount = 1
	if !runner.shouldRetry(job) {
		t.Fatalf("expected config-based retry allowance when job max retries unset")
	}
	runner = NewKBJobRunner(nil, nil, nil, nil, nil, ingest.Config{}, IngestionRetryConfig{})
	if runner.shouldRetry(job) {
		t.Fatalf("expected retry disabled when no limits configured")
	}
}

func TestKBJobRunnerRetryDelay(t *testing.T) {
	retryCfg := IngestionRetryConfig{MaxAttempts: 3, BackoffSeconds: []int{3, 6}}
	runner := NewKBJobRunner(nil, nil, nil, nil, nil, ingest.Config{}, retryCfg)
	if runner.retryDelay(1) != 3*time.Second {
		t.Fatalf("expected first retry to use configured backoff")
	}
	if runner.retryDelay(2) != 6*time.Second {
		t.Fatalf("expected second retry to use configured backoff")
	}
	if runner.retryDelay(5) != 6*time.Second {
		t.Fatalf("expected subsequent retries to reuse last backoff value")
	}
	runner = NewKBJobRunner(nil, nil, nil, nil, nil, ingest.Config{}, IngestionRetryConfig{})
	if runner.retryDelay(1) != time.Duration(defaultRetryBackoffSeconds)*time.Second {
		t.Fatalf("expected default backoff when config absent")
	}
}
