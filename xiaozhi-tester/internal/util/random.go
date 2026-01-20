package util

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// RandomMAC generates a locally administered unicast MAC-like string.
func RandomMAC() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand mac: %w", err)
	}
	buf[0] = (buf[0] | 0x02) & 0xFE
	parts := make([]string, len(buf))
	for i, b := range buf {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":"), nil
}

// RandomUUID returns a new UUID string.
func RandomUUID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("rand uuid: %w", err)
	}
	return id.String(), nil
}
