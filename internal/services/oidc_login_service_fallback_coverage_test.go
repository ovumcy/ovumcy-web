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
	calls       int
	fallback    models.User
	fallbackAll []models.User
	fallbackErr error
}

func (s *oidcCovMissThenFindUserStore) FindAllByNormalizedEmail(_ context.Context, _ string) ([]models.User, error) {
	s.calls++
	switch {
	case s.calls == 1:
		return nil, nil
	case s.fallbackErr != nil:
		return nil, s.fallbackErr
	case s.fallbackAll != nil:
		return s.fallbackAll, nil
	}
	return []models.User{s.fallback}, nil
}

func newOIDCCovAutoProvisionService(users OIDCUserStore, identities *stubOIDCIdentityStore, provisioner *stubOIDCAutoProvisioner) *OIDCLoginService {
	return NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config: security.OIDCConfig{
			Enabled:       true,
			AutoProvision: true,
		},
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "shared-sub",
				Email:         "shared@example.com",
				EmailVerified: true,
			},
		},
	}, identities, users, provisioner)
}

func TestOIDCLoginServiceEmailLookupStoreErrorFailsResolution(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{}
	identities := &stubOIDCIdentityStore{}
	users := &stubOIDCUserStore{byEmailErr: errors.New("db down")}
	service := newOIDCCovAutoProvisionService(users, identities, provisioner)

	_, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{})
	if !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("expected ErrOIDCIdentityResolveFailed, got %v", err)
	}
	if provisioner.called || identities.createCallSeen {
		t.Fatalf("a failed email lookup must neither provision nor link: provisioned=%v linked=%v", provisioner.called, identities.createCallSeen)
	}
}

func TestOIDCLoginServiceAutoProvisionConflictFallbackLookupFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		users *oidcCovMissThenFindUserStore
		want  error
	}{
		{
			name: "store error",
			users: &oidcCovMissThenFindUserStore{
				stubOIDCUserStore: &stubOIDCUserStore{},
				fallbackErr:       errors.New("db down"),
			},
			want: ErrOIDCIdentityResolveFailed,
		},
		{
			name: "ambiguous address",
			users: &oidcCovMissThenFindUserStore{
				stubOIDCUserStore: &stubOIDCUserStore{},
				fallbackAll: []models.User{
					{ID: 3, Email: "shared@example.com", Role: models.RoleOwner},
					{ID: 4, Email: "shared@example.com", Role: models.RoleOwner},
				},
			},
			want: ErrOIDCLinkRequiresConfirmation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provisioner := &stubOIDCAutoProvisioner{err: ErrAuthEmailExists}
			identities := &stubOIDCIdentityStore{}
			service := newOIDCCovAutoProvisionService(tc.users, identities, provisioner)

			_, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if !provisioner.called || tc.users.calls != 2 {
				t.Fatalf("expected miss, provision conflict, re-lookup; provisioned=%v lookups=%d", provisioner.called, tc.users.calls)
			}
			if identities.createCallSeen {
				t.Fatalf("a failed re-lookup must not link an identity, got %+v", identities.created)
			}
		})
	}
}

func TestOIDCLoginServiceAutoProvisionConflictFallbackRejectsUnsupportedRole(t *testing.T) {
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
