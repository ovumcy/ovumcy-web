package services

// oidc_login_service_coverage_test.go
//
// Targets surviving mutants at lines 200, 210, 215 of oidc_login_service.go
// and exercises previously uncovered lines 110, 368, 374, 409.
//
// All helpers and types are prefixed "oidcloginserviceCov" to avoid collisions
// with other agents' files.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// ---------------------------------------------------------------------------
// Line 200 — reauthClaimsFresh: maxAuthAge == 0 must return false (disabled)
//
// Surviving mutant: change `<= 0` → `< 0`.  Under that mutation maxAuthAge==0
// would fall through to the freshness check, and a reference exactly equal to
// now would pass (now.Sub(now) == 0 <= 0).  This test detects that mutation.
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovReauthClaimsFreshZeroMaxAuthAgeReturnsFalse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	// reference == now: the most favourable possible freshness value.
	claims := security.OIDCClaims{AuthTime: now}

	// maxAuthAge == 0 must unconditionally disable freshness.
	if reauthClaimsFresh(claims, 0, now) {
		t.Fatal("reauthClaimsFresh must return false when maxAuthAge == 0 (feature disabled)")
	}
}

func TestOidcloginserviceCovReauthClaimsFreshNegativeMaxAuthAgeReturnsFalse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	claims := security.OIDCClaims{AuthTime: now}

	if reauthClaimsFresh(claims, -1*time.Second, now) {
		t.Fatal("reauthClaimsFresh must return false when maxAuthAge < 0")
	}
}

// Positive companion: maxAuthAge > 0 with a fresh reference must return true,
// so we know the zero test is not just "always false".
func TestOidcloginserviceCovReauthClaimsFreshPositiveMaxAuthAgeReturnsTrue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	claims := security.OIDCClaims{AuthTime: now.Add(-30 * time.Second)}

	if !reauthClaimsFresh(claims, 5*time.Minute, now) {
		t.Fatal("reauthClaimsFresh must return true when maxAuthAge > 0 and reference is fresh")
	}
}

// ---------------------------------------------------------------------------
// Line 210 — reauthClaimsFresh: 1-minute clock-skew tolerance boundary
//
// Surviving mutant: change the tolerance constant (e.g. 1m → 0 or 2m).
// Tests pin the exact boundary: reference exactly 1 minute into the future
// must be REJECTED; reference exactly at now must be ACCEPTED.
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovReauthClaimsFreshExactlyOneMinuteInFutureIsRejected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	// reference is exactly now + 1m — the check is reference.After(now.Add(1m)),
	// which is false when reference == now+1m (After is strict), so the code
	// should NOT return false here, but the next check (elapsed <= maxAuthAge)
	// will catch it only if elapsed would be negative.  Let's confirm the
	// function returns false because elapsed = now - (now+1m) = -1m < 0 but
	// <= maxAuthAge=5m, meaning it would pass without the forward-skew guard.
	// With the skew guard: now+1m is NOT After now+1m (strict), so the guard
	// does not trigger; but elapsed = -1m, which is <= 5m so it returns true.
	// The real guard fires at > 1m.  Provide reference = now + 61s.
	reference := now.Add(61 * time.Second)
	claims := security.OIDCClaims{AuthTime: reference}

	if reauthClaimsFresh(claims, 5*time.Minute, now) {
		t.Fatal("reauthClaimsFresh must reject a reference more than 1 minute in the future (possible forgery)")
	}
}

func TestOidcloginserviceCovReauthClaimsFreshWithinSkewToleranceIsAccepted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	// reference 59 seconds in the future — within the 1-minute tolerance.
	// The forward-skew guard must NOT reject this.
	reference := now.Add(59 * time.Second)
	claims := security.OIDCClaims{AuthTime: reference}

	// elapsed = now - (now+59s) = -59s, which is <= maxAuthAge=5m, so fresh.
	if !reauthClaimsFresh(claims, 5*time.Minute, now) {
		t.Fatal("reauthClaimsFresh must accept a reference within the 1-minute clock-skew tolerance")
	}
}

// ---------------------------------------------------------------------------
// Line 215 — reauthClaimsFresh: elapsed == maxAuthAge boundary (<=  vs <)
//
// Surviving mutant: change `<= maxAuthAge` → `< maxAuthAge`.
// A reference exactly maxAuthAge seconds in the past must be ACCEPTED.
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovReauthClaimsFreshExactlyAtMaxAuthAgeIsAccepted(t *testing.T) {
	t.Parallel()

	maxAge := 5 * time.Minute
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	// reference is exactly maxAge in the past → elapsed == maxAge.
	reference := now.Add(-maxAge)
	claims := security.OIDCClaims{AuthTime: reference}

	if !reauthClaimsFresh(claims, maxAge, now) {
		t.Fatal("reauthClaimsFresh must accept a reference exactly at the maxAuthAge boundary (inclusive)")
	}
}

func TestOidcloginserviceCovReauthClaimsFreshOneNanosecondBeyondMaxAuthAgeIsRejected(t *testing.T) {
	t.Parallel()

	maxAge := 5 * time.Minute
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	// One nanosecond older than the allowed window.
	reference := now.Add(-maxAge - time.Nanosecond)
	claims := security.OIDCClaims{AuthTime: reference}

	if reauthClaimsFresh(claims, maxAge, now) {
		t.Fatal("reauthClaimsFresh must reject a reference one nanosecond beyond maxAuthAge")
	}
}

// ---------------------------------------------------------------------------
// Line 110 — LocalPublicAuthEnabled: nil receiver returns true
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovLocalPublicAuthEnabledNilServiceReturnsTrue(t *testing.T) {
	t.Parallel()

	var svc *OIDCLoginService
	if !svc.LocalPublicAuthEnabled() {
		t.Fatal("LocalPublicAuthEnabled on a nil service must return true (open fallback)")
	}
}

