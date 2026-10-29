package services

// oidc_login_service_userinfo_test.go — an unlinked sign-in resolves its
// account by the email claims of the provider's UserInfo endpoint, where OpenID
// Connect delivers them to a code-flow client. The ID token's email claims are
// used only when the provider has no usable endpoint, and a linked identity
// never asks.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

const userInfoTestIssuer = "https://id.example.com"

func userInfoTestExchange(subject string, email string) security.OIDCExchangeResult {
	return security.OIDCExchangeResult{Claims: security.OIDCClaims{
		Issuer:        userInfoTestIssuer,
		Subject:       subject,
		Email:         email,
		EmailVerified: email != "",
	}}
}

func userInfoTestClaims(subject string, email string, verified bool) *security.OIDCClaims {
	return &security.OIDCClaims{Issuer: userInfoTestIssuer, Subject: subject, Email: email, EmailVerified: verified}
}

// TestOIDCLoginServiceAuthenticateReadsUserInfoWhenTheIDTokenHasNoEmail is the
// regression for a conforming provider (Authelia 4.39 by default): the ID token
// carries only the identity, UserInfo carries the verified address, and the
// sign-in matches the existing account by that address — refusing to link it
// without confirmation, exactly as an ID-token address would.
func TestOIDCLoginServiceAuthenticateReadsUserInfoWhenTheIDTokenHasNoEmail(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: userInfoTestExchange("userinfo-sub", ""),
		userInfo: userInfoTestClaims("userinfo-sub", " Owner@Example.com ", true),
	}
	users := &stubOIDCUserStore{
		byEmailFound: true,
		byEmail:      models.User{ID: 9, Email: "owner@example.com", Role: models.RoleOwner, OnboardingCompleted: true},
	}
	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(client, identities, users, nil)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if !errors.Is(err, ErrOIDCLinkRequiresConfirmation) {
		t.Fatalf("Authenticate() error = %v, want ErrOIDCLinkRequiresConfirmation for the account the UserInfo address names", err)
	}
	if client.userInfoCalls != 1 {
		t.Fatalf("UserInfoClaims was called %d times, want 1", client.userInfoCalls)
	}
	if client.userInfoExchange.Claims.Subject != "userinfo-sub" {
		t.Fatalf("UserInfoClaims was handed subject %q, want the exchange's", client.userInfoExchange.Claims.Subject)
	}
	if users.lastLookupEmail != "owner@example.com" {
		t.Fatalf("account looked up by %q, want the normalized UserInfo address", users.lastLookupEmail)
	}
	if result.PendingLinkClaims == nil || result.PendingLinkClaims.Email != " Owner@Example.com " || result.PendingLinkClaims.Subject != "userinfo-sub" {
		t.Fatalf("PendingLinkClaims = %+v, want the UserInfo claims", result.PendingLinkClaims)
	}
	if identities.createCallSeen {
		t.Fatal("an email from UserInfo must never link an existing account on its own")
	}
}

// TestOIDCLoginServiceAuthenticateAutoProvisionsFromTheUserInfoEmail: with no
// account holding the UserInfo address, auto-provisioning creates one for that
// address and links the (issuer, subject) the ID token asserted.
func TestOIDCLoginServiceAuthenticateAutoProvisionsFromTheUserInfoEmail(t *testing.T) {
	t.Parallel()

	provisioner := &stubOIDCAutoProvisioner{user: models.User{ID: 17, Email: "owner@example.com", Role: models.RoleOwner}}
	identities := &stubOIDCIdentityStore{}
	service := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled:  true,
		config:   security.OIDCConfig{Enabled: true, AutoProvision: true, AutoProvisionAllowedDomains: []string{"example.com"}},
		exchange: userInfoTestExchange("provision-sub", ""),
		userInfo: userInfoTestClaims("provision-sub", "owner@example.com", true),
	}, identities, &stubOIDCUserStore{}, provisioner)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if !result.AutoProvisioned || provisioner.email != "owner@example.com" {
		t.Fatalf("expected an account provisioned for the UserInfo address, got provisioned=%v email=%q", result.AutoProvisioned, provisioner.email)
	}
	if identities.created.UserID != 17 || identities.created.Issuer != userInfoTestIssuer || identities.created.Subject != "provision-sub" {
		t.Fatalf("unexpected identity link: %+v", identities.created)
	}
}

