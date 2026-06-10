package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

type stubOIDCProviderClient struct {
	enabled       bool
	authURL       string
	config        security.OIDCConfig
	exchange      security.OIDCExchangeResult
	authErr       error
	exchangeErr   error
	lastAuthExtra map[string]string
}

func (stub *stubOIDCProviderClient) Enabled() bool {
	return stub.enabled
}

func (stub *stubOIDCProviderClient) LocalPublicAuthEnabled() bool {
	return stub.config.LocalPublicAuthEnabled()
}

func (stub *stubOIDCProviderClient) Config() security.OIDCConfig {
	return stub.config
}

func (stub *stubOIDCProviderClient) AuthCodeURL(_ context.Context, _, _, _ string, extra map[string]string) (string, error) {
	stub.lastAuthExtra = extra
	if stub.authErr != nil {
		return "", stub.authErr
	}
	return stub.authURL, nil
}

func (stub *stubOIDCProviderClient) ExchangeCode(context.Context, string, string, string) (security.OIDCExchangeResult, error) {
	if stub.exchangeErr != nil {
		return security.OIDCExchangeResult{}, stub.exchangeErr
	}
	return stub.exchange, nil
}

type stubOIDCIdentityStore struct {
	identity       models.OIDCIdentity
	found          bool
	findErr        error
	createErr      error
	touchedID      uint
	touchedAt      time.Time
	created        models.OIDCIdentity
	createCallSeen bool
}

func (stub *stubOIDCIdentityStore) FindByIssuerSubject(context.Context, string, string) (models.OIDCIdentity, bool, error) {
	if stub.findErr != nil {
		return models.OIDCIdentity{}, false, stub.findErr
	}
	if !stub.found {
		return models.OIDCIdentity{}, false, nil
	}
	return stub.identity, true, nil
}

func (stub *stubOIDCIdentityStore) Create(ctx context.Context, identity *models.OIDCIdentity) error {
	stub.createCallSeen = true
	if identity != nil {
		stub.created = *identity
	}
	return stub.createErr
}

func (stub *stubOIDCIdentityStore) TouchLastUsed(ctx context.Context, identityID uint, usedAt time.Time) error {
	stub.touchedID = identityID
	stub.touchedAt = usedAt
	return nil
}

type stubOIDCUserStore struct {
	byID            models.User
	byIDErr         error
	byEmail         models.User
	byEmailFound    bool
	byEmailErr      error
	lastLookupEmail string
}

type stubOIDCAutoProvisioner struct {
	user   models.User
	err    error
	called bool
	email  string
}

func (stub *stubOIDCAutoProvisioner) AutoProvisionOwnerAccount(ctx context.Context, email string, _ time.Time) (models.User, error) {
	stub.called = true
	stub.email = email
	if stub.err != nil {
		return models.User{}, stub.err
	}
	return stub.user, nil
}

func (stub *stubOIDCUserStore) FindByID(context.Context, uint) (models.User, error) {
	if stub.byIDErr != nil {
		return models.User{}, stub.byIDErr
	}
	return stub.byID, nil
}

func (stub *stubOIDCUserStore) FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error) {
	stub.lastLookupEmail = email
	if stub.byEmailErr != nil {
		return models.User{}, false, stub.byEmailErr
	}
	if !stub.byEmailFound {
		return models.User{}, false, nil
	}
	return stub.byEmail, true, nil
}

