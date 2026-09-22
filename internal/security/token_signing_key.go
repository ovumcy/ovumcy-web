package security

import (
	"crypto/sha256"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// The signed tokens the application mints — the auth session carried inside
// `ovumcy_auth` and the password-reset grant carried inside
// `ovumcy_reset_password` — are HMAC-signed JWTs. Each purpose signs under its
// own key, derived from SECRET_KEY through HKDF with a purpose-specific info
// label, so a token minted for one purpose never verifies under the other's key
// whatever its claims say. Signing both directly with SECRET_KEY made the two
// domains separable only by claim shape.
//
// Changing any label below is a deliberate, release-noted event: every token
// already minted under the old label stops verifying, which signs every owner
// out and voids every outstanding reset grant.
const (
	tokenSigningKeySaltLabel = "ovumcy.token-signing.salt.v1" // #nosec G101 -- public HKDF salt label, not a secret; the key material is SECRET_KEY.

	// AuthSessionTokenKeyLabel is the HKDF info label of the auth-session JWT key.
	AuthSessionTokenKeyLabel = "ovumcy.token-signing.auth-session.v1" // #nosec G101 -- public HKDF info label, not a secret; the key material is SECRET_KEY.
	// PasswordResetTokenKeyLabel is the HKDF info label of the password-reset JWT key.
	PasswordResetTokenKeyLabel = "ovumcy.token-signing.password-reset.v1" // #nosec G101 -- public HKDF info label, not a secret; the key material is SECRET_KEY.
)

var (
	ErrTokenSigningKeyMissing      = errors.New("token signing: secret key missing")
	ErrTokenSigningPurposeRequired = errors.New("token signing: purpose label required")
)

// DeriveTokenSigningKey returns the 32-byte HMAC key for one token purpose.
// An empty secret or an empty purpose label is refused rather than derived: an
// empty label would collapse every purpose onto one key, which is the property
// this function exists to prevent.
func DeriveTokenSigningKey(secretKey []byte, purposeLabel string) ([]byte, error) {
	if len(secretKey) == 0 {
		return nil, ErrTokenSigningKeyMissing
	}
	if strings.TrimSpace(purposeLabel) == "" {
		return nil, ErrTokenSigningPurposeRequired
	}
	key := make([]byte, sha256.Size)
	reader := hkdf.New(sha256.New, secretKey, []byte(tokenSigningKeySaltLabel), []byte(purposeLabel))
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err // codecov:ignore -- hkdf over sha256 cannot short-read this size
	}
	return key, nil
}
