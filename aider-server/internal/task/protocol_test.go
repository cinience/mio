package task

import (
	"testing"

	"aider-server/internal/taskmodel"
)

func TestResolveProtocolNonCodexRejectsSelection(t *testing.T) {
	_, err := ResolveProtocol("claude", taskmodel.ProtocolAppServer, "pty")
	if err == nil {
		t.Fatalf("expected error for non-codex protocol")
	}
}

func TestResolveProtocolCodexDefaultApplied(t *testing.T) {
	got, err := ResolveProtocol("codex", "", "app-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != taskmodel.ProtocolAppServer {
		t.Fatalf("expected app-server, got %q", got)
	}
}

func TestResolveProtocolCodexInvalid(t *testing.T) {
	_, err := ResolveProtocol("codex", taskmodel.Protocol("invalid"), "pty")
	if err == nil {
		t.Fatalf("expected error for invalid protocol")
	}
}
