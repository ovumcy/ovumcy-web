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

func NewAuthAttemptPolicy(scope string, limiter *AttemptLimiter, attempts int, window time.Duration) *AuthAttemptPolicy {
	if limiter == nil {
		limiter = NewAttemptLimiter()
	}

	policy := &AuthAttemptPolicy{
		scope:    strings.TrimSpace(scope),
		limiter:  limiter,
		attempts: attempts,
		window:   window,
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

func (policy *AuthAttemptPolicy) TooManyRecent(secretKey []byte, clientKey string, identity string, now time.Time) bool {
	return policy.limiter.TooManyRecentAny(policy.keys(secretKey, clientKey, identity), now, policy.attempts, policy.window)
}

func (policy *AuthAttemptPolicy) AddFailure(secretKey []byte, clientKey string, identity string, now time.Time) {
	policy.limiter.AddFailureAll(policy.keys(secretKey, clientKey, identity), now, policy.budget())
}

// The policy has two success resets, and each caller names the one its flow
// needs; there is deliberately no plain Reset to fall back on.
//
// ResetClient is for flows reachable WITHOUT a session (password sign-in,
// recovery-code sign-in, the password-reset start, the sign-in TOTP step, the
// pre-session OIDC link confirmation). It forgives the failures of the client
// that just succeeded — its own client bucket, and nothing else. The identity
// bucket is left to age out of its window: it pools the failures of EVERY
// client that tried this identity, so clearing it on one client's success
// would let the owner's own sign-in wipe the budget an attacker elsewhere had
// spent guessing at the same account.
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
