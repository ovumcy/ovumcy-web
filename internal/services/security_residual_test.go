package services

import (
	"testing"
	"time"
)

// Targeted coverage for two security-sensitive boundary mutants that survived the
// broad coverage pass. These guard the recovery-code alphabet and the login
// lockout configuration, where the exact comparison matters.

func TestIsCanonicalRecoveryCodeBody_AlphabetBoundaries(t *testing.T) {
	// The exact edges of the accepted alphabet (A, Z, 0, 9) must be accepted.
	if !isCanonicalRecoveryCodeBody("AZ09AZ09AZ09") {
		t.Fatal("expected the alphabet edges A/Z/0/9 to be accepted")
	}

	// The character immediately outside each accepted range must be rejected.
	// This kills boundary mutations on `c >= 'A'`, `c <= 'Z'`, `c >= '0'`, `c <= '9'`.
	for _, body := range []string{
		"@Z09AZ09AZ09", // '@' is one below 'A'
		"A[09AZ09AZ09", // '[' is one above 'Z'
		"AZ/9AZ09AZ09", // '/' is one below '0'
		"AZ0:AZ09AZ09", // ':' is one above '9'
	} {
		if isCanonicalRecoveryCodeBody(body) {
			t.Fatalf("expected out-of-range character in %q to be rejected", body)
		}
	}

	// Length boundary: only exactly 12 characters is canonical.
	if isCanonicalRecoveryCodeBody("AZ09AZ09AZ0") {
		t.Fatal("expected an 11-character body to be rejected")
	}
	if isCanonicalRecoveryCodeBody("AZ09AZ09AZ090") {
		t.Fatal("expected a 13-character body to be rejected")
	}
}

func TestAuthAttemptPolicyConfigure_AttemptsLowerBound(t *testing.T) {
	// Assert the attempts floor purely through the public TooManyRecent surface,
	// never by reading the unexported policy.attempts. A wide window keeps only
	// the attempts threshold in play.
	secretKey := []byte("attempts-lower-bound-secret")
	clientKey := "198.51.100.7"
	identity := "floor@example.com"
	now := time.Now().UTC()

	// Start above the floor (threshold 8), then lower it to 1.
	policy := NewAuthAttemptPolicy("login", nil, 8, time.Minute)

	// attempts == 1 is valid (lock after a single failure) and must be applied.
	// Kills the `attempts >= 1` -> `attempts > 1` boundary mutant, under which
	// the threshold would stay at 8 and one failure would NOT trip the lock.
	policy.Configure(1, time.Minute)
	if policy.TooManyRecent(secretKey, clientKey, identity, now) {
		t.Fatal("expected the throttle to be open before any failure")
	}
	policy.AddFailure(secretKey, clientKey, identity, now)
	if !policy.TooManyRecent(secretKey, clientKey, identity, now) {
		t.Fatal("Configure(1) must lower the threshold to 1: a single failure should trip the lock (kills >=→>)")
	}

	// Zero attempts are rejected; the previous floor of 1 is kept. Were 0 wrongly
	// applied, TooManyRecent would report throttled with zero failures (limit 0 is
	// always exceeded), so a clean state must still read as OPEN after Configure(0).
	policy.Reset(secretKey, clientKey, identity)
	policy.Configure(0, time.Minute)
	if policy.TooManyRecent(secretKey, clientKey, identity, now) {
		t.Fatal("Configure(0) must be ignored: a clean state must stay open, not throttle at limit 0")
	}
	policy.AddFailure(secretKey, clientKey, identity, now)
	if !policy.TooManyRecent(secretKey, clientKey, identity, now) {
		t.Fatal("threshold should remain 1 after Configure(0): a single failure should still trip the lock")
	}
}
