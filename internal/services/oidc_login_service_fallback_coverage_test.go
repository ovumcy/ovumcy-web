package services

// oidc_login_service_fallback_coverage_test.go — covers the role check in the
// auto-provision conflict fallback (oidc_login_service.go:374), which gremlins
// reported as NOT COVERED. The pre-existing "fallback" test in
// oidc_login_service_coverage_test.go actually hit the direct-found role check
// (line 347) because the single-result stubOIDCUserStore returns found on the
// FIRST lookup; reaching line 374 needs a miss-then-find sequence.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// oidcCovMissThenFindUserStore returns not-found on the first email lookup (so
// auto-provisioning is attempted) and a stored user on every later lookup (the
// post-conflict fallback in autoProvisionOrLookupUser). It embeds the standard
// stub for the rest of the interface (FindByID).
type oidcCovMissThenFindUserStore struct {
	*stubOIDCUserStore
	calls    int
	fallback models.User
}

func (s *oidcCovMissThenFindUserStore) FindByNormalizedEmailOptional(_ context.Context, _ string) (models.User, bool, error) {
	s.calls++
	if s.calls == 1 {
		return models.User{}, false, nil
	}
	return s.fallback, true, nil
}

func TestOidcloginserviceCovAutoProvisionFallbackUnsupportedRoleReachesLine374(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{err: ErrAuthEmailExists}
	users := &oidcCovMissThenFindUserStore{
		stubOIDCUserStore: &stubOIDCUserStore{},
		fallback: models.User{
			ID:    55,
			Email: "operator@example.com",
			Role:  "operator", // ValidateSupportedWebUser rejects non-owner roles
		},
	}
	identities := &stubOIDCIdentityStore{}

	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config: security.OIDCConfig{
			Enabled:       true,
			AutoProvision: true,
		},
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "operator-sub",
				Email:         "operator@example.com",
				EmailVerified: true,
			},
		},
	}, identities, users, provisioner)

	_, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{})
	if !errors.Is(err, ErrOIDCAccountUnavailable) {
		t.Fatalf("expected ErrOIDCAccountUnavailable from fallback role check, got %v", err)
	}
	// Proves the path actually reached the fallback (two lookups) rather than the
	// direct-found role check at line 347 (one lookup) — the bug in the original.
	if users.calls < 2 {
		t.Fatalf("expected miss-then-find (>=2 lookups) to reach line 374, got %d", users.calls)
	}
}
