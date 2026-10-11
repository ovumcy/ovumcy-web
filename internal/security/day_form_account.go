package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
)

// DayFormAccountKeyLabel is the HKDF info label of the key that binds a
// rendered day form to the account that rendered it. The session cookie is
// browser-wide and the CSRF token is not bound to an account, so a day form
// left open in one tab while a different account signs in from another would
// otherwise save its entry into that second account. The form carries a MAC of
// the rendering account's id, and the write refuses a form whose MAC names a
// different account.
//
// The value is opaque — an HMAC under a key derived from SECRET_KEY, never the
// raw id — and inert: it authorizes nothing, it only lets the server recognise
// that the form was not rendered for the account now signed in. Changing the
// label (or rotating SECRET_KEY) makes every day form already open answer as
// rendered for another account; reloading the page renders a fresh one.
const DayFormAccountKeyLabel = "ovumcy.day-form-account.v1" // #nosec G101 -- public HKDF info label, not a secret; the key material is SECRET_KEY.

var ErrDayFormAccountIDRequired = errors.New("day form account: account id required")

// DayFormAccountBinder mints and checks day-form account bindings under a key
// derived from SECRET_KEY once, at construction, rather than on every render
// and every write. The derived key is never written after construction, and
// every call builds its own HMAC from it, so one binder is safe to share
// across concurrent requests.
type DayFormAccountBinder struct {
	key    []byte
	keyErr error
}

// NewDayFormAccountBinder derives the binding key from secretKey. A secret that
// yields no key is kept as the binder's error: every Binding call then returns
// it and every Matches call refuses, exactly as the per-call derivation did.
func NewDayFormAccountBinder(secretKey []byte) *DayFormAccountBinder {
	key, err := DeriveTokenSigningKey(secretKey, DayFormAccountKeyLabel)
	return &DayFormAccountBinder{key: key, keyErr: err}
}

// Binding returns the opaque value a day form carries for userID. A zero id is
// refused rather than bound: no account has it, and a binding minted for it
// would be a value every unauthenticated render could share.
func (binder *DayFormAccountBinder) Binding(userID uint) (string, error) {
	if userID == 0 {
		return "", ErrDayFormAccountIDRequired
	}
	if binder.keyErr != nil {
		return "", binder.keyErr
	}
	mac := hmac.New(sha256.New, binder.key)
	_, _ = mac.Write([]byte(strconv.FormatUint(uint64(userID), 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Matches reports whether presented is the binding of userID, comparing in
// constant time. A zero id, a key that cannot be derived or an empty presented
// value never match: an operand that is missing is a mismatch, never a reason
// to skip the comparison.
func (binder *DayFormAccountBinder) Matches(userID uint, presented string) bool {
	expected, err := binder.Binding(userID)
	if err != nil || presented == "" {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(presented))
}

// DayFormAccountBinding is the one-shot form of DayFormAccountBinder.Binding: it
// derives the key for this call alone. A long-lived caller builds a binder once
// instead.
func DayFormAccountBinding(secretKey []byte, userID uint) (string, error) {
	return NewDayFormAccountBinder(secretKey).Binding(userID)
}

// DayFormAccountBindingMatches is the one-shot form of
// DayFormAccountBinder.Matches.
func DayFormAccountBindingMatches(secretKey []byte, userID uint, presented string) bool {
	return NewDayFormAccountBinder(secretKey).Matches(userID, presented)
}
