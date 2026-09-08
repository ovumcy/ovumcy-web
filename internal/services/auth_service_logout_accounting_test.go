package services

import (
	"fmt"
	"testing"
	"time"
)

func TestLogoutAttemptIdentityQualifiesTheSessionByItsOwner(t *testing.T) {
	t.Parallel()

	if got := LogoutAttemptIdentity(7, "session-a"); got != "7:session-a" {
		t.Fatalf("LogoutAttemptIdentity(7, session-a) = %q, want 7:session-a", got)
	}
	if got := LogoutAttemptIdentity(7, " session-a "); got != "7:session-a" {
		t.Fatalf("surrounding whitespace must not mint a second identity: %q", got)
	}
	if got := LogoutAttemptIdentity(7, ""); got != "7" {
		t.Fatalf("a session-less caller keys on the owner alone, got %q", got)
	}
	if LogoutAttemptIdentity(7, "session-a") == LogoutAttemptIdentity(8, "session-a") {
		t.Fatal("two owners with the same session id must not share a budget")
	}
}

// TestLogoutAccountingNeverTouchesAnotherBudget: a logout is an attempt
// against the logout budget only. Even with the logout policy on the SAME
// limiter as the login policy (bootstrap gives it its own; this is the worst
// case), a thousand sign-outs neither add a failure to, nor reset, a login
// lockout or a partially spent login budget — and the logout budget itself
// still trips per session.
func TestLogoutAccountingNeverTouchesAnotherBudget(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	secretKey := []byte("logout-accounting-secret")
	login := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)
	service := NewAuthService(nil)
	service.logoutAttemptPolicy = NewAuthAttemptPolicy("logout", limiter, DefaultLogoutAttemptsLimit, DefaultLogoutAttemptsWindow)
	now := time.Now().UTC()

	for range DefaultLoginAttemptsLimit {
		login.AddFailure(secretKey, "203.0.113.7", "locked@example.com", now)
	}
	partial := DefaultLoginAttemptsLimit - 2
	for range partial {
		login.AddFailure(secretKey, "203.0.113.8", "partial@example.com", now)
	}

	// One session spends its whole budget from one address; the next sign-out
	// of that session is refused, and no other session's first sign-out is.
	for attempt := range DefaultLogoutAttemptsLimit {
		if service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1, "session-00"), now) {
			t.Fatalf("sign-out %d of session-00 was refused before the budget of %d was spent", attempt+1, DefaultLogoutAttemptsLimit)
		}
	}
	if !service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1, "session-00"), now) {
		t.Fatal("the logout budget of one session never tripped")
	}
	// Stays below the limiter's per-scope cap (two keys per sign-out), so what
	// is measured here is the accounting, not the eviction covered elsewhere.
	const owners, sessions = 25, 10
	for owner := range owners {
		for session := range sessions {
			identity := LogoutAttemptIdentity(uint(owner+2), fmt.Sprintf("session-%02d", session))
			if service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", identity, now) {
				t.Fatalf("owner %d session %d: the first sign-out of a session was refused by another session's spend", owner+2, session)
			}
		}
	}

	if !login.TooManyRecent(secretKey, "203.0.113.7", "locked@example.com", now) {
		t.Fatal("sign-outs reset or evicted a login lockout")
	}
	for range DefaultLoginAttemptsLimit - partial {
		if login.TooManyRecent(secretKey, "203.0.113.8", "partial@example.com", now) {
			t.Fatal("sign-outs added failures to a login budget")
		}
		login.AddFailure(secretKey, "203.0.113.8", "partial@example.com", now)
	}
	if !login.TooManyRecent(secretKey, "203.0.113.8", "partial@example.com", now) {
		t.Fatal("sign-outs erased a partially spent login budget")
	}
}
