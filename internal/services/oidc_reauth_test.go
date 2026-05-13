package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func TestOIDCLoginServiceStartReauthPassesPromptLoginAndMaxAge(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{
		enabled: true,
		authURL: "https://id.example.com/authorize?prompt=login",
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	url, err := service.StartReauth(context.Background(), "state", "nonce", "verifier")
	if err != nil {
		t.Fatalf("StartReauth() unexpected error: %v", err)
	}
	if url != client.authURL {
		t.Fatalf("expected auth URL %q, got %q", client.authURL, url)
	}
	if client.lastAuthExtra["prompt"] != "login" {
		t.Fatalf("expected extra[prompt]=login, got %q", client.lastAuthExtra["prompt"])
	}
	if client.lastAuthExtra["max_age"] != "0" {
		t.Fatalf("expected extra[max_age]=0, got %q", client.lastAuthExtra["max_age"])
	}
}

func TestOIDCLoginServiceStartReauthDisabled(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.StartReauth(context.Background(), "state", "nonce", "verifier"); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

func TestOIDCLoginServiceStartReauthEmptyInputs(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.StartReauth(context.Background(), "", "nonce", "verifier"); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty state, got %v", err)
	}
	if _, err := service.StartReauth(context.Background(), "state", "", "verifier"); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty nonce, got %v", err)
	}
	if _, err := service.StartReauth(context.Background(), "state", "nonce", ""); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty verifier, got %v", err)
	}
}

func TestOIDCLoginServiceStartReauthClientError(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true, authErr: errors.New("provider down")}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if _, err := service.StartReauth(context.Background(), "state", "nonce", "verifier"); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("expected ErrOIDCUnavailable, got %v", err)
	}
}

func makeReauthExchange(issuer, subject string, authTime time.Time) security.OIDCExchangeResult {
	return security.OIDCExchangeResult{
		Claims: security.OIDCClaims{
			Issuer:   issuer,
			Subject:  subject,
			AuthTime: authTime,
		},
	}
}

func TestOIDCLoginServiceValidateReauthExchangeHappyFreshAuthTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 10, UserID: 5, Issuer: "https://id.example.com", Subject: "sub-1"},
	}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-1", now.Add(-2*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, now); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if identities.touchedID != 10 {
		t.Fatalf("expected TouchLastUsed for identity 10, got %d", identities.touchedID)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeHappyIATFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 11, UserID: 6, Issuer: "https://id.example.com", Subject: "sub-2"},
	}
	client := &stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:   "https://id.example.com",
				Subject:  "sub-2",
				IssuedAt: now.Add(-1 * time.Minute),
			},
		},
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 6, 5*time.Minute, now); err != nil {
		t.Fatalf("expected nil error with iat fallback, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeStaleAuthTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 12, UserID: 7, Issuer: "https://id.example.com", Subject: "sub-3"},
	}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-3", now.Add(-10*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 7, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeStaleIATFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 13, UserID: 8, Issuer: "https://id.example.com", Subject: "sub-4"},
	}
	client := &stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:   "https://id.example.com",
				Subject:  "sub-4",
				IssuedAt: now.Add(-10 * time.Minute),
			},
		},
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 8, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale on stale iat fallback, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeNoClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 14, UserID: 9, Issuer: "https://id.example.com", Subject: "sub-5"},
	}
	client := &stubOIDCProviderClient{
		enabled: true,
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{Issuer: "https://id.example.com", Subject: "sub-5"},
		},
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 9, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale when both auth_time and iat are zero, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeFutureDatedAuthTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 15, UserID: 10, Issuer: "https://id.example.com", Subject: "sub-6"},
	}
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-6", now.Add(5*time.Minute)),
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 10, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthStale) {
		t.Fatalf("expected ErrOIDCReauthStale for future-dated auth_time, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeIdentityNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-1", now.Add(-1*time.Minute)),
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{found: false}, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthIdentityMismatch) {
		t.Fatalf("expected ErrOIDCReauthIdentityMismatch when identity not found, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeUserIDMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-1", now.Add(-1*time.Minute)),
	}
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 20, UserID: 99, Issuer: "https://id.example.com", Subject: "sub-1"},
	}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCReauthIdentityMismatch) {
		t.Fatalf("expected ErrOIDCReauthIdentityMismatch on user ID mismatch, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeIdentityStoreError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: makeReauthExchange("https://id.example.com", "sub-1", now.Add(-1*time.Minute)),
	}
	identities := &stubOIDCIdentityStore{findErr: errors.New("db error")}
	service := NewOIDCLoginService(client, identities, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCIdentityResolveFailed) {
		t.Fatalf("expected ErrOIDCIdentityResolveFailed on store error, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeExchangeError(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true, exchangeErr: errors.New("token endpoint error")}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCAuthenticationFailed) {
		t.Fatalf("expected ErrOIDCAuthenticationFailed on exchange error, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeDisabled(t *testing.T) {
	t.Parallel()

	service := NewOIDCLoginService(&stubOIDCProviderClient{}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 5, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCDisabled) {
		t.Fatalf("expected ErrOIDCDisabled, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeZeroUserID(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)

	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "nonce", 0, 5*time.Minute, time.Now()); !errors.Is(err, ErrOIDCReauthIdentityMismatch) {
		t.Fatalf("expected ErrOIDCReauthIdentityMismatch for userID=0, got %v", err)
	}
}

func TestOIDCLoginServiceValidateReauthExchangeEmptyInputs(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{enabled: true}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)
	now := time.Now()

	if err := service.ValidateReauthExchange(context.Background(), "", "verifier", "nonce", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty code, got %v", err)
	}
	if err := service.ValidateReauthExchange(context.Background(), "code", "", "nonce", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty verifier, got %v", err)
	}
	if err := service.ValidateReauthExchange(context.Background(), "code", "verifier", "", 5, 5*time.Minute, now); !errors.Is(err, ErrOIDCCallbackInvalid) {
		t.Fatalf("expected ErrOIDCCallbackInvalid on empty nonce, got %v", err)
	}
}
