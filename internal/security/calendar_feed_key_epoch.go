package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

// HKDF label pair for the calendar-feed key epoch. Its own purpose, like every
// other derived domain: the epoch value is unrelated to the sealed-cookie,
// field-encryption, and feed-MAC keys, so storing it can never help an attacker
// against any of them.
const (
	calendarFeedKeyEpochSaltLabel = "ovumcy.calendar-feed-key-epoch.salt.v1"
	calendarFeedKeyEpochInfoLabel = "ovumcy.calendar-feed-key-epoch.key.v1"
)

// ErrCalendarFeedKeyEpochKeyMissing is returned when no secret key is available
// to derive the epoch from. Callers must treat it as a hard failure, never as
// "no epoch yet".
var ErrCalendarFeedKeyEpochKeyMissing = errors.New("calendar feed key epoch: secret key missing")

// CalendarFeedKeyEpoch derives the opaque identifier of the current
// calendar-feed verification regime. It changes exactly when previously stored
// feed-verifier MACs stop verifying:
//
//   - when SECRET_KEY is rotated (the HKDF input changes), and
//   - when the feed verifier-MAC label pair is version-bumped (the labels are
//     bound into the value below).
//
// The boot-time rotation sentinel persists this value in app_state and compares
// it on every start. A mismatch means "every armed feed minted before migration
// 032 would still verify through its key-independent bcrypt hash even though
// the operator rotated the key (or a label bump shipped)" — the one gap in the
// hard-refusal rule that MAC verification enforces on its own — so those rows
// are disarmed instead of surviving the rotation.
//
// The value is an HMAC-SHA256 output under a key derived from SECRET_KEY:
// non-invertible, and useless to an attacker who reads it from the database
// (deriving or checking it requires SECRET_KEY itself).
func CalendarFeedKeyEpoch(secretKey []byte) (string, error) {
	if len(secretKey) == 0 {
		return "", ErrCalendarFeedKeyEpochKeyMissing
	}

	epochKey := make([]byte, sha256.Size)
	reader := hkdf.New(sha256.New, secretKey, []byte(calendarFeedKeyEpochSaltLabel), []byte(calendarFeedKeyEpochInfoLabel))
	if _, err := io.ReadFull(reader, epochKey); err != nil {
		return "", err // codecov:ignore -- hkdf over sha256 cannot short-read this size
	}

	mac := hmac.New(sha256.New, epochKey)
	// Binding the verifier-MAC label pair (with the same length-prefix-free 0x00
	// separator convention as the MAC itself) makes a label version bump change
	// the epoch, so the sentinel disarms legacy bcrypt-only rows on a bump too —
	// the documented "label bump behaves like a rotation" rule would otherwise
	// silently exempt exactly those rows.
	_, _ = mac.Write([]byte(calendarFeedMACSaltLabel))
	_, _ = mac.Write([]byte{0x00})
	_, _ = mac.Write([]byte(calendarFeedMACInfoLabel))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
