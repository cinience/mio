package state

import (
	"testing"
)

func TestAsrAudioBufferConfigure(t *testing.T) {
	var buf AsrAudioBuffer
	buf.Configure(160, 4)

	if buf.capacity != 640 {
		t.Fatalf("unexpected capacity: got %d want %d", buf.capacity, 640)
	}
	if buf.frameSize != 160 {
		t.Fatalf("unexpected frameSize: got %d want %d", buf.frameSize, 160)
	}
	if buf.length != 0 || buf.start != 0 {
		t.Fatalf("buffer should be reset after configure")
	}
}

func TestAsrAudioBufferAddAndCopyLatest(t *testing.T) {
	var buf AsrAudioBuffer
	buf.Configure(4, 5) // capacity = 20 samples = 5 frames

	// helper to build deterministic frames
	frame := func(base float32) []float32 {
		return []float32{base, base + 0.1, base + 0.2, base + 0.3}
	}

	for i := 0; i < 5; i++ {
		buf.AddAsrAudioData(frame(float32(i)))
	}

	backing := make([]float32, 12)
	got := buf.CopyLatest(3, backing[:0])

	expected := []float32{
		2, 2.1, 2.2, 2.3,
		3, 3.1, 3.2, 3.3,
		4, 4.1, 4.2, 4.3,
	}

	if len(got) != len(expected) {
		t.Fatalf("unexpected copy length: got %d want %d", len(got), len(expected))
	}

	for i, v := range expected {
		if got[i] != v {
			t.Fatalf("unexpected value at %d: got %v want %v", i, got[i], v)
		}
	}

	// ensure scratch slice reused when capacity sufficient
	if &got[0] != &backing[0] {
		t.Fatalf("expected CopyLatest to reuse provided scratch buffer")
	}
}

func TestAsrAudioBufferOverflowDropsOldest(t *testing.T) {
	var buf AsrAudioBuffer
	buf.Configure(2, 3) // capacity 6 samples

	// add 4 frames, capacity holds 3 -> first frame dropped
	for i := 0; i < 4; i++ {
		buf.AddAsrAudioData([]float32{float32(i * 2), float32(i*2 + 1)})
	}

	got := buf.CopyLatest(3, nil)
	expected := []float32{2, 3, 4, 5, 6, 7}

	if len(got) != len(expected) {
		t.Fatalf("unexpected copy length: got %d want %d", len(got), len(expected))
	}
	for i, v := range expected {
		if got[i] != v {
			t.Fatalf("unexpected value at %d: got %v want %v", i, got[i], v)
		}
	}
}

func TestAsrAudioBufferDrainAndRemove(t *testing.T) {
	var buf AsrAudioBuffer
	buf.Configure(3, 4) // capacity 12 samples

	data := []float32{0, 1, 2, 3, 4, 5}
	buf.AddAsrAudioData(data)

	// remove first frame (3 samples)
	buf.RemoveAsrAudioData(1)

	copied := buf.Drain(make([]float32, 0, 9))
	expected := []float32{3, 4, 5}

	if len(copied) != len(expected) {
		t.Fatalf("unexpected drain length: got %d want %d", len(copied), len(expected))
	}
	for i, v := range expected {
		if copied[i] != v {
			t.Fatalf("unexpected value at %d: got %v want %v", i, copied[i], v)
		}
	}

	if buf.GetAsrDataSize() != 0 {
		t.Fatalf("buffer should be empty after drain, size=%d", buf.GetAsrDataSize())
	}
}
