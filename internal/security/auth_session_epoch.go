package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// authSessionEpochBytes is the entropy of one session epoch.
const authSessionEpochBytes = 32

// NewAuthSessionEpoch mints a session epoch: 32 random bytes, base64url without
// padding. The value is never empty on a nil error.
func NewAuthSessionEpoch() (string, error) {
	buffer := make([]byte, authSessionEpochBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// ValidAuthSessionEpoch reports whether value has the shape NewAuthSessionEpoch
// produces. A stored value that fails it is replaced, never trusted.
func ValidAuthSessionEpoch(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == authSessionEpochBytes
}

// AuthSessionEpochsEqual compares a claim's epoch with the server's in constant
// time. An empty operand on either side is a mismatch, never a pass: a check
// that a missing value can disable is not a check.
func AuthSessionEpochsEqual(claimed string, current string) bool {
	if claimed == "" || current == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(claimed), []byte(current)) == 1
}
