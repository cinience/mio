package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return nil
	}

	err := Do(context.Background(), DefaultConfig(), fn)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestDo_RetryAndSuccess(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}

	config := DefaultConfig()
	config.InitialDelay = 10 * time.Millisecond

	err := Do(context.Background(), config, fn)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_MaxAttemptsReached(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("timeout")
	}

	config := DefaultConfig()
	config.MaxAttempts = 3
	config.InitialDelay = 10 * time.Millisecond

	err := Do(context.Background(), config, fn)
	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_NonRetryableError(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("400 Bad Request")
	}

	err := Do(context.Background(), DefaultConfig(), fn)
	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d", attempts)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	config := DefaultConfig()
	config.InitialDelay = 100 * time.Millisecond

	err := Do(ctx, config, fn)
	if err == nil {
		t.Error("expected context cancelled error")
	}
	// 应该只尝试一次，因为 context 已取消
	if attempts > 2 {
		t.Errorf("expected at most 2 attempts with cancelled context, got %d", attempts)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"timeout", errors.New("timeout exceeded"), true},
		{"503 error", errors.New("503 Service Unavailable"), true},
		{"400 error", errors.New("400 Bad Request"), false},
		{"401 error", errors.New("401 Unauthorized"), false},
		{"invalid argument", errors.New("invalid argument"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryable(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	config := DefaultConfig()
	config.InitialDelay = 1 * time.Second
	config.Multiplier = 2.0
	config.MaxDelay = 10 * time.Second

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},  // 1 * 2^0
		{2, 2 * time.Second},  // 1 * 2^1
		{3, 4 * time.Second},  // 1 * 2^2
		{4, 8 * time.Second},  // 1 * 2^3
		{5, 10 * time.Second}, // 限制到 MaxDelay
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := ExponentialBackoff(tt.attempt, config)
			if delay != tt.expected {
				t.Errorf("ExponentialBackoff(%d) = %v, expected %v", tt.attempt, delay, tt.expected)
			}
		})
	}
}
