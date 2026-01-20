package api

import "testing"

func TestSanitizeTerminalInputPreservesArrowKeys(t *testing.T) {
	t.Parallel()
	input := []byte("\x1b[A")
	got := sanitizeTerminalInput(input)
	if string(got) != string(input) {
		t.Fatalf("expected arrow key sequence preserved, got %q", string(got))
	}
}

func TestSanitizeTerminalInputStripsOSC(t *testing.T) {
	t.Parallel()
	input := []byte("\x1b]52;foo\x07")
	got := sanitizeTerminalInput(input)
	if len(got) != 0 {
		t.Fatalf("expected osc sequence stripped, got %q", string(got))
	}
}

func TestSanitizeTerminalInputStripsCSIQuery(t *testing.T) {
	t.Parallel()
	input := []byte("\x1b[6n")
	got := sanitizeTerminalInput(input)
	if len(got) != 0 {
		t.Fatalf("expected csi query stripped, got %q", string(got))
	}
}
