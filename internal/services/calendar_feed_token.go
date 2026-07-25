package services

import (
	"crypto/subtle"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/crypto/bcrypt"
)

// Calendar (.ics) feed subscription token (slice 1: token generation +
// verification only). The feed URL path carries a bearer capability token so a
// calendar client can poll a read-only .ics of the owner's own cycle events.
//
// A bearer token in a URL path is a deliberate, owner-approved carve-out to the
// "no secret in transport" invariant for the feed surface, compensated by
// hashing-at-rest (the verifier is never stored in plaintext — only a keyed MAC
// of it, plus a bcrypt hash for rollback), one-click revoke, 404-no-oracle,
// per-IP rate-limit, and log-redaction. Only the hash-at-rest + no-oracle
// verification live in this slice; the transport compensations land in later
// slices.
//
// The token is SELECTOR + VERIFIER, concatenated with no separator into a
// fixed-width string:
//
//   - selector (calendarFeedSelectorLength chars) is the NON-secret lookup id.
//     It only NAMES the row: a feed request resolves the single matching user
//     with one indexed `WHERE calendar_feed_selector = ?` lookup, avoiding an
//     O(N) bcrypt scan over every user (the feed carries no email to look up
//     by). It is stored in plaintext and UNIQUE-indexed.
//   - verifier (calendarFeedVerifierLength chars) is the SECRET. Only a keyed MAC
//     of it (plus a bcrypt hash, kept for rollback) is stored; the verifier
//     plaintext is never persisted. The full token is shown to the owner exactly
//     once at generation, like a recovery code.
//
// Both halves draw from calendarFeedAlphabet: 32 unambiguous URL/path-safe
// characters (the recovery-code alphabet — no I/O/0/1). With a 32-char alphabet
// each character carries 5 bits, so the 16-char selector is ~80 bits (ample for
// a non-secret, collision-resistant lookup id) and the 32-char verifier is ~160
// bits, comfortably above the ≥128-bit secret floor.
const (
	// calendarFeedAlphabet is the 32-char unambiguous alphabet shared by the
	// selector and verifier (identical to the recovery-code alphabet). Every
	// character is URL/path-safe, so the concatenated token drops straight into a
	// feed URL path with no escaping.
	calendarFeedAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
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
// never retrievable afterward) alongside the storables: the plaintext selector
// (the non-secret lookup id), a keyed MAC of the verifier, and a bcrypt hash of
// the verifier. The verifier plaintext is deliberately NOT returned separately —
// it exists only inside fullToken — so a caller cannot accidentally persist it.
// Callers store the whole triple via the repository; on rotation a new call
// replaces all of it, invalidating the previous token because the old verifier
// matches neither the new MAC nor the new hash.
//
// Both verifier columns are minted every time, on purpose:
//
//   - The MAC is what verification compares — a microsecond constant-time compare
//     instead of ~265 ms of bcrypt on an unauthenticated endpoint.
//   - The bcrypt hash stays current so rolling back to a binary that predates
//     migration 032 keeps verifying tokens minted by this one.
//
// A missing secretKey is a hard failure, never a token with an empty MAC: an
// empty MAC means "minted before migration 032, verify via bcrypt", so minting
// one would silently pin a fresh subscription to the slow path forever.
func GenerateCalendarFeedToken(secretKey []byte) (fullToken string, columns models.CalendarFeedTokenColumns, err error) {
	selector, err := security.RandomString(calendarFeedSelectorLength, calendarFeedAlphabet)
	if err != nil {
		return "", models.CalendarFeedTokenColumns{}, err // codecov:ignore -- crypto/rand failure, not reachable in tests
	}
	verifier, err := security.RandomString(calendarFeedVerifierLength, calendarFeedAlphabet)
	if err != nil {
		return "", models.CalendarFeedTokenColumns{}, err // codecov:ignore -- crypto/rand failure, not reachable in tests
	}
	verifierMAC, err := security.CalendarFeedVerifierMAC(secretKey, selector, verifier)
	if err != nil {
		return "", models.CalendarFeedTokenColumns{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(verifier), passwordHashCost)
	if err != nil {
		return "", models.CalendarFeedTokenColumns{}, err // codecov:ignore -- bcrypt only errors on an out-of-range cost
	}
	return selector + verifier, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: string(hash),
		VerifierMAC:  verifierMAC,
	}, nil
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
// request matches the stored token columns for a row. It splits the token,
// constant-time-compares the presented selector against the stored one (defense
// in depth — the selector is non-secret, but a byte-wise compare avoids even an
// incidental timing signal), then verifies the verifier half.
//
// Which verifier column decides is NOT a fallback chain, it is a strict
// precedence:
//
//   - A stored MAC is authoritative. It is recomputed over the PRESENTED
//     (selector, verifier) and compared in constant time. A mismatch is a hard
//     refusal — bcrypt is deliberately NOT consulted afterward. That matters most
//     when SECRET_KEY was rotated: every stored MAC then mismatches, and healing
//     the row from bcrypt would keep a bearer capability alive across a key
//     rotation while also making every wrong verifier pay bcrypt again, which is
//     exactly the CPU cost this design removes. A rotation disarms armed feeds
//     instead, and the owner re-generates the subscribe URL from settings.
//   - An empty MAC means the row was minted before migration 032, when no MAC
//     existed. Only then does bcrypt decide. The MAC cannot be backfilled from
//     the hash, so the caller writes it in after a successful verification here
//     and the row leaves the slow path for good.
//   - Neither column set means the feed is off: refuse.
//
// It is written to give a caller no oracle: a malformed token, a selector
// mismatch, and a wrong verifier all return false the same way. The intended
// call site first looks the row up by selector — a missing selector yields no row
// and the same "not found" outcome — so selector existence, selector
// correctness, and verifier correctness are indistinguishable to a caller of the
// feed endpoint.
//
// Both comparisons are evaluated unconditionally (the results are combined with a
// bitwise AND, not a short-circuiting &&) so the verifier check is always paid
// once a stored verifier column is present. This keeps the run time independent
// of whether the selector matched, leaving no timing signal that separates a
// selector mismatch from a selector-match-plus-verifier-mismatch even when this
// primitive is called directly (not via the by-selector lookup).
func VerifyCalendarFeedToken(secretKey []byte, fullToken string, stored models.CalendarFeedTokenColumns) bool {
	presentedSelector, presentedVerifier, ok := SplitCalendarFeedToken(fullToken)
	if !ok {
		return false
	}
	if stored.Selector == "" {
		return false
	}
	selectorMatch := subtle.ConstantTimeCompare([]byte(presentedSelector), []byte(stored.Selector))
	verifierMatch := 0
	switch {
	case stored.VerifierMAC != "":
		// The MAC binds the selector into its input, so a MAC lifted from another
		// row cannot authenticate a token presented against this one — the
		// constant-time selector compare above is belt and braces.
		if security.VerifyCalendarFeedVerifierMAC(secretKey, presentedSelector, presentedVerifier, stored.VerifierMAC) {
			verifierMatch = 1
		}
	case stored.VerifierHash != "":
		if bcrypt.CompareHashAndPassword([]byte(stored.VerifierHash), []byte(presentedVerifier)) == nil {
			verifierMatch = 1
		}
	default:
		return false
	}
	return selectorMatch&verifierMatch == 1
}
