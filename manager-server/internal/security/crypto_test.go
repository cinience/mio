package security

import "testing"

func TestCryptoEncryptDecrypt(t *testing.T) {
	crypto, err := NewCrypto("test-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cipherText, err := crypto.Encrypt("password")
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	if cipherText == "password" {
		t.Fatalf("expected encrypted value")
	}
	plainText, err := crypto.Decrypt(cipherText)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if plainText != "password" {
		t.Fatalf("expected decrypted value, got %q", plainText)
	}
}

func TestMaskSecret(t *testing.T) {
	if MaskSecret("secret") != MaskedValue {
		t.Fatalf("expected masked value")
	}
}
