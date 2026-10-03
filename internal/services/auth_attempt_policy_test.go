package services

import (
	"strings"
	"testing"
	"time"
)

func TestAuthAttemptPolicyKeysUseScopedHMACFingerprint(t *testing.T) {
	policy := NewAuthAttemptPolicy("login", NewAttemptLimiter(), 2, time.Hour)
	secretKey := []byte("test-secret-key")

	keys := policy.keys(secretKey, "10.0.0.1", "owner@example.com")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != "login:client:10.0.0.1" {
		t.Fatalf("unexpected client key: %q", keys[0])
	}
	if !strings.HasPrefix(keys[1], "login:identity:") {
		t.Fatalf("expected scoped identity key, got %q", keys[1])
	}
	if strings.Contains(keys[1], "owner@example.com") {
		t.Fatalf("identity key should not contain the raw email: %q", keys[1])
	}

	repeated := policy.keys(secretKey, "10.0.0.9", "owner@example.com")
	if repeated[1] != keys[1] {
		t.Fatalf("expected deterministic identity fingerprint for same secret and identity")
	}

	withDifferentSecret := policy.keys([]byte("different-secret-key"), "10.0.0.9", "owner@example.com")
	if withDifferentSecret[1] == keys[1] {
		t.Fatalf("expected identity fingerprint to change when the secret changes")
	}
}

func TestAuthAttemptPolicyKeysOmitIdentityFingerprintForBlankIdentity(t *testing.T) {
	policy := NewAuthAttemptPolicy("recovery", NewAttemptLimiter(), 2, time.Hour)

	keys := policy.keys([]byte("test-secret-key"), "127.0.0.1", "   ")
	if len(keys) != 1 {
		t.Fatalf("expected only client key for blank identity, got %d", len(keys))
	}
	if keys[0] != "recovery:client:127.0.0.1" {
		t.Fatalf("unexpected client key: %q", keys[0])
	}
}

// TestNewAuthAttemptPolicyAppliesTheConfigureFloors pins that the constructor
// takes its figures through the same floors Configure applies: a limit below one
// would refuse every attempt (the budget would lock the account out before any
// compare) and a window below a second would drop every attempt at once (the
// budget would never limit). Either falls back to the default instead.
func TestNewAuthAttemptPolicyAppliesTheConfigureFloors(t *testing.T) {
	secretKey := []byte("floor-secret-key")
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	for name, figures := range map[string]struct {
		attempts int
		window   time.Duration
	}{
		"zero limit":           {attempts: 0, window: time.Hour},
		"negative limit":       {attempts: -3, window: time.Hour},
		"zero window":          {attempts: 2, window: 0},
		"sub-second window":    {attempts: 2, window: time.Millisecond},
		"negative window":      {attempts: 2, window: -time.Minute},
		"both below the floor": {attempts: 0, window: 0},
	} {
		t.Run(name, func(t *testing.T) {
			policy := NewAuthAttemptPolicy("floor", NewAttemptLimiter(), figures.attempts, figures.window)

			if policy.attempts < 1 {
				t.Fatalf("limit = %d, want at least 1", policy.attempts)
			}
			if policy.window < time.Second {
				t.Fatalf("window = %s, want at least one second", policy.window)
			}
			reservation, admitted := policy.Reserve(secretKey, "10.0.0.1", "owner@example.com", now)
			if !admitted || reservation == nil {
				t.Fatal("a fresh budget refused its first attempt")
			}
			spent := 1
			for ; spent <= DefaultLoginAttemptsLimit+policy.attempts; spent++ {
				if _, ok := policy.Reserve(secretKey, "10.0.0.1", "owner@example.com", now); !ok {
					break
				}
			}
			if spent > DefaultLoginAttemptsLimit+policy.attempts {
				t.Fatal("the budget never limited")
			}
		})
	}
}
