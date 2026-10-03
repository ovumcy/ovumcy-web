package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultLoginAttemptsLimit  = 8
	DefaultLoginAttemptsWindow = 15 * time.Minute
)

type AuthAttemptPolicy struct {
	scope    string
	limiter  *AttemptLimiter
	attempts int
	window   time.Duration
}

// attemptFigures is one budget's limit and window.
type attemptFigures struct {
	attempts int
	window   time.Duration
}

// scopeAttemptDefaults is each budget's own default, the figures a policy falls
// back to when the ones it was built with are below the floor. A scope absent
// from it takes the sign-in figures; a guard test reads every scope this
// package constructs a policy for from the source and refuses one missing here.
var scopeAttemptDefaults = map[string]attemptFigures{
	"login":           {DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow},
	"recovery":        {DefaultRecoveryAttemptsLimit, DefaultRecoveryAttemptsWindow},
	"logout":          {DefaultLogoutAttemptsLimit, DefaultLogoutAttemptsWindow},
	"totp":            {DefaultTOTPAttemptsLimit, DefaultTOTPAttemptsWindow},
	"totp.disable":    {DefaultTOTPDisableAttemptsLimit, DefaultTOTPDisableAttemptsWindow},
	"totp.enroll":     {DefaultTOTPEnrollAttemptsLimit, DefaultTOTPEnrollAttemptsWindow},
	"settings.reauth": {DefaultSettingsReauthAttemptsLimit, DefaultSettingsReauthAttemptsWindow},
}

func NewAuthAttemptPolicy(scope string, limiter *AttemptLimiter, attempts int, window time.Duration) *AuthAttemptPolicy {
	if limiter == nil {
		limiter = NewAttemptLimiter()
	}

	// The policy starts from the scope's own defaults and takes the caller's
	// figures only through Configure's floors: a limit below one would refuse
	// every attempt and a window below a second would never count one. A figure
	// below its floor leaves THIS budget's default, never another budget's.
	scope = strings.TrimSpace(scope)
	defaults, known := scopeAttemptDefaults[scope]
	if !known {
		defaults = attemptFigures{DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow}
	}
	policy := &AuthAttemptPolicy{
		scope:    scope,
		limiter:  limiter,
		attempts: defaults.attempts,
		window:   defaults.window,
	}
	policy.Configure(attempts, window)
	return policy
}

func (policy *AuthAttemptPolicy) Configure(attempts int, window time.Duration) {
	if attempts >= 1 {
		policy.attempts = attempts
	}
	if window >= time.Second {
		policy.window = window
	}
}

// Reserve is the policy's only way to draw an attempt. It refuses (ok false)
// when the client bucket or the identity bucket is spent, and otherwise books
// one provisional attempt under both in the same critical section (see
// AttemptLimiter.Reserve), so the compare the caller runs next is covered by the
// limit however many requests arrive at once. The caller leaves the reservation
// booked when the compare fails and calls Refund on the reservation when it
// succeeds or never ran. There is deliberately no separate check and no
// separate failure booking: a check that does not reserve is a window for every
// concurrent request to read the same count.
func (policy *AuthAttemptPolicy) Reserve(secretKey []byte, clientKey string, identity string, now time.Time) (*AttemptReservation, bool) {
	return policy.limiter.Reserve(policy.keys(secretKey, clientKey, identity), now, policy.budget())
}

// The policy has two success resets, and each caller names the one its flow
// needs; there is deliberately no plain Reset to fall back on.
//
// ResetClient is for flows reachable WITHOUT a session (password sign-in, the
// password-reset start, the sign-in TOTP step). It forgives the failures of
// the client that just succeeded — its own client bucket, and nothing else.
// The identity bucket is left to age out of its window: it pools the failures
// of EVERY client that tried this identity, so clearing it on one client's
// success would let the owner's own sign-in wipe the budget an attacker
// elsewhere had spent guessing at the same account.
func (policy *AuthAttemptPolicy) ResetClient(clientKey string) {
	policy.limiter.ResetAll(policy.keys(nil, clientKey, ""))
}

// ResetAll is for flows that already require a live session of the account
// being checked (the settings re-authentication password check, the password
// change, the TOTP disable confirmation). It clears the client bucket AND the
// identity bucket. Only the session holder reaches these flows, so the
// identity bucket holds that owner's own mistakes; keeping it after a correct
// answer would let ordinary typos accumulate until the owner is locked out of
// their own settings.
func (policy *AuthAttemptPolicy) ResetAll(secretKey []byte, clientKey string, identity string) {
	policy.limiter.ResetAll(policy.keys(secretKey, clientKey, identity))
}

func (policy *AuthAttemptPolicy) budget() AttemptBudget {
	return AttemptBudget{Scope: policy.scope, Limit: policy.attempts, Window: policy.window}
}

func (policy *AuthAttemptPolicy) keys(secretKey []byte, clientKey string, identity string) []string {
	keys := []string{fmt.Sprintf("%s:client:%s", policy.scope, NormalizeLimiterKey(clientKey))}
	normalizedIdentity := strings.TrimSpace(identity)
	if normalizedIdentity != "" {
		keys = append(keys, fmt.Sprintf("%s:identity:%s", policy.scope, hashAuthAttemptIdentity(secretKey, normalizedIdentity)))
	}
	return keys
}

func hashAuthAttemptIdentity(secretKey []byte, identity string) string {
	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte("ovumcy.auth-attempt.identity.v1:"))
	_, _ = mac.Write([]byte(identity))
	return hex.EncodeToString(mac.Sum(nil))
}
