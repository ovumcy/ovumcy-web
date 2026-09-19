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