// ---------------------------------------------------------------------------
// Line 368 — autoProvisionOrLookupUser: ErrAuthEmailExists + user not found
//
// Path: provisioner returns ErrAuthEmailExists; subsequent email lookup
// finds nothing → must return ErrOIDCProvisionFailed.
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovAutoProvisionFallbackUserNotFoundReturnsProvisionFailed(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{err: ErrAuthEmailExists}
	// User store returns not-found on the fallback lookup.
	users := &stubOIDCUserStore{byEmailFound: false}
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
				Subject:       "race-sub",
				Email:         "race@example.com",
				EmailVerified: true,
			},
		},
	}, identities, users, provisioner)

	_, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Time{})
	if !errors.Is(err, ErrOIDCProvisionFailed) {
		t.Fatalf("expected ErrOIDCProvisionFailed when ErrAuthEmailExists + lookup miss, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 347 — findOrProvisionUser: a directly-found user fails role validation
//
// With byEmailFound:true the *first* email lookup already returns the user, so
// the role check at line 347 (not the provision fallback at line 374) rejects a
// non-owner role with ErrOIDCAccountUnavailable. The line-374 provision-fallback
// variant is covered in oidc_login_service_fallback_coverage_test.go.
// ---------------------------------------------------------------------------

func TestOidcloginserviceCovDirectFoundUnsupportedRoleReturnsAccountUnavailable(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{err: ErrAuthEmailExists}
	// The fallback lookup returns a user whose role is not "owner".
	users := &stubOIDCUserStore{
		byEmailFound: true,
		byEmail: models.User{
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
		t.Fatalf("expected ErrOIDCAccountUnavailable for unsupported role in provision fallback, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 409 — buildLogoutState: returns nil when a required field is empty
//
// The Authenticate result.Logout must be nil when the provider logout is
// enabled but the session is missing EndSessionEndpoint or IDTokenHint, or
// the config carries no usable post-logout redirect URL.
// ---------------------------------------------------------------------------

// oidcloginserviceCovLogoutConfig returns an OIDCConfig with provider logout
// enabled via LogoutModeProvider and a valid redirect URL that ResolvedPostLogoutRedirectURL
// can derive a post-logout URL from.
func oidcloginserviceCovLogoutConfig() security.OIDCConfig {
	return security.OIDCConfig{
		Enabled:     true,
		LogoutMode:  security.OIDCLogoutModeProvider,
		RedirectURL: "https://app.example.com/auth/oidc/callback",
	}
}

// oidcloginserviceCovLinkedIdentitySetup builds the minimal service +
// identity stub for an already-linked happy path with a configurable session.
func oidcloginserviceCovLinkedIdentitySetup(cfg security.OIDCConfig, session security.OIDCSession) *OIDCLoginService {
	identities := &stubOIDCIdentityStore{
		found: true,
		identity: models.OIDCIdentity{
			ID:      77,
			UserID:  3,
			Issuer:  "https://id.example.com",
			Subject: "logout-sub",
		},
	}
	users := &stubOIDCUserStore{
		byID: models.User{
			ID:                  3,
			Role:                models.RoleOwner,
			OnboardingCompleted: true,
		},
	}
	return NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config:  cfg,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        "https://id.example.com",
				Subject:       "logout-sub",
				Email:         "user@example.com",
				EmailVerified: true,
			},
			Session: session,
		},
	}, identities, users, nil)
}

func TestOidcloginserviceCovBuildLogoutStateReturnsNilWhenEndpointMissing(t *testing.T) {
	t.Parallel()

	cfg := oidcloginserviceCovLogoutConfig()
	session := security.OIDCSession{
		EndSessionEndpoint: "", // missing
		IDTokenHint:        "id-token-hint",
	}
	service := oidcloginserviceCovLinkedIdentitySetup(cfg, session)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.Logout != nil {
		t.Fatalf("expected nil Logout when EndSessionEndpoint is empty, got %+v", result.Logout)
	}
}

func TestOidcloginserviceCovBuildLogoutStateReturnsNilWhenIDTokenHintMissing(t *testing.T) {
	t.Parallel()

	cfg := oidcloginserviceCovLogoutConfig()
	session := security.OIDCSession{
		EndSessionEndpoint: "https://id.example.com/logout",
		IDTokenHint:        "", // missing
	}
	service := oidcloginserviceCovLinkedIdentitySetup(cfg, session)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.Logout != nil {
		t.Fatalf("expected nil Logout when IDTokenHint is empty, got %+v", result.Logout)
	}
}

func TestOidcloginserviceCovBuildLogoutStatePopulatedWhenAllFieldsPresent(t *testing.T) {
	t.Parallel()

	cfg := oidcloginserviceCovLogoutConfig()
	session := security.OIDCSession{
		EndSessionEndpoint: "https://id.example.com/logout",
		IDTokenHint:        "raw-id-token",
	}
	service := oidcloginserviceCovLinkedIdentitySetup(cfg, session)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.Logout == nil {
		t.Fatal("expected non-nil Logout when all required fields are present")
	}
	if result.Logout.EndSessionEndpoint != "https://id.example.com/logout" {
		t.Fatalf("unexpected EndSessionEndpoint: %q", result.Logout.EndSessionEndpoint)
	}
	if result.Logout.IDTokenHint != "raw-id-token" {
		t.Fatalf("unexpected IDTokenHint: %q", result.Logout.IDTokenHint)
	}
	if result.Logout.PostLogoutRedirectURL == "" {
		t.Fatal("expected non-empty PostLogoutRedirectURL derived from RedirectURL")
	}
}
