package mcp

import (
	"context"
	"testing"
	"time"
)

func TestMcpClientPoolCloseStopsSweepLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := NewMcpClientPool(ctx, 5*time.Millisecond)

	done := make(chan struct{})
	go func() {
		pool.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("pool.Close() did not return in time")
	}
}

func TestMcpClientPoolRemoveClosesSession(t *testing.T) {
	pool := NewMcpClientPool(context.Background(), time.Second)
	defer pool.Close()

	session := NewDeviceMCPSessionWithContext(context.Background(), "dev-1")
	pool.AddMcpClient("dev-1", session)

	pool.RemoveMcpClient("dev-1")
	if !session.closed {
		t.Fatalf("expected session to be closed after removal")
	}
	if client := pool.GetMcpClient("dev-1"); client != nil {
		t.Fatalf("expected session to be removed from pool")
	}
}
