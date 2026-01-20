package task

import (
	"testing"
	"time"
)

func TestWaitAndSendEnterSends(t *testing.T) {
	t.Parallel()
	var sent [][]byte
	err := WaitAndSendEnter(func() time.Time { return time.Time{} }, func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, 200*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(sent))
	}
	if string(sent[0]) != "\r" || string(sent[1]) != "\n" {
		t.Fatalf("unexpected payloads: %q %q", string(sent[0]), string(sent[1]))
	}
}

func TestWaitAndSendEnterWaitsForQuiet(t *testing.T) {
	t.Parallel()
	var calls int
	var sent [][]byte
	err := WaitAndSendEnter(func() time.Time {
		calls++
		if calls < 3 {
			return time.Now()
		}
		return time.Now().Add(-200 * time.Millisecond)
	}, func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, 500*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(sent))
	}
	if string(sent[0]) != "\r" || string(sent[1]) != "\n" {
		t.Fatalf("unexpected payloads: %q %q", string(sent[0]), string(sent[1]))
	}
}

func TestWaitAndSendEnterTimeout(t *testing.T) {
	t.Parallel()
	var sent [][]byte
	err := WaitAndSendEnter(func() time.Time { return time.Now() }, func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(sent))
	}
}
