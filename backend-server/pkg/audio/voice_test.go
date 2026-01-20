package audio

import "testing"

func TestBuildWavHeader(t *testing.T) {
	header := BuildWavHeader(16000, 1, 320)
	if len(header) != 44 {
		t.Fatalf("expected header len 44, got %d", len(header))
	}
	if string(header[0:4]) != "RIFF" {
		t.Fatalf("expected RIFF header, got %q", string(header[0:4]))
	}
	if string(header[8:12]) != "WAVE" {
		t.Fatalf("expected WAVE header, got %q", string(header[8:12]))
	}
	if string(header[36:40]) != "data" {
		t.Fatalf("expected data chunk, got %q", string(header[36:40]))
	}
}
