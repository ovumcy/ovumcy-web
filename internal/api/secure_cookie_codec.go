package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/security"
)

const (
	secureCookieVersion       = "v2"
	secureCookiePurposePrefix = "ovumcy.cookie."
)

var errInvalidSecureCookieValue = errors.New("invalid secure cookie value")

// secureCookieCodec frames AEAD-sealed cookie values. The cryptographic core
// (HKDF-SHA256 key derivation under the v2 cookie labels + AES-256-GCM) lives
// in internal/security; this codec owns only the cookie-specific framing:
// the purpose→AAD mapping ("ovumcy.cookie.<name>"), the "v2." version
// envelope, and base64url transport. Legacy "v1" values predate the AAD
// binding and are rejected on read by the envelope check.
type secureCookieCodec struct {
	aead *security.SealedCipher
}

func newSecureCookieCodec(secretKey []byte) (*secureCookieCodec, error) {
	if len(secretKey) == 0 {
		return nil, errors.New("secure cookie secret key is required")
	}

	aead, err := security.NewSecureCookieCipher(secretKey)
	if err != nil {
		return nil, fmt.Errorf("init secure cookie cipher: %w", err)
	}
	return &secureCookieCodec{aead: aead}, nil
}

// cookieCodec returns the handler's secureCookieCodec, building it from
// handler.secretKey on first use and caching both the codec and the error.
// Initialization is lazy rather than done in NewHandler because tests
// construct Handler as a bare literal; sync.Once keeps first use race-free
// under concurrent requests.
func (handler *Handler) cookieCodec() (*secureCookieCodec, error) {
	handler.cookieCodecOnce.Do(func() {
		handler.cookieCodecCached, handler.cookieCodecErr = newSecureCookieCodec(handler.secretKey)
	})
	return handler.cookieCodecCached, handler.cookieCodecErr
}

func (codec *secureCookieCodec) seal(purpose string, plaintext []byte) (string, error) {
	trimmedPurpose := strings.TrimSpace(purpose)
	if trimmedPurpose == "" {
		return "", errors.New("secure cookie purpose is required")
	}
	if codec == nil || codec.aead == nil {
		return "", errors.New("secure cookie codec is not initialized")
	}

	aad := []byte(secureCookiePurposePrefix + trimmedPurpose)
	payload, err := codec.aead.Seal(plaintext, aad)
	if err != nil {
		return "", fmt.Errorf("seal secure cookie payload: %w", err) // codecov:ignore -- defensive: AEAD Seal only fails on a crypto/rand error
	}

	return secureCookieVersion + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (codec *secureCookieCodec) open(purpose string, rawValue string) ([]byte, error) {
	trimmedPurpose := strings.TrimSpace(purpose)
	if trimmedPurpose == "" {
		return nil, errors.New("secure cookie purpose is required")
	}
	if codec == nil || codec.aead == nil {
		return nil, errors.New("secure cookie codec is not initialized")
	}

	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return nil, errInvalidSecureCookieValue
	}

	version, encodedPayload, found := strings.Cut(rawValue, ".")
	if !found || version != secureCookieVersion || strings.TrimSpace(encodedPayload) == "" {
		return nil, errInvalidSecureCookieValue
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, errInvalidSecureCookieValue
	}

	aad := []byte(secureCookiePurposePrefix + trimmedPurpose)
	plaintext, err := codec.aead.Open(payload, aad)
	if err != nil {
		return nil, errInvalidSecureCookieValue
	}
	return plaintext, nil
}
