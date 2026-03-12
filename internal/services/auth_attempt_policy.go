package services

import (
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

func (policy *AuthAttemptPolicy) TooManyRecent(clientKey string, identity string, now time.Time) bool {
	return policy.limiter.TooManyRecentAny(policy.keys(clientKey, identity), now, policy.attempts, policy.window)
}

func (policy *AuthAttemptPolicy) AddFailure(clientKey string, identity string, now time.Time) {
	policy.limiter.AddFailureAll(policy.keys(clientKey, identity), now, policy.window)
}

func (policy *AuthAttemptPolicy) Reset(clientKey string, identity string) {
	policy.limiter.ResetAll(policy.keys(clientKey, identity))
}

func (policy *AuthAttemptPolicy) keys(clientKey string, identity string) []string {
	keys := []string{fmt.Sprintf("%s:client:%s", policy.scope, NormalizeLimiterKey(clientKey))}
	normalizedIdentity := strings.TrimSpace(identity)
	if normalizedIdentity != "" {
		keys = append(keys, fmt.Sprintf("%s:identity:%s", policy.scope, hashAuthAttemptIdentity(normalizedIdentity)))
	}
	return keys
}

func hashAuthAttemptIdentity(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}
