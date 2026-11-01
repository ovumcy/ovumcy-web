package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// TestOIDCLoginServiceVerifyIdentityLinkReauthReturnsTheIdentityWithoutLinking
// is the positive anchor for issue #701's Settings step-up, as WEB-401 split
// it: a fresh exchange for an (issuer, subject) linked to nobody returns that
// identity and persists NOTHING. The link is written only by
// ConfirmAndLinkIdentity, from the owner's CSRF-protected confirm.
func TestOIDCLoginServiceVerifyIdentityLinkReauthReturnsTheIdentityWithoutLinking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{} // found=false: nothing linked yet
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "settings-link-sub", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	claims, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if claims.Issuer != "https://id.example.com" || claims.Subject != "settings-link-sub" {
		t.Fatalf("unexpected identity: %+v", claims)
	}
	if identities.createCallSeen {
		t.Fatal("verifying the step-up must not persist a link")
	}
	if identities.lastIssuer != "https://id.example.com" || identities.lastSubject != "settings-link-sub" {
		t.Fatalf("expected the cross-account check on the returned identity, looked up %q/%q", identities.lastIssuer, identities.lastSubject)
	}
	// The exchange carries no email, and the link step-up still never asks UserInfo.
	if client.userInfoCalls != 0 {
		t.Fatalf("UserInfoClaims was called %d times for a link step-up, want 0", client.userInfoCalls)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthAcceptsAnIdentityAlreadyOnThisAccount:
// re-linking the account's own identity is ConfirmAndLinkIdentity's no-op, not
// a refusal.
func TestOIDCLoginServiceVerifyIdentityLinkReauthAcceptsAnIdentityAlreadyOnThisAccount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 5, UserID: 42, Issuer: "https://id.example.com", Subject: "own-sub"},
	}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "own-sub", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if _, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now); err != nil {
		t.Fatalf("expected nil error for the account's own identity, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesStaleExchange pins the
// freshness requirement: an exchange whose auth_time falls outside maxAuthAge
// returns no identity, even though the (issuer, subject) itself would
// otherwise be free to claim.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesStaleExchange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "settings-link-stale", now.Add(-10*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	_, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale, got %v", err)
	}
	// The provider DID date this sign-in, so the verdict must not be the
	// missing-claim one: that copy tells the owner a retry cannot work, which
	// is false here — the sign-in was merely old.
	if errors.Is(err, ErrOIDCReauthAuthTimeMissing) {
		t.Fatalf("a dated but old sign-in must not be reported as a provider that omits auth_time, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesIATOnlyExchange: a
// permanent binding is authorised only by auth_time; a fresh iat on a token
// that omits auth_time is refused as the provider omission it is, not as a
// sign-in that got old, because the identity-link step-up is reachable on any
// provider and its owner needs to know that repeating the flow cannot help.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesIATOnlyExchange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:   "https://id.example.com",
				Subject:  "settings-link-iat-only",
				IssuedAt: now,
			},
		},
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	_, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if !errors.Is(err, ErrOIDCReauthAuthTimeMissing) {
		t.Fatalf("expected ErrOIDCReauthAuthTimeMissing for an iat-only token, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesCrossUserClaim: an
// identity already linked to a DIFFERENT account is refused before the owner
// is asked to confirm a link ConfirmAndLinkIdentity would refuse anyway.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesCrossUserClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found: true,
		identity: models.OIDCIdentity{
			ID:      99,
			UserID:  7, // already linked to a DIFFERENT user than targetUserID below
			Issuer:  "https://id.example.com",
			Subject: "claimed-elsewhere",
		},
	}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "claimed-elsewhere", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	_, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed for a cross-user claim, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthMapsALookupFault: a storage
// fault on the cross-account check fails closed.
func TestOIDCLoginServiceVerifyIdentityLinkReauthMapsALookupFault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "lookup-fault", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{findErr: errors.New("store down")}, &stubOIDCUserStore{}, nil)

	if _, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("expected ErrOIDCIdentityResolveFailed, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesAnExchangeWithoutAnIdentityKey:
// a token missing its subject names nothing that could be linked.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRefusesAnExchangeWithoutAnIdentityKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresEnabledProvider mirrors
// the other OIDCLoginService methods: a disabled provider refuses before
// touching the exchange at all.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresEnabledProvider(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresTargetUserID: a zero
// account id is refused before any exchange.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresTargetUserID(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{enabled: true}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 0, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed for a zero target user id, got %v", err)
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresNonEmptyInputs guards
// the callback-invalid arm: an empty code, verifier, or nonce refuses before
// ever reaching the provider.
func TestOIDCLoginServiceVerifyIdentityLinkReauthRequiresNonEmptyInputs(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{enabled: true}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	cases := []struct {
		name         string
		code         string
		codeVerifier string
		nonce        string
	}{
		{name: "empty code", code: "", codeVerifier: "verifier", nonce: "nonce"},
		{name: "empty verifier", code: "code", codeVerifier: "", nonce: "nonce"},
		{name: "empty nonce", code: "code", codeVerifier: "verifier", nonce: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := service.VerifyIdentityLinkReauth(context.Background(), tc.code, tc.codeVerifier, tc.nonce, 42, 5*time.Minute, time.Now())
			if !errors.Is(err, ErrOIDCCallbackInvalid) {
				t.Fatalf("expected ErrOIDCCallbackInvalid, got %v", err)
			}
		})
	}
}

// TestOIDCLoginServiceVerifyIdentityLinkReauthMapsExchangeFailure covers the
// arm where the provider itself refuses the code exchange.
func TestOIDCLoginServiceVerifyIdentityLinkReauthMapsExchangeFailure(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true, exchangeErr: ErrOIDCUnavailable}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	_, err := service.VerifyIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, time.Now())
	if !errors.Is(err, ErrOIDCAuthenticationFailed) {
		t.Fatalf("expected ErrOIDCAuthenticationFailed, got %v", err)
	}
}
