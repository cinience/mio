package realtime

import (
	"testing"
	"time"
)

func TestVADSessionStatePrefixManagement(t *testing.T) {
	state := &vadSessionState{sampleRate: 16000}
	state.setPrefixPadding(100 * time.Millisecond)

	if state.prefixSamples != 1600 {
		t.Fatalf("expected prefix samples 1600, got %d", state.prefixSamples)
	}

	history := make([]float32, 1200)
	for i := range history {
		history[i] = float32(i)
	}
	state.pushHistory(history)
	if len(state.prefixBuffer) != 1200 {
		t.Fatalf("expected prefix buffer length 1200, got %d", len(state.prefixBuffer))
	}

	segment := []float32{10, 11, 12}
	combined := state.combineWithPrefix(segment)
	if len(combined) != len(history)+len(segment) {
		t.Fatalf("unexpected combined length: want %d, got %d", len(history)+len(segment), len(combined))
	}
	if combined[0] != history[0] || combined[len(history)-1] != history[len(history)-1] {
		t.Fatalf("historical samples not preserved in combined output")
	}
	if combined[len(history)] != segment[0] {
		t.Fatalf("segment not appended after prefix buffer")
	}

	wantPrefix := state.prefixSamples
	if wantPrefix > len(combined) {
		wantPrefix = len(combined)
	}
	if len(state.prefixBuffer) != wantPrefix {
		t.Fatalf("expected prefix buffer trimmed to %d, got %d", wantPrefix, len(state.prefixBuffer))
	}

	state.resetHistory()
	if len(state.prefixBuffer) != 0 {
		t.Fatalf("expected prefix buffer cleared, got %d", len(state.prefixBuffer))
	}
}

func TestVADSessionStateZeroPadding(t *testing.T) {
	state := &vadSessionState{sampleRate: 16000, prefixBuffer: []float32{1, 2, 3}}
	state.setPrefixPadding(0)
	if state.prefixSamples != 0 {
		t.Fatalf("expected prefixSamples 0, got %d", state.prefixSamples)
	}
	if len(state.prefixBuffer) != 0 {
		t.Fatalf("expected prefix buffer emptied, got %d", len(state.prefixBuffer))
	}
}
