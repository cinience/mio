package task

import (
	"sync"
	"testing"
	"time"
)

type fakeTerminalHub struct {
	mu   sync.Mutex
	last time.Time
}

func (f *fakeTerminalHub) Append() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = time.Now()
	return f.last
}

func (f *fakeTerminalHub) LastActivity() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func TestWaitAndSendEnterWaitsForTerminalQuiet(t *testing.T) {
	t.Parallel()
	hub := &fakeTerminalHub{}
	quiet := 200 * time.Millisecond
	timeout := 2 * time.Second

	sendAt := make(chan time.Time, 2)
	errCh := make(chan error, 1)
	hub.Append()
	go func() {
		errCh <- WaitAndSendEnter(hub.LastActivity, func([]byte) error {
			sendAt <- time.Now()
			return nil
		}, timeout, quiet)
	}()

	time.Sleep(50 * time.Millisecond)
	last := hub.Append()

	select {
	case sent := <-sendAt:
		if sent.Sub(last) < quiet {
			t.Fatalf("enter sent too early: %v < %v", sent.Sub(last), quiet)
		}
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for enter send")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for enter result")
	}
}
