package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	fieldCryptoSaltLabel = "ovumcy.field-crypto.salt.v1"
	fieldCryptoInfoLabel = "ovumcy.field-crypto.key.v1"
)

// EncryptField encrypts a plaintext string using AES-256-GCM with a key derived
// from secretKey via HKDF-SHA256. The returned value is base64url-encoded and
// suitable for persistent storage in the database.
func EncryptField(plaintext string, secretKey []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", errors.New("field crypto: secret key is required")
	}

	aead, err := newFieldCryptoAEAD(secretKey)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("field crypto: generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := make([]byte, 0, len(nonce)+len(ciphertext))
	payload = append(payload, nonce...)
	payload = append(payload, ciphertext...)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecryptField decrypts a value produced by EncryptField.
func DecryptField(encoded string, secretKey []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", errors.New("field crypto: secret key is required")
	}

	aead, err := newFieldCryptoAEAD(secretKey)
	if err != nil {
		return "", err
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("field crypto: decode: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(payload) < nonceSize+1 {
		return "", errors.New("field crypto: ciphertext too short")
	}

	nonce, ct := payload[:nonceSize], payload[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("field crypto: decrypt: %w", err)
	}

	return string(plaintext), nil
}

func newFieldCryptoAEAD(secretKey []byte) (cipher.AEAD, error) {
	reader := hkdf.New(sha256.New, secretKey, []byte(fieldCryptoSaltLabel), []byte(fieldCryptoInfoLabel))
	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(reader, derivedKey); err != nil {
		return nil, fmt.Errorf("field crypto: derive key: %w", err)
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("field crypto: create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("field crypto: create aead: %w", err)
	}

	return aead, nil
}
