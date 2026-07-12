package services

import (
	"testing"
	"time"
)

// TestDefaultLogoutAttemptsWindowRetainsAttempts pins the value of the
// package-default logout rate-limit window (DefaultLogoutAttemptsWindow) via
// observable behavior. NewAuthService wires this default into the logout
// AuthAttemptPolicy; production overrides it through ConfigureLogoutAttemptLimits,
// but the default remains the fallback whenever the configured window is invalid
// (< time.Second, which Configure ignores), so a broken default silently
// disables logout rate limiting on that path.
//
// All calls share a single `now`: every recorded attempt is only retained if it
// falls inside the window, so with a positive multi-minute window the
// (limit+1)-th call must trip the limiter. A zero or sub-second window (e.g. the
// ARITHMETIC_BASE mutant `15 * time.Minute` -> `15 / time.Minute` == 0) prunes
// every prior attempt on each call and never limits — this test then fails.
func TestDefaultLogoutAttemptsWindowRetainsAttempts(t *testing.T) {
	service := NewAuthService(nil)
	secretKey := []byte("logout-window-mutkill-secret")
	clientKey := "203.0.113.7"
	identity := "logout-window@example.com"
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)

	// Up to the limit, no call is rate-limited; each is recorded and must be
	// retained by the window so it counts toward the threshold.
	for attempt := 1; attempt <= DefaultLogoutAttemptsLimit; attempt++ {
		if service.CheckAndRecordLogoutAttempt(secretKey, clientKey, identity, now) {
			t.Fatalf("attempt %d was limited before reaching the limit of %d; the default window dropped earlier attempts",
				attempt, DefaultLogoutAttemptsLimit)
		}
	}

	// The next attempt exceeds DefaultLogoutAttemptsLimit within the window and
	// must be limited. A zero/sub-second default window would let it through.
	if !service.CheckAndRecordLogoutAttempt(secretKey, clientKey, identity, now) {
		t.Fatalf("attempt %d was not limited; DefaultLogoutAttemptsWindow failed to retain %d attempts within the window",
			DefaultLogoutAttemptsLimit+1, DefaultLogoutAttemptsLimit)
	}
}
