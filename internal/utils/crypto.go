package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
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

func CreateFullTokenString() string {
	id := GenerateID()
	secret := GenerateSecret()
	return fmt.Sprintf("atom_%s_%s", id, secret)
}
