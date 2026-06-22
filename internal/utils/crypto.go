package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func GenerateID() string {
	b := make([]byte, 6)

	if _, err := rand.Read(b); err != nil {
		panic("failed to read random bytes for token ID: " + err.Error())
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

func GenerateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to read random bytes for token secret: " + err.Error())
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

// EncryptPayload encrypts a payload using the Token Secret
func EncryptPayload(secret string, plaintext []byte) ([]byte, error) {
	// 1 - Decode the base64 secret back into 32 bytes
	key, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid secret encoding: %w", err)
	}

	// 2 - Create the AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 3 - Generate a random nonce
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// 4 - Encrypt and append the nonce to the beginning of the ciphertext
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptPayload decrypts a payload using the Token Secret
func DecryptPayload(secret string, ciphertext []byte) ([]byte, error) {
	// 1 - Decode the base64 secret back into 32 bytes
	key, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid secret encoding: %w", err)
	}

	// 2 - Create the AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 3 - Ensure the ciphertext is long enough to contain the nonce
	nonceSize := aesgcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// 4 - Separate the nonce from the actual encrypted data
	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := aesgcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt (wrong secret or tampered data): %w", err)
	}

	return plaintext, nil
}
