package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestOIDCLoginServiceCompleteIdentityLinkReauthLinksOnFreshExchange is the
// positive anchor for issue #701's Settings step-up: a fresh exchange for an
// (issuer, subject) that has never been linked to ANYONE persists exactly the
// binding ConfirmAndLinkIdentity (and, before it was closed, the public
// link-confirm route) would have created. Unlike ValidateReauthExchange, this
// must succeed precisely because the identity store has no matching row yet.
func TestOIDCLoginServiceCompleteIdentityLinkReauthLinksOnFreshExchange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{} // found=false: nothing linked yet
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "settings-link-sub", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !identities.createCallSeen {
		t.Fatal("expected the identity to be persisted via Create()")
	}
	if identities.created.UserID != 42 || identities.created.Issuer != "https://id.example.com" || identities.created.Subject != "settings-link-sub" {
		t.Fatalf("unexpected persisted identity: %+v", identities.created)
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthRefusesStaleExchange pins the
// freshness requirement: an exchange whose auth_time/iat falls outside
// maxAuthAge must not link anything, even though the (issuer, subject) itself
// would otherwise be free to claim.
func TestOIDCLoginServiceCompleteIdentityLinkReauthRefusesStaleExchange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "settings-link-stale", now.Add(-10*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("a stale exchange must never persist a link")
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthRefusesCrossUserClaim proves
// this method still fails closed when the (issuer, subject) the exchange
// resolved to is already claimed by a DIFFERENT account — the same guard
// ConfirmAndLinkIdentity applies to the (now closed) public route.
func TestOIDCLoginServiceCompleteIdentityLinkReauthRefusesCrossUserClaim(t *testing.T) {
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

	err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, now)
	if !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed for a cross-user claim, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect Create() when the identity is already claimed by someone else")
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresEnabledProvider mirrors
// the other OIDCLoginService methods: a disabled provider refuses before
// touching the exchange at all.
func TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresEnabledProvider(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresTargetUserID guards
// against ever calling ConfirmAndLinkIdentity with a zero user id.
func TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresTargetUserID(t *testing.T) {
	t.Parallel()

	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(&stubOIDCProviderClient{enabled: true}, identities, &stubOIDCUserStore{}, nil)

	if err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 0, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCLinkFailed) {
		t.Fatalf("expected ErrOIDCLinkFailed for a zero target user id, got %v", err)
	}
	if identities.createCallSeen {
		t.Fatal("did not expect an exchange or Create() with no target user")
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresNonEmptyInputs guards
// the callback-invalid arm: an empty code, verifier, or nonce refuses before
// ever reaching the provider.
func TestOIDCLoginServiceCompleteIdentityLinkReauthRequiresNonEmptyInputs(t *testing.T) {
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
			err := service.CompleteIdentityLinkReauth(context.Background(), tc.code, tc.codeVerifier, tc.nonce, 42, 5*time.Minute, time.Now())
			if !errors.Is(err, ErrOIDCCallbackInvalid) {
				t.Fatalf("expected ErrOIDCCallbackInvalid, got %v", err)
			}
		})
	}
}

// TestOIDCLoginServiceCompleteIdentityLinkReauthMapsExchangeFailure covers the
// arm where the provider itself refuses the code exchange.
func TestOIDCLoginServiceCompleteIdentityLinkReauthMapsExchangeFailure(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true, exchangeErr: ErrOIDCUnavailable}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	err := service.CompleteIdentityLinkReauth(context.Background(), "code", "verifier", "nonce", 42, 5*time.Minute, time.Now())
	if !errors.Is(err, ErrOIDCAuthenticationFailed) {
		t.Fatalf("expected ErrOIDCAuthenticationFailed, got %v", err)
	}
}
