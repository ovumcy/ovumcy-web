package services

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestAuthSessionLifetimeWithinPolicyRefusesATokenMissingADate(t *testing.T) {
	now := jwt.NewNumericDate(time.Now())
	for name, claims := range map[string]*AuthSessionClaims{
		"nil claims":   nil,
		"no issued-at": {RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: now}},
		"no expiry":    {RegisteredClaims: jwt.RegisteredClaims{IssuedAt: now}},
		"neither date": {},
	} {
		if authSessionLifetimeWithinPolicy(claims, DefaultMaxAuthSessionLifetime) {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The ceiling filters, it does not revoke: the same token is judged against
// whatever ceiling is in force, so raising the ceiling back admits a token a
// lowered one refused. The documents say so; revoking takes a version bump.
func TestAuthSessionLifetimeCeilingIsAFilterNotARevocation(t *testing.T) {
	issued := time.Now()
	claims := &AuthSessionClaims{RegisteredClaims: jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(issued),
		ExpiresAt: jwt.NewNumericDate(issued.Add(365 * 24 * time.Hour)),
	}}
	for _, step := range []struct {
		ceiling time.Duration
		want    bool
	}{
		{ceiling: 365 * 24 * time.Hour, want: true},
		{ceiling: 8 * 24 * time.Hour, want: false},
		{ceiling: 365 * 24 * time.Hour, want: true},
	} {
		if got := authSessionLifetimeWithinPolicy(claims, step.ceiling); got != step.want {
			t.Fatalf("ceiling %s: accepted=%t, want %t", step.ceiling, got, step.want)
		}
	}
}

func TestResolveAuthSessionRefusesATokenOverTheCeilingBeforeTheLookup(t *testing.T) {
	secret := []byte("ceiling-test-secret")
	service := NewAuthService(nil)
	token, _, err := service.BuildAuthSessionTokenWithSessionID(secret, 7, models.RoleOwner, 1, 31*24*time.Hour, time.Now())
	if err != nil {
		t.Fatalf("build token: %v", err)
	}
	// A nil repository proves the refusal comes before the user lookup.
	_, _, err = service.ResolveAuthSession(t.Context(), secret, token, time.Now())
	if !errors.Is(err, ErrAuthSessionLifetimeOverPolicy) || !errors.Is(err, ErrAuthSessionTokenExpired) {
		t.Fatalf("ResolveAuthSession error = %v, want ErrAuthSessionLifetimeOverPolicy (an expiry)", err)
	}

	service.ConfigureMaxSessionLifetime(365 * 24 * time.Hour)
	zero := &AuthService{}
	if zero.sessionLifetimeCeiling() != DefaultMaxAuthSessionLifetime {
		t.Fatalf("a zero service ceiling = %s, want the default %s", zero.sessionLifetimeCeiling(), DefaultMaxAuthSessionLifetime)
	}
	if service.sessionLifetimeCeiling() != 365*24*time.Hour {
		t.Fatalf("ConfigureMaxSessionLifetime did not take: %s", service.sessionLifetimeCeiling())
	}
}
