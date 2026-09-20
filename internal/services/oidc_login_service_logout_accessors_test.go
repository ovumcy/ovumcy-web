package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/security"
)

// IssuerURL and PostLogoutRedirectURL are what the transport layer composes a
// provider end-session redirect from: the stored end-session endpoint is pinned
// to the first, and the return address is the second, never the one stored with
// the logout state. A nil service (OIDC disabled) reports neither, which admits
// no stored endpoint and composes no provider redirect.
func TestOIDCLoginServiceReportsTheConfiguredLogoutInputs(t *testing.T) {
	t.Parallel()

	var disabled *OIDCLoginService
	if got := disabled.IssuerURL(); got != "" {
		t.Fatalf("nil service IssuerURL() = %q, want empty", got)
	}
	if got := disabled.PostLogoutRedirectURL(); got != "" {
		t.Fatalf("nil service PostLogoutRedirectURL() = %q, want empty", got)
	}

	for name, tc := range map[string]struct {
		config security.OIDCConfig
		want   string
	}{
		"explicit post-logout address": {
			config: security.OIDCConfig{
				IssuerURL:             "https://id.example.com",
				RedirectURL:           "https://ovumcy.example.com/auth/oidc/callback",
				PostLogoutRedirectURL: "https://ovumcy.example.com/signed-out",
			},
			want: "https://ovumcy.example.com/signed-out",
		},
		"derived from the redirect origin": {
			config: security.OIDCConfig{
				IssuerURL:   "https://id.example.com",
				RedirectURL: "https://ovumcy.example.com/auth/oidc/callback",
			},
			want: "https://ovumcy.example.com/login",
		},
	} {
		service := NewOIDCLoginService(&stubOIDCProviderClient{config: tc.config}, &stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil)
		if got := service.IssuerURL(); got != "https://id.example.com" {
			t.Fatalf("%s: IssuerURL() = %q", name, got)
		}
		if got := service.PostLogoutRedirectURL(); got != tc.want {
			t.Fatalf("%s: PostLogoutRedirectURL() = %q, want %q", name, got, tc.want)
		}
	}
}

// ProviderLogoutEnabled is the one predicate both ends of the provider-logout
// bridge ask: the callback asks it before storing a row, and the transport
// layer asks it again at sign-out before composing an end-session redirect
// from one. It follows the configuration in force and nothing else — a row
// outlives a mode switch by up to its TTL, so the answer after the switch is
// what keeps a turned-off mode turned off.
func TestOIDCLoginServiceProviderLogoutEnabledFollowsTheConfigurationInForce(t *testing.T) {
	t.Parallel()

	var disabled *OIDCLoginService
	if disabled.ProviderLogoutEnabled() {
		t.Fatal("nil service reported provider logout enabled")
	}

	for name, tc := range map[string]struct {
		enabled bool
		mode    security.OIDCLogoutMode
		want    bool
	}{
		"provider mode":           {enabled: true, mode: security.OIDCLogoutModeProvider, want: true},
		"auto mode":               {enabled: true, mode: security.OIDCLogoutModeAuto, want: true},
		"local mode":              {enabled: true, mode: security.OIDCLogoutModeLocal, want: false},
		"unset mode":              {enabled: true, mode: "", want: false},
		"provider mode, oidc off": {enabled: false, mode: security.OIDCLogoutModeProvider, want: false},
		"auto mode, oidc off":     {enabled: false, mode: security.OIDCLogoutModeAuto, want: false},
	} {
		service := NewOIDCLoginService(
			&stubOIDCProviderClient{enabled: tc.enabled, config: security.OIDCConfig{
				Enabled:     tc.enabled,
				LogoutMode:  tc.mode,
				IssuerURL:   "https://id.example.com",
				RedirectURL: "https://ovumcy.example.com/auth/oidc/callback",
			}},
			&stubOIDCIdentityStore{}, &stubOIDCUserStore{}, nil,
		)
		if got := service.ProviderLogoutEnabled(); got != tc.want {
			t.Fatalf("%s: ProviderLogoutEnabled() = %t, want %t", name, got, tc.want)
		}
		// The write side reads the same predicate, so a mode that composes no
		// redirect stores no row either.
		state := service.buildLogoutState(security.OIDCSession{
			EndSessionEndpoint: "https://id.example.com/logout",
			IDTokenHint:        "id-token",
		}, 7)
		if (state != nil) != tc.want {
			t.Fatalf("%s: buildLogoutState produced %#v, want stored=%t", name, state, tc.want)
		}
	}
}
