package services

import (
	"testing"
	"time"
)

// TestMR3Auth_ConfigureWindowBoundary pins auth_attempt_policy.go:43:12
// `if window >= time.Second` through the PUBLIC rate-limit surface only. A
// window of exactly time.Second must be ACCEPTED (kills BOUNDARY >=→>), while a
// sub-second window must be IGNORED, leaving the last accepted window in place
// (kills NEGATION). The window is observed by how long a recorded failure keeps
// TooManyRecent tripped — never by reading the unexported policy.window.
func TestMR3Auth_ConfigureWindowBoundary(t *testing.T) {
	t.Parallel()

	secretKey := []byte("mr3auth-window-secret")
	clientKey := "203.0.113.5"
	identity := "boundary@example.com"
	now := time.Now().UTC()

	// Constructor seeds a 10-minute window and a 1-failure threshold, so a single
	// AddFailure trips TooManyRecent and only clears once the failure ages past
	// the configured window.
	policy := NewAuthAttemptPolicy("mr3auth", nil, 1, 10*time.Minute)

	// Exactly time.Second must be accepted: the failure then ages out just past
	// 1s. If the boundary mutant (>=→>) rejected it, the constructor's 10-minute
	// window would remain and the failure would still be counted 2s later.
	policy.Configure(1, time.Second)
	policy.AddFailure(secretKey, clientKey, identity, now)
	if !policy.TooManyRecent(secretKey, clientKey, identity, now) {
		t.Fatal("expected throttle to engage immediately after a failure")
	}
	if policy.TooManyRecent(secretKey, clientKey, identity, now.Add(2*time.Second)) {
		t.Fatal("window==time.Second must be accepted: the failure should age out after 2s (kills >=→>)")
	}

	// A sub-second window must be ignored, leaving the accepted 1s window in
	// place. Under the negation/`<=` family the 500ms window would install and
	// the failure would wrongly age out before 750ms.
	policy.Reset(secretKey, clientKey, identity)
	policy.Configure(1, 500*time.Millisecond)
	policy.AddFailure(secretKey, clientKey, identity, now)
	if !policy.TooManyRecent(secretKey, clientKey, identity, now.Add(750*time.Millisecond)) {
		t.Fatal("sub-second window must be ignored: a failure 750ms later is still within the retained 1s window (kills negation)")
	}
	if policy.TooManyRecent(secretKey, clientKey, identity, now.Add(2*time.Second)) {
		t.Fatal("window should remain 1s: the failure should still age out after 2s")
	}
}
