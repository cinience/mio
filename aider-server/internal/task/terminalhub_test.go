package task

import "testing"

func TestTerminalHubLastActivityUpdates(t *testing.T) {
	t.Parallel()
	hub := NewTerminalHub(64)
	if !hub.LastActivity("task-1").IsZero() {
		t.Fatalf("expected zero last activity")
	}
	hub.Append("task-1", []byte("hi"))
	if hub.LastActivity("task-1").IsZero() {
		t.Fatalf("expected last activity to update")
	}
}

func TestTerminalHubSubscribeFromCursor(t *testing.T) {
	t.Parallel()
	hub := NewTerminalHub(8)
	hub.Append("task-1", []byte("abcd"))
	hub.Append("task-1", []byte("efgh"))
	_, backlog, unsubscribe := hub.Subscribe("task-1", 4)
	defer unsubscribe()
	if string(backlog) != "efgh" {
		t.Fatalf("expected backlog to be efgh, got %q", string(backlog))
	}
}

func TestTerminalHubCleanupClosesSubscribers(t *testing.T) {
	t.Parallel()
	hub := NewTerminalHub(64)
	ch, _, unsubscribe := hub.Subscribe("task-1", 0)
	defer unsubscribe()
	hub.Cleanup("task-1")
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected channel to be closed")
		}
	default:
		t.Fatalf("expected channel to be closed")
	}
}