func TestOIDCLoginServiceStartAuthRequiresEnabledProvider(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.StartAuth(context.Background(), "state", "nonce", "verifier"); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

func TestOIDCLoginServiceAuthenticateUsesExistingIdentityLink(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 28, 11, 30, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found: true,
		identity: models.OIDCIdentity{
			ID:      44,
			UserID:  7,
			Issuer:  "https://id.example.com",
			Subject: "owner-subject",
		},
	}
	users := &stubOIDCUserStore{
		byID: models.User{
			ID:                  7,
			Role:                models.RoleOwner,
			OnboardingCompleted: true,
		},
	}
	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "owner-subject",
				Email:         "owner@example.com",
				EmailVerified: true,
			},
		},
	}, identities, users, nil)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", now)
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.NewlyLinked {
		t.Fatal("did not expect existing identity to be linked again")
	}
	if result.User.ID != 7 {
		t.Fatalf("expected linked user id 7, got %d", result.User.ID)
	}
	if identities.touchedID != 44 {
		t.Fatalf("expected last-used touch for identity 44, got %d", identities.touchedID)
	}
	if !identities.touchedAt.Equal(now) {
		t.Fatalf("expected last-used timestamp %s, got %s", now, identities.touchedAt)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect Create() for an existing identity link")
	}
}

// TestOIDCLoginServiceAuthenticateRequiresConfirmationOnFirstLinkToExistingEmail
// pins the defence against malicious / sloppy upstream IdP account takeover:
// when an OIDC callback resolves to a pre-existing local user by email but no
// (issuer, subject) link exists yet, Authenticate must REFUSE to auto-link.
// It returns ErrOIDCLinkRequiresConfirmation with the pending claims so the
// handler can demand explicit password confirmation before
// ConfirmAndLinkIdentity creates the link.
func TestOIDCLoginServiceAuthenticateRequiresConfirmationOnFirstLinkToExistingEmail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{}
	users := &stubOIDCUserStore{
		byEmailFound: true,
		byEmail: models.User{
			ID:                  9,
			Email:               "owner@example.com",
			Role:                models.RoleOwner,
			OnboardingCompleted: true,
		},
	}
	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "first-login-sub",
				Email:         " Owner@Example.com ",
				EmailVerified: true,
			},
		},
	}, identities, users, nil)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", now)
	if !errors.Is(err, ErrOIDCLinkRequiresConfirmation) {
		t.Fatalf("Authenticate() expected ErrOIDCLinkRequiresConfirmation, got err=%v result=%+v", err, result)
	}
	if result.NewlyLinked {
		t.Fatal("did not expect NewlyLinked when link confirmation is required")
	}
	if result.User.ID != 9 {
		t.Fatalf("expected pending-link target user id 9, got %d", result.User.ID)
	}
	if users.lastLookupEmail != "owner@example.com" {
		t.Fatalf("expected normalized email lookup, got %q", users.lastLookupEmail)
	}
	if result.PendingLinkClaims == nil {
		t.Fatal("expected PendingLinkClaims to carry the pending issuer/subject")
	}
	if result.PendingLinkClaims.Issuer != "https://id.example.com" || result.PendingLinkClaims.Subject != "first-login-sub" {
		t.Fatalf("unexpected pending claims: %+v", result.PendingLinkClaims)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect Create() — auto-link to a pre-existing email is exactly the vulnerability this guard prevents")
	}
}

// TestOIDCLoginServiceConfirmAndLinkIdentityPersistsLink covers the path the
// handler uses after the password-confirmation step succeeds.
func TestOIDCLoginServiceConfirmAndLinkIdentityPersistsLink(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(&stubOIDCProviderClient{enabled: true}, identities, &stubOIDCUserStore{}, nil)

	claims := security.OIDCClaims{
		Issuer:  "https://id.example.com",
		Subject: "first-login-sub",
		Email:   "owner@example.com",
	}
	if err := service.ConfirmAndLinkIdentity(context.Background(), 9, claims, now); err != nil {
		t.Fatalf("ConfirmAndLinkIdentity() unexpected error: %v", err)
	}
	if !identities.createCallSeen {
		t.Fatal("expected Create() after confirmation")
	}
	if identities.created.UserID != 9 || identities.created.Issuer != claims.Issuer || identities.created.Subject != claims.Subject {
		t.Fatalf("unexpected persisted identity: %+v", identities.created)
	}
	if identities.created.LastUsedAt == nil || !identities.created.LastUsedAt.Equal(now) {
		t.Fatalf("expected LastUsedAt=%s, got %+v", now, identities.created.LastUsedAt)
	}
}

