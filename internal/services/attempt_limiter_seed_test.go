package services

import (
	"strconv"
	"time"
)

// The helpers below let a test put a limiter, or a policy over it, into a known
// state, and read the count back. They live in a test file on purpose: the
// production budget has no way to read the count without reserving, and none to
// book a failure on its own, because a check followed by a separate booking is
// the window a concurrent burst passes through (see AttemptLimiter.Reserve).
// Code outside the tests cannot call them.

// TooManyRecentAny reports whether any key already carries `limit` attempts
// inside the window, without booking anything.
func (limiter *AttemptLimiter) TooManyRecentAny(keys []string, now time.Time, limit int, window time.Duration) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	return limiter.atLimitLocked(normalizeLimiterKeys(keys), now, limit, window)
}

// AddFailureAll books one attempt at `now` under every key, judged by budget,
// whatever the count already is.
func (limiter *AttemptLimiter) AddFailureAll(keys []string, now time.Time, budget AttemptBudget) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	limiter.bookLocked(normalizeLimiterKeys(keys), now, budget)
}

// TooManyRecent is the policy-level read of TooManyRecentAny.
func (policy *AuthAttemptPolicy) TooManyRecent(secretKey []byte, clientKey string, identity string, now time.Time) bool {
	return policy.limiter.TooManyRecentAny(policy.keys(secretKey, clientKey, identity), now, policy.attempts, policy.window)
}

// AddFailure is the policy-level seed of AddFailureAll.
func (policy *AuthAttemptPolicy) AddFailure(secretKey []byte, clientKey string, identity string, now time.Time) {
	policy.limiter.AddFailureAll(policy.keys(secretKey, clientKey, identity), now, policy.budget())
}

// CheckRateLimit reads the sign-in TOTP budget the way ReserveAttempt would
// judge it, without booking.
func (service *TOTPService) CheckRateLimit(secretKey []byte, clientKey string, userID uint, now time.Time) error {
	if service.attemptPolicy.TooManyRecent(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now) {
		return ErrTOTPRateLimited
	}
	return nil
}

// RecordFailure seeds one failed sign-in code under the budget's keys.
func (service *TOTPService) RecordFailure(secretKey []byte, clientKey string, userID uint, now time.Time) {
	service.attemptPolicy.AddFailure(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now)
}

// bookFailure seeds one failed re-auth under the budget's keys.
func (budget ReauthBudget) bookFailure(attempt ReauthAttempt) {
	clientKey, identity := budget.keys(attempt)
	budget.policy.AddFailure(budget.secretKey, clientKey, identity, attempt.at())
}
