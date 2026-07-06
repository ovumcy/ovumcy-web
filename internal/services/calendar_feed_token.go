package services

import (
	"crypto/subtle"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/crypto/bcrypt"
)

// Calendar (.ics) feed subscription token (slice 1: token generation +
// verification only). The feed URL path carries a bearer capability token so a
// calendar client can poll a read-only .ics of the owner's own cycle events.
//
// A bearer token in a URL path is a deliberate, owner-approved carve-out to the
// "no secret in transport" invariant for the feed surface, compensated by
// hashing-at-rest (the verifier is bcrypt-hashed here, never stored plaintext),
// one-click revoke, 404-no-oracle, per-IP rate-limit, and log-redaction. Only
// the hash-at-rest + no-oracle verification live in this slice; the transport
// compensations land in later slices.
//
// The token is SELECTOR + VERIFIER, concatenated with no separator into a
// fixed-width string:
//
//   - selector (calendarFeedSelectorLength chars) is the NON-secret lookup id.
//     It only NAMES the row: a feed request resolves the single matching user
//     with one indexed `WHERE calendar_feed_selector = ?` lookup, avoiding an
//     O(N) bcrypt scan over every user (the feed carries no email to look up
//     by). It is stored in plaintext and UNIQUE-indexed.
//   - verifier (calendarFeedVerifierLength chars) is the SECRET. Only its bcrypt
//     hash is stored; the verifier plaintext is never persisted. The full token
//     is shown to the owner exactly once at generation, like a recovery code.
//
// Both halves draw from calendarFeedTokenAlphabet: 32 unambiguous URL/path-safe
// characters (the recovery-code alphabet — no I/O/0/1). With a 32-char alphabet
// each character carries 5 bits, so the 16-char selector is ~80 bits (ample for
// a non-secret, collision-resistant lookup id) and the 32-char verifier is ~160
// bits, comfortably above the ≥128-bit secret floor.
const (
	// calendarFeedTokenAlphabet is the 32-char unambiguous alphabet shared by the
	// selector and verifier (identical to the recovery-code alphabet). Every
	// character is URL/path-safe, so the concatenated token drops straight into a
	// feed URL path with no escaping.
	calendarFeedTokenAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// calendarFeedSelectorLength is the selector length in characters (~80 bits
	// over the 32-char alphabet). Non-secret; sized for collision resistance as a
	// lookup id, not for secrecy.
	calendarFeedSelectorLength = 16
	// calendarFeedVerifierLength is the verifier length in characters (~160 bits
	// over the 32-char alphabet), well above the 128-bit secret floor.
	calendarFeedVerifierLength = 32
	// calendarFeedTokenLength is the total length of a well-formed full token.
	calendarFeedTokenLength = calendarFeedSelectorLength + calendarFeedVerifierLength
)

// GenerateCalendarFeedToken mints a fresh calendar-feed token. It returns the
// full shown-once token (selector+verifier, handed to the owner exactly once and
// never retrievable afterward) alongside the two storables: the plaintext
// selector (the non-secret lookup id) and the bcrypt hash of the verifier. The
// verifier plaintext is deliberately NOT returned separately — it exists only
// inside fullToken — so a caller cannot accidentally persist it. Callers store
// selector + verifierHash via the repository; on rotation a new call replaces
// both, invalidating the previous token because the old verifier no longer
// matches the new hash.
func GenerateCalendarFeedToken() (fullToken string, selector string, verifierHash string, err error) {
	selector, err = security.RandomString(calendarFeedSelectorLength, calendarFeedTokenAlphabet)
	if err != nil {
		return "", "", "", err
	}
	verifier, err := security.RandomString(calendarFeedVerifierLength, calendarFeedTokenAlphabet)
	if err != nil {
		return "", "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(verifier), passwordHashCost)
	if err != nil {
		return "", "", "", err
	}
	return selector + verifier, selector, string(hash), nil
}

// SplitCalendarFeedToken splits a full feed token into its selector and verifier
// halves. It returns ok=false for any token that is not exactly
// calendarFeedTokenLength characters, so a malformed feed URL is rejected before
// any DB lookup. The selector half is safe to use as a lookup key; the verifier
// half is a secret and must only be fed to a constant-time compare.
func SplitCalendarFeedToken(fullToken string) (selector string, verifier string, ok bool) {
	if len(fullToken) != calendarFeedTokenLength {
		return "", "", false
	}
	return fullToken[:calendarFeedSelectorLength], fullToken[calendarFeedSelectorLength:], true
}

// VerifyCalendarFeedToken reports whether a full feed token presented by a
// request matches the stored selector + verifier hash for a row. It splits the
// token, constant-time-compares the presented selector against the stored one
// (defense in depth — the selector is non-secret, but a byte-wise compare avoids
// even an incidental timing signal), then verifies the verifier half against the
// stored bcrypt hash with bcrypt.CompareHashAndPassword (inherently
// constant-time).
//
// It is written to give a caller no oracle: a malformed token, a selector
// mismatch, and a wrong verifier all return false the same way. The intended
// call site (a later slice) first looks the row up by selector — a missing
// selector yields no row and the same "not found" outcome — so selector
// existence, selector correctness, and verifier correctness are indistinguishable
// to a caller of the feed endpoint.
//
// Both comparisons are evaluated unconditionally (the results are combined with a
// bitwise AND, not a short-circuiting &&) so the bcrypt cost is always paid once a
// stored hash is present. This keeps the run time independent of whether the
// selector matched, leaving no timing signal that separates a selector mismatch
// from a selector-match-plus-verifier-mismatch even when this primitive is called
// directly (not via the by-selector lookup).
func VerifyCalendarFeedToken(fullToken string, storedSelector string, storedVerifierHash string) bool {
	presentedSelector, presentedVerifier, ok := SplitCalendarFeedToken(fullToken)
	if !ok {
		return false
	}
	if storedSelector == "" || storedVerifierHash == "" {
		return false
	}
	selectorMatch := subtle.ConstantTimeCompare([]byte(presentedSelector), []byte(storedSelector))
	verifierMatch := 0
	if bcrypt.CompareHashAndPassword([]byte(storedVerifierHash), []byte(presentedVerifier)) == nil {
		verifierMatch = 1
	}
	return selectorMatch&verifierMatch == 1
}
