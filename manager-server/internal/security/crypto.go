package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const (
	encryptedPrefix = "enc:"
	MaskedValue     = "******"
)

// Crypto provides AES-GCM encryption helpers for sensitive fields.
type Crypto struct {
	gcm cipher.AEAD
}

func NewCrypto(secret string) (*Crypto, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	hash := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Crypto{gcm: gcm}, nil
}

func (c *Crypto) Encrypt(plain string) (string, error) {
	if c == nil || plain == "" {
		return plain, nil
	}
	if strings.HasPrefix(plain, encryptedPrefix) {
		return plain, nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	cipherText := c.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(cipherText), nil
}

func (c *Crypto) Decrypt(payload string) (string, error) {
	if c == nil || payload == "" {
		return payload, nil
	}
	if !strings.HasPrefix(payload, encryptedPrefix) {
		return payload, nil
	}
	encoded := strings.TrimPrefix(payload, encryptedPrefix)
	cipherText, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	nonceSize := c.gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return "", fmt.Errorf("invalid cipher payload")
	}
	nonce := cipherText[:nonceSize]
	data := cipherText[nonceSize:]
	plain, err := c.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func MaskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return MaskedValue
}