// TestOIDCLoginServiceAuthenticatePrefersUserInfoOverTheIDTokenEmail: when the
// provider has a usable UserInfo endpoint, its address is the one the account is
// resolved by, even when the ID token carries another.
func TestOIDCLoginServiceAuthenticatePrefersUserInfoOverTheIDTokenEmail(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: userInfoTestExchange("id-token-sub", "id-token@example.com"),
		userInfo: userInfoTestClaims("id-token-sub", "owner@example.com", true),
	}
	users := &stubOIDCUserStore{
		byEmailFound: true,
		byEmail:      models.User{ID: 9, Email: "owner@example.com", Role: models.RoleOwner, OnboardingCompleted: true},
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, users, nil)

	if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, ErrOIDCLinkRequiresConfirmation) {
		t.Fatalf("Authenticate() error = %v, want ErrOIDCLinkRequiresConfirmation", err)
	}
	if client.userInfoCalls != 1 {
		t.Fatalf("UserInfoClaims was called %d times, want 1", client.userInfoCalls)
	}
	if users.lastLookupEmail != "owner@example.com" {
		t.Fatalf("account looked up by %q, want the UserInfo address", users.lastLookupEmail)
	}
}

// TestOIDCLoginServiceAuthenticateTakesVerificationFromUserInfoAlone: the
// address and its verification flag both come from UserInfo, so an ID token's
// flag neither rescues an unverified UserInfo address nor blocks a verified one.
func TestOIDCLoginServiceAuthenticateTakesVerificationFromUserInfoAlone(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		idTokenVerified  bool
		userInfoVerified bool
		want             error
	}{
		"unverified in the ID token, verified in UserInfo": {idTokenVerified: false, userInfoVerified: true, want: ErrOIDCLinkRequiresConfirmation},
		"verified in the ID token, unverified in UserInfo": {idTokenVerified: true, userInfoVerified: false, want: ErrOIDCAccountUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			exchange := userInfoTestExchange("verified-sub", "owner@example.com")
			exchange.Claims.EmailVerified = testCase.idTokenVerified
			users := &stubOIDCUserStore{
				byEmailFound: true,
				byEmail:      models.User{ID: 9, Email: "owner@example.com", Role: models.RoleOwner, OnboardingCompleted: true},
			}
			service := NewOIDCLoginService(&stubOIDCProviderClient{
				enabled:  true,
				exchange: exchange,
				userInfo: userInfoTestClaims("verified-sub", "owner@example.com", testCase.userInfoVerified),
			}, &stubOIDCIdentityStore{}, users, nil)

			if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, testCase.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

// TestOIDCLoginServiceAuthenticateFallsBackToTheIDTokenWithoutUserInfo: a
// provider with no usable UserInfo endpoint signs in on the ID token's email
// claims, as before.
func TestOIDCLoginServiceAuthenticateFallsBackToTheIDTokenWithoutUserInfo(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{
		enabled:  true,
		exchange: userInfoTestExchange("id-token-sub", "owner@example.com"),
	}
	users := &stubOIDCUserStore{
		byEmailFound: true,
		byEmail:      models.User{ID: 9, Email: "owner@example.com", Role: models.RoleOwner, OnboardingCompleted: true},
	}
	service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, users, nil)

	if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, ErrOIDCLinkRequiresConfirmation) {
		t.Fatalf("Authenticate() error = %v, want ErrOIDCLinkRequiresConfirmation", err)
	}
	if client.userInfoCalls != 1 {
		t.Fatalf("UserInfoClaims was called %d times, want 1", client.userInfoCalls)
	}
	if users.lastLookupEmail != "owner@example.com" {
		t.Fatalf("account looked up by %q, want the ID token's address", users.lastLookupEmail)
	}
}

