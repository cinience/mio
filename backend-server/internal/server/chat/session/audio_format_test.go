package session

import "testing"

func TestNormalizeAudioFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"default_empty", "", "opus", false},
		{"default_spaces", "  ", "opus", false},
		{"opus", "opus", "opus", false},
		{"opus_upper", "OPUS", "opus", false},
		{"pcm", "pcm_s16le", "pcm_s16le", false},
		{"pcm_mixed", "PCM_S16LE", "pcm_s16le", false},
		{"unsupported", "flac", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAudioFormat(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeAudioFormat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeOutputFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
		wantErr  bool
	}{
		{"fallback_opus", "", "opus", "opus", false},
		{"fallback_pcm", "", "pcm_s16le", "pcm_s16le", false},
		{"wav", "wav", "opus", "wav", false},
		{"wav_upper", "WAV", "opus", "wav", false},
		{"opus_explicit", "opus", "pcm_s16le", "opus", false},
		{"pcm_explicit", "pcm_s16le", "opus", "pcm_s16le", false},
		{"unsupported", "aac", "opus", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeOutputFormat(tt.input, tt.fallback)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeOutputFormat(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}
