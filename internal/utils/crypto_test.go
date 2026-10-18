package utils

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestGenerateIDEntropy(t *testing.T) {
	id := GenerateID()
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		t.Fatalf("failed to decode generated ID: %v", err)
	}
	if len(decoded) != 16 {
		t.Errorf("expected 16 bytes of entropy (128 bits), got %d bytes", len(decoded))
	}
}

func TestEncryptDecryptPayload(t *testing.T) {
	secret := GenerateSecret()
	plaintext := []byte("secret mesh payload data")

	ciphertext, err := EncryptPayload(secret, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	decrypted, err := DecryptPayload(secret, ciphertext)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("expected %s, got %s", plaintext, decrypted)
	}
}

func TestDecryptTamperedPayload(t *testing.T) {
	secret := GenerateSecret()
	plaintext := []byte("secret payload")

	ciphertext, err := EncryptPayload(secret, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Tamper with ciphertext
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err = DecryptPayload(secret, ciphertext)
	if err == nil {
		t.Fatal("expected decryption of tampered ciphertext to fail, but it succeeded")
	}
}
