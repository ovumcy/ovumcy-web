package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// HKDF label pairs, one per sealing purpose. The labels are inputs to key
// derivation: changing any of these strings derives a different AEAD key and
// silently invalidates every value previously sealed under that purpose
// (cookies stop opening, stored TOTP ciphertexts stop decrypting). They are
// version-suffixed so a deliberate rotation introduces a new pair instead of
// editing an existing one. Backward compatibility is pinned by
// field_crypto_golden_test.go and internal/api/secure_cookie_codec_golden_test.go.
const (
	fieldCryptoSaltLabel = "ovumcy.field-crypto.salt.v1"
	fieldCryptoInfoLabel = "ovumcy.field-crypto.key.v1"

	secureCookieSaltLabel = "ovumcy.secure-cookie.salt.v2"
	secureCookieInfoLabel = "ovumcy.secure-cookie.key.v2"
)

// SealedCipher is the single AEAD primitive behind Ovumcy's sealed values:
// AES-256-GCM under a 32-byte key derived from the application SECRET_KEY via
// HKDF-SHA256 with a purpose-specific salt/info label pair. Sealed payloads
// are nonce||ciphertext with a fresh random nonce per Seal call.
//
// The cipher deliberately owns nothing beyond key derivation and the AEAD
// operation. AAD policy (what to bind, whether empty AAD is allowed) and
// transport framing (base64, version envelopes) belong to the consumers:
// field encryption in this package and the sealed-cookie codec in
// internal/api.
type SealedCipher struct {
	aead cipher.AEAD
}

// NewSecureCookieCipher derives the AEAD used by the sealed-cookie codec in
// internal/api. The v2 label pair matches the codec's "v2." version envelope;
// rotating one without the other would mislabel the stored format.
func NewSecureCookieCipher(secretKey []byte) (*SealedCipher, error) {
	return newSealedCipher(secretKey, secureCookieSaltLabel, secureCookieInfoLabel)
}

// newFieldCipher derives the AEAD used for field-level encryption of
// database columns (currently users.totp_secret via EncryptField).
func newFieldCipher(secretKey []byte) (*SealedCipher, error) {
	return newSealedCipher(secretKey, fieldCryptoSaltLabel, fieldCryptoInfoLabel)
}

func newSealedCipher(secretKey []byte, saltLabel, infoLabel string) (*SealedCipher, error) {
	if len(secretKey) == 0 {
		return nil, errors.New("sealed cipher: secret key is required")
	}

	reader := hkdf.New(sha256.New, secretKey, []byte(saltLabel), []byte(infoLabel))
	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(reader, derivedKey); err != nil {
		return nil, fmt.Errorf("sealed cipher: derive key: %w", err) // codecov:ignore -- defensive: HKDF read cannot fail for a 32-byte sha256 stream
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("sealed cipher: create cipher: %w", err) // codecov:ignore -- defensive: aes.NewCipher never fails for a 32-byte key
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("sealed cipher: create aead: %w", err) // codecov:ignore -- defensive: cipher.NewGCM never fails for AES
	}

	return &SealedCipher{aead: aead}, nil
}

// NonceSize returns the AEAD nonce length in bytes — the length of the nonce
// prefix in payloads produced by Seal.
func (c *SealedCipher) NonceSize() int {
	return c.aead.NonceSize()
}

// Seal encrypts plaintext bound to aad and returns nonce||ciphertext with a
// fresh random nonce. The aad is passed through to the AEAD as-is; callers
// enforce their own non-empty-AAD policies.
func (c *SealedCipher) Seal(plaintext, aad []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("sealed cipher: not initialized")
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("sealed cipher: generate nonce: %w", err) // codecov:ignore -- defensive: crypto/rand read cannot fail in practice
	}

	ciphertext := c.aead.Seal(nil, nonce, plaintext, aad)
	payload := make([]byte, 0, len(nonce)+len(ciphertext))
	payload = append(payload, nonce...)
	payload = append(payload, ciphertext...)
	return payload, nil
}

// Open decrypts a nonce||ciphertext payload produced by Seal under the same
// purpose labels and aad. Payloads that cannot carry a nonce plus at least
// one ciphertext byte are rejected before the AEAD runs.
func (c *SealedCipher) Open(payload, aad []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("sealed cipher: not initialized")
	}

	nonceSize := c.aead.NonceSize()
	if len(payload) <= nonceSize {
		return nil, errors.New("sealed cipher: payload too short")
	}

	nonce, ciphertext := payload[:nonceSize], payload[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("sealed cipher: open: %w", err)
	}
	return plaintext, nil
}
