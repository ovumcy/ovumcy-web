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

func (policy *AuthAttemptPolicy) Reset(secretKey []byte, clientKey string, identity string) {
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
