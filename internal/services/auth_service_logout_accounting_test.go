package services

import (
	"fmt"
	"testing"
	"time"
)

func TestLogoutAttemptIdentityKeysOnTheOwnerNotTheSession(t *testing.T) {
	t.Parallel()

	if got := LogoutAttemptIdentity(7); got != "7" {
		t.Fatalf("LogoutAttemptIdentity(7) = %q, want 7", got)
	}
	if LogoutAttemptIdentity(7) == LogoutAttemptIdentity(8) {
		t.Fatal("two owners must not share one logout budget")
	}
}

// TestLogoutClientBucketSeparatesOwnersBehindOneAddress pins the half of the
// keying that the session used to carry: the client bucket is (address,
// account), so a household — or a serial test run — behind one address does not
// spend one shared per-address budget.
func TestLogoutClientBucketSeparatesOwnersBehindOneAddress(t *testing.T) {
	t.Parallel()

	if logoutClientBucket("203.0.113.7", "7") == logoutClientBucket("203.0.113.7", "8") {
		t.Fatal("two owners behind one address must not share the client bucket")
	}
	if got := logoutClientBucket("203.0.113.7", ""); got != "203.0.113.7" {
		t.Fatalf("an identity-less caller keys on the address alone, got %q", got)
	}
}

// TestLogoutBudgetIsSpendableAcrossSuccessiveSessions is the regression for a
// budget keyed on the session id. RevokeAuthSessions bumps AuthSessionVersion,
// so the token an owner signed out with is refused by ResolveAuthSession and the
// same session never reaches the handler twice: a session-keyed budget recorded
// exactly one attempt per key and could never trip, leaving the account side of
// the logout route with no cap at all. The owner key counts the succession.
func TestLogoutBudgetIsSpendableAcrossSuccessiveSessions(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	secretKey := []byte("logout-spend-secret")
	service := NewAuthService(nil)
	service.logoutAttemptPolicy = NewAuthAttemptPolicy("logout", limiter, DefaultLogoutAttemptsLimit, DefaultLogoutAttemptsWindow)
	now := time.Now().UTC()

	for attempt := range DefaultLogoutAttemptsLimit {
		if service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1), now) {
			t.Fatalf("sign-out %d was refused before the budget of %d was spent", attempt+1, DefaultLogoutAttemptsLimit)
		}
	}
	if !service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1), now) {
		t.Fatal("the logout budget never tripped: it cannot be spent")
	}
	if service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(2), now) {
		t.Fatal("one owner's spent budget refused another owner behind the same address")
	}
}

// TestLogoutAccountingNeverTouchesAnotherBudget: a logout is an attempt
// against the logout budget only. Even with the logout policy on the SAME
// limiter as the login policy (bootstrap gives it its own; this is the worst
// case), a thousand sign-outs neither add a failure to, nor reset, a login
// lockout or a partially spent login budget — and the logout budget itself
// still trips per owner.
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

	// One owner spends its whole budget from one address; its next sign-out is
	// refused, and no other owner's first sign-out is.
	for attempt := range DefaultLogoutAttemptsLimit {
		if service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1), now) {
			t.Fatalf("sign-out %d of owner 1 was refused before the budget of %d was spent", attempt+1, DefaultLogoutAttemptsLimit)
		}
	}
	if !service.CheckAndRecordLogoutAttempt(secretKey, "203.0.113.7", LogoutAttemptIdentity(1), now) {
		t.Fatal("the logout budget of one owner never tripped")
	}
	// Stays below the limiter's per-scope cap (two keys per sign-out), so what
	// is measured here is the accounting, not the eviction covered elsewhere.
	const owners = 250
	for owner := range owners {
		identity := LogoutAttemptIdentity(uint(owner + 2))
		if service.CheckAndRecordLogoutAttempt(secretKey, fmt.Sprintf("203.0.113.%d", owner%256), identity, now) {
			t.Fatalf("owner %d: the first sign-out was refused by another owner's spend", owner+2)
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
