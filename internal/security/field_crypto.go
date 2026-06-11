package security

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// EncryptField encrypts a plaintext string with AES-256-GCM using a key
// derived from secretKey via HKDF-SHA256 (the field-crypto label pair in
// sealed_cipher.go). The ciphertext is bound to `aad` through the AEAD
// authentication tag, so an attacker with database write access cannot swap
// the ciphertext from one row or column into another under the same key
// without invalidating the tag — DecryptField with a different aad will fail
// to open.
//
// `aad` MUST be non-empty. Callers should use a stable, context-specific
// identifier such as "ovumcy.field.<purpose>:<row id>" (for example
// "ovumcy.field.totp_secret:42"). Empty aad would re-introduce the
// cross-row swap vector that this function exists to close.
//
// The returned value is base64url-encoded and suitable for persistent
// storage in the database.
func EncryptField(plaintext string, secretKey []byte, aad []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", errors.New("field crypto: secret key is required")
	}
	if len(aad) == 0 {
		return "", errors.New("field crypto: aad is required")
	}
	return sealFieldCiphertext(plaintext, secretKey, aad)
}

// DecryptField opens a ciphertext produced by EncryptField. It returns the
// plaintext together with an `isLegacy` flag.
//
// The function first tries to open the payload with the supplied aad (the
// normal path for ciphertexts written by this revision of EncryptField).
// On failure it falls back to opening without aad, which succeeds only for
// LEGACY ciphertexts written by earlier Ovumcy versions that called
// EncryptField with nil aad. When the legacy fallback succeeds `isLegacy`
// is true, signaling to the caller that the persisted ciphertext is in the
// deprecated unbound format and should be re-encrypted with aad on the next
// safe write opportunity.
//
// If neither path opens, the original aad-bound error is returned so the
// failure mode mirrors a tampered ciphertext rather than a misconfigured
// aad.
func DecryptField(encoded string, secretKey []byte, aad []byte) (string, bool, error) {
	if len(secretKey) == 0 {
		return "", false, errors.New("field crypto: secret key is required")
	}

	plaintext, err := openFieldCiphertext(encoded, secretKey, aad)
	if err == nil {
		return plaintext, false, nil
	}
	if len(aad) == 0 {
		return "", false, err
	}
	if legacy, legacyErr := openFieldCiphertext(encoded, secretKey, nil); legacyErr == nil {
		return legacy, true, nil
	}
	return "", false, err
}

func sealFieldCiphertext(plaintext string, secretKey []byte, aad []byte) (string, error) {
	fieldCipher, err := newFieldCipher(secretKey)
	if err != nil {
		return "", err
	}

	payload, err := fieldCipher.Seal([]byte(plaintext), aad)
	if err != nil {
		return "", fmt.Errorf("field crypto: seal: %w", err) // codecov:ignore -- defensive: AEAD Seal only fails on a crypto/rand error
	}

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func openFieldCiphertext(encoded string, secretKey []byte, aad []byte) (string, error) {
	fieldCipher, err := newFieldCipher(secretKey)
	if err != nil {
		return "", err
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("field crypto: decode: %w", err)
	}

	plaintext, err := fieldCipher.Open(payload, aad)
	if err != nil {
		return "", fmt.Errorf("field crypto: decrypt: %w", err)
	}

	return string(plaintext), nil
}

// EncryptFieldNoAADForTest produces a ciphertext using the legacy no-aad
// format that earlier Ovumcy versions wrote to disk. It exists ONLY so
// regression tests in other packages can construct legacy fixtures without
// re-implementing the AEAD machinery (the HKDF labels are unexported).
// Production code MUST NOT call this function — use EncryptField instead.
func EncryptFieldNoAADForTest(plaintext string, secretKey []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", errors.New("field crypto: secret key is required")
	}
	return sealFieldCiphertext(plaintext, secretKey, nil)
}
