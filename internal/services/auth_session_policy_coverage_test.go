package services

// auth_session_policy_coverage_test.go — covers the non-positive-ttl default in
// BuildAuthSessionTokenWithVersionAndSessionID (gremlins reported the ttl
// fallback assignment on line 46 as NOT COVERED).

import (
	"errors"
	"testing"
	"time"
)

func TestAuthsessionpolicyCovBuildDefaultsNonPositiveTTL(t *testing.T) {
	secret := []byte("test-auth-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	// ttl <= 0 must default to 7 days (line 45–47). Pin the boundary: the token
	// is valid well before 7 days and expired well after. The ttl=0 case also
	// kills a `<= 0` → `< 0` boundary mutation (which would leave ttl=0 and make
	// the token expire immediately).
	for _, ttl := range []time.Duration{0, -time.Hour} {
		token, sessionID, err := BuildAuthSessionTokenWithVersionAndSessionID(secret, 42, "owner", 1, ttl, now)
		if err != nil {
			t.Fatalf("BuildAuthSessionTokenWithVersionAndSessionID(ttl=%v) unexpected error: %v", ttl, err)
		}
		if sessionID == "" {
			t.Fatalf("expected a session id (ttl=%v)", ttl)
		}
		if _, err := ParseAuthSessionToken(secret, token, now.Add(6*24*time.Hour)); err != nil {
			t.Fatalf("token with defaulted ttl should be valid at +6d (ttl=%v), got %v", ttl, err)
		}
		if _, err := ParseAuthSessionToken(secret, token, now.Add(8*24*time.Hour)); !errors.Is(err, ErrAuthSessionTokenExpired) {
			t.Fatalf("token with defaulted ttl should expire by +8d (ttl=%v), got %v", ttl, err)
		}
	}
}
