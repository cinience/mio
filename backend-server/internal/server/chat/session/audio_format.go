package session

import (
	"fmt"
	"strings"

	"backend-server/internal/domain/audio"
)

func normalizeAudioFormat(format string) (string, error) {
	trimmed := strings.TrimSpace(format)
	if trimmed == "" {
		return audio.Format, nil
	}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case audio.Format, audio.FormatPCM16LE:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported audio format: %s", format)
	}
}

func normalizeOutputFormat(format string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(format)
	if trimmed == "" {
		return fallback, nil
	}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case audio.Format, audio.FormatPCM16LE, audio.FormatWAV:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported output format: %s", format)
	}
}