// TestOIDCLoginServiceAuthenticateLinkedIdentityNeverAsksUserInfo: a linked
// (issuer, subject) signs in on the link alone, so a provider whose ID tokens
// carry no email keeps signing linked accounts in whatever UserInfo would say.
func TestOIDCLoginServiceAuthenticateLinkedIdentityNeverAsksUserInfo(t *testing.T) {
	t.Parallel()

	client := &stubOIDCProviderClient{
		enabled:     true,
		exchange:    userInfoTestExchange("linked-sub", ""),
		userInfoErr: errors.New("userinfo must not be asked"),
	}
	identities := &stubOIDCIdentityStore{
		found:    true,
		identity: models.OIDCIdentity{ID: 44, UserID: 7, Issuer: userInfoTestIssuer, Subject: "linked-sub"},
	}
	users := &stubOIDCUserStore{byID: models.User{ID: 7, Role: models.RoleOwner, OnboardingCompleted: true}}
	service := NewOIDCLoginService(client, identities, users, nil)

	result, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now())
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.User.ID != 7 {
		t.Fatalf("expected the linked account 7, got %d", result.User.ID)
	}
	if client.userInfoCalls != 0 {
		t.Fatalf("UserInfoClaims was called %d times for a linked identity, want 0", client.userInfoCalls)
	}
}

// TestOIDCLoginServiceAuthenticateWithoutAnyEmailSource: when the provider has
// no usable UserInfo endpoint, or UserInfo carries no verified address, the
// sign-in is refused as an unavailable account — the outcome before UserInfo
// was consulted — and nothing is provisioned.
func TestOIDCLoginServiceAuthenticateWithoutAnyEmailSource(t *testing.T) {
	t.Parallel()

	for name, userInfo := range map[string]*security.OIDCClaims{
		"no usable endpoint":    nil,
		"an unverified address": userInfoTestClaims("no-email-sub", "owner@example.com", false),
		"no address":            userInfoTestClaims("no-email-sub", "", true),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provisioner := &stubOIDCAutoProvisioner{user: models.User{ID: 17, Email: "owner@example.com", Role: models.RoleOwner}}
			client := &stubOIDCProviderClient{
				enabled:  true,
				config:   security.OIDCConfig{Enabled: true, AutoProvision: true},
				exchange: userInfoTestExchange("no-email-sub", ""),
				userInfo: userInfo,
			}
			service := NewOIDCLoginService(client, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, provisioner)

			if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, ErrOIDCAccountUnavailable) {
				t.Fatalf("Authenticate() error = %v, want ErrOIDCAccountUnavailable", err)
			}
			if client.userInfoCalls != 1 {
				t.Fatalf("UserInfoClaims was called %d times, want 1", client.userInfoCalls)
			}
			if provisioner.called {
				t.Fatal("no account may be provisioned without a verified address")
			}
		})
	}
}

// TestOIDCLoginServiceAuthenticateFailsOnAFailedUserInfoRequest: a UserInfo
// request that fails, or whose answer is refused, fails the sign-in as an
// authentication failure rather than reading as an account without an email,
// and never falls back to an email the ID token carries.
func TestOIDCLoginServiceAuthenticateFailsOnAFailedUserInfoRequest(t *testing.T) {
	t.Parallel()

	for name, idTokenEmail := range map[string]string{
		"ID token without an email": "",
		"ID token with an email":    "owner@example.com",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provisioner := &stubOIDCAutoProvisioner{user: models.User{ID: 17, Email: "owner@example.com", Role: models.RoleOwner}}
			service := NewOIDCLoginService(&stubOIDCProviderClient{
				enabled:     true,
				config:      security.OIDCConfig{Enabled: true, AutoProvision: true},
				exchange:    userInfoTestExchange("failed-sub", idTokenEmail),
				userInfoErr: errors.New("oidc userinfo sub does not match the id_token sub"),
			}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, provisioner)

			if _, err := service.Authenticate(context.Background(), "code", "verifier", "nonce", time.Now()); !errors.Is(err, ErrOIDCAuthenticationFailed) {
				t.Fatalf("Authenticate() error = %v, want ErrOIDCAuthenticationFailed", err)
			}
			if provisioner.called {
				t.Fatal("a failed UserInfo request must not provision an account")
			}
		})
	}
}
