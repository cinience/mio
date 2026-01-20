package tts

import (
	"strings"

	gocn2an "github.com/godeps/go-cn2an"

	"speech-server/internal/logger"
)

// TtsNormalizer converts Arabic numerals within text to their Chinese representations.
type TtsNormalizer struct {
	transform *gocn2an.Transform
}

func NewTTSNormalizer() *TtsNormalizer {
	return &TtsNormalizer{
		transform: gocn2an.NewTransform(),
	}
}

func (n *TtsNormalizer) Normalize(text string) string {
	if n == nil {
		return text
	}

	clean := strings.TrimSpace(text)
	if clean == "" {
		return text
	}

	normalized, err := n.transform.Transform(clean, "an2cn")
	if err != nil {
		logger.Warnf("tts normalize failed: %v", err)
		return text
	}

	if normalized = strings.TrimSpace(normalized); normalized == "" {
		return text
	}

	return normalized
}
