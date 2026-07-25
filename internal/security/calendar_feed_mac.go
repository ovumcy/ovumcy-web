package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

// HKDF label pair for the calendar-feed verifier MAC. It is its own purpose, so
// the derived key is unrelated to the sealed-cookie and field-encryption keys:
// a leak or rotation of one never affects another.
//
// Bumping the version suffix invalidates every previously stored MAC, and the
// verify path treats a mismatch as a hard refusal rather than healing the row
// from its bcrypt hash — so a label bump, exactly like a SECRET_KEY rotation,
// disarms every armed calendar feed and each owner re-generates the subscribe URL
// from settings. Ship a label bump as a deliberate, release-noted change, not a
// silent cleanup.
const (
	calendarFeedMACSaltLabel = "ovumcy.calendar-feed-token.salt.v1"
	calendarFeedMACInfoLabel = "ovumcy.calendar-feed-token.key.v1"
)

// ErrCalendarFeedMACKeyMissing is returned when no secret key is available to
// derive the MAC key from. Callers must treat it as a hard failure rather than
// falling back to an unkeyed digest.
var ErrCalendarFeedMACKeyMissing = errors.New("calendar feed mac: secret key missing")

// CalendarFeedVerifierMAC derives the keyed authenticator stored in place of the
// verifier's bcrypt hash.
//
// Why a keyed hash rather than bcrypt: bcrypt's work factor exists to slow
// offline guessing of LOW-ENTROPY human secrets. The feed verifier is 32
// characters over a 32-symbol alphabet — 160 bits from crypto/rand — so brute
// force is already infeasible with or without a work factor. What the work
// factor did buy was a per-request CPU cost on an unauthenticated endpoint,
// which is a liability rather than a defence.
//
// The MAC is keyed (not a bare digest) so that a database leak alone does not
// let an attacker verify guessed verifiers offline: without SECRET_KEY the
// stored value is unusable. The selector is bound into the input, so a MAC
// lifted from one row cannot authenticate a token presented against another.
func CalendarFeedVerifierMAC(secretKey []byte, selector string, verifier string) (string, error) {
	if len(secretKey) == 0 {
		return "", ErrCalendarFeedMACKeyMissing
	}

	macKey := make([]byte, sha256.Size)
	reader := hkdf.New(sha256.New, secretKey, []byte(calendarFeedMACSaltLabel), []byte(calendarFeedMACInfoLabel))
	if _, err := io.ReadFull(reader, macKey); err != nil {
		return "", err // codecov:ignore -- hkdf over sha256 cannot short-read this size
	}

	mac := hmac.New(sha256.New, macKey)
	// A length-prefixed separator keeps the two halves unambiguous: without it a
	// (selector, verifier) pair could be re-split at a different offset and
	// produce the same input bytes.
	_, _ = mac.Write([]byte(selector))
	_, _ = mac.Write([]byte{0x00})
	_, _ = mac.Write([]byte(verifier))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyCalendarFeedVerifierMAC recomputes the MAC and compares it in constant
// time. A missing key, an empty stored value, or any mismatch all report false
// identically, so the caller gains no signal about which condition failed.
func VerifyCalendarFeedVerifierMAC(secretKey []byte, selector string, verifier string, storedMAC string) bool {
	if storedMAC == "" {
		return false
	}
	computed, err := CalendarFeedVerifierMAC(secretKey, selector, verifier)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedMAC)) == 1
}