// TestOIDCLoginServiceConfirmAndLinkIdentityRefusesCrossUserClaim ensures the
// confirmation step fails closed if a race or DB inconsistency means the
// (issuer, subject) was already claimed by a different user between callback
// and confirmation submission.
func TestOIDCLoginServiceConfirmAndLinkIdentityRefusesCrossUserClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found: true,
		identity: models.OIDCIdentity{
			ID:      11,
			UserID:  44, // already linked to a DIFFERENT user
			Issuer:  "https://id.example.com",
			Subject: "first-login-sub",
		},
	}
	service := NewOIDCLoginService(&stubOIDCProviderClient{enabled: true}, identities, &stubOIDCUserStore{}, nil)

	claims := security.OIDCClaims{Issuer: "https://id.example.com", Subject: "first-login-sub"}
	if err := service.ConfirmAndLinkIdentity(context.Background(), 9, claims, now); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed for cross-user claim, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect Create() when (issuer, subject) is already linked to another user")
	}
}

func TestOIDCLoginServiceAuthenticateRejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "no-verified-email",
				Email:         "owner@example.com",
				EmailVerified: false,
			},
		},
	}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{}); !errors.Is(err, ErrOIDCAccountUnavailable) {
		t.Fatalf("expected ErrOIDCAccountUnavailable, got %v", err)
	}
}

// TestOIDCLoginServiceAuthenticateMapsLinkPersistenceFailure exercises the
// link-failure mapping on the auto-provision path (the only Authenticate path
// that still calls linkIdentity inline — the existing-email path now requires
// confirmation and links through ConfirmAndLinkIdentity instead, covered
// separately).
func TestOIDCLoginServiceAuthenticateMapsLinkPersistenceFailure(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{
		user: models.User{ID: 5, Email: "owner@example.com", Role: models.RoleOwner},
	}
	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config: security.OIDCConfig{
			Enabled:                     true,
			AutoProvision:               true,
			AutoProvisionAllowedDomains: []string{"example.com"},
		},
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "duplicate-link",
				Email:         "owner@example.com",
				EmailVerified: true,
			},
		},
	}, &stubOIDCIdentityStore{
		createErr: errors.New("duplicate key"),
	}, &stubOIDCUserStore{}, provisioner)

	if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{}); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed, got %v", err)
	}
}

func TestOIDCLoginServiceAuthenticateAutoProvisionsWhenEnabled(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{
		user: models.User{
			ID:                  17,
			Email:               "owner@example.com",
			Role:                models.RoleOwner,
			LocalAuthEnabled:    false,
			OnboardingCompleted: false,
		},
	}
	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config: security.OIDCConfig{
			Enabled:                     true,
			AutoProvision:               true,
			AutoProvisionAllowedDomains: []string{"example.com"},
		},
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "autoprovision-sub",
				Email:         "owner@example.com",
				EmailVerified: true,
			},
		},
	}, identities, &stubOIDCUserStore{}, provisioner)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{})
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if !result.AutoProvisioned {
		t.Fatal("expected auto-provisioned result")
	}
	if !provisioner.called || provisioner.email != "owner@example.com" {
		t.Fatalf("expected auto-provisioner call for normalized email, got called=%v email=%q", provisioner.called, provisioner.email)
	}
	if !identities.createCallSeen || identities.created.UserID != 17 {
		t.Fatalf("expected persisted identity link for auto-provisioned user, got %+v", identities.created)
	}
}

func TestOIDCLoginServiceRejectsAutoProvisionOutsideAllowedDomains(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config: security.OIDCConfig{
			Enabled:                     true,
			AutoProvision:               true,
			AutoProvisionAllowedDomains: []string{"example.com"},
		},
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "autoprovision-sub",
				Email:         "owner@blocked.example.org",
				EmailVerified: true,
			},
		},
	}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, &stubOIDCAutoProvisioner{})

	if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{}); !errors.Is(err, ErrOIDCAccountUnavailable) {
		t.Fatalf("expected ErrOIDCAccountUnavailable, got %v", err)
	}
}
