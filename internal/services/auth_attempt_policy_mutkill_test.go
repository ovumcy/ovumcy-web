package services

import (
	"testing"
	"time"
)

// TestDefaultLoginAttemptsWindowBounds pins the DefaultLoginAttemptsWindow
// constant (auth_attempt_policy.go) as consumed by a default-configured login
// attempt policy — the same wiring NewLoginService uses.
//
// The default login policy must count failures recorded *within* the window: a
// full DefaultLoginAttemptsLimit of failures logged one minute ago still falls
// inside the ~15-minute window, so the next attempt is rate-limited; the same
// failures logged sixteen minutes ago have aged out and must not trip it.
//
// This kills the ARITHMETIC_BASE mutant `15 * time.Minute` -> `15 / time.Minute`,
// which collapses the window to 0. A zero window prunes every prior failure
// (pruneLocked keeps only entries strictly After(now-window), and now-0 == now),
// silently DISABLING login brute-force rate limiting for any policy that keeps
// the default window. The existing login rate-limit regression
// (TestLoginServiceAuthenticateRateLimitsByIdentityAcrossIPs) cannot catch this
// because it overrides the window via ConfigureAttemptLimits(2, time.Hour).
func TestDefaultLoginAttemptsWindowBounds(t *testing.T) {
	secretKey := []byte("mutkill-login-window-secret")
	clientKey := "203.0.113.7"
	identity := "owner@example.com"
	now := time.Date(2026, time.March, 2, 13, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		failureAge  time.Duration
		wantLimited bool
	}{
		// Inside the 15-minute window: recording exactly the limit one minute ago
		// must trip the limiter. Kills the zeroed-window mutant.
		{name: "one_minute_ago_counts", failureAge: time.Minute, wantLimited: true},
		// Outside the window: the same failures sixteen minutes ago must expire.
		// Pins the window's upper bound (~15m), not merely "> 0".
		{name: "sixteen_minutes_ago_expires", failureAge: 16 * time.Minute, wantLimited: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limiter := NewAttemptLimiter()
			policy := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)

			failedAt := now.Add(-tc.failureAge)
			for range DefaultLoginAttemptsLimit {
				policy.AddFailure(secretKey, clientKey, identity, failedAt)
			}

			if got := policy.TooManyRecent(secretKey, clientKey, identity, now); got != tc.wantLimited {
				t.Fatalf("TooManyRecent after %d failures %s before now = %v, want %v (DefaultLoginAttemptsWindow must be ~15m, not 0 or unbounded)",
					DefaultLoginAttemptsLimit, tc.failureAge, got, tc.wantLimited)
			}
		})
	}
}
