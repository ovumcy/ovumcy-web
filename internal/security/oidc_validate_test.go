package security

import "testing"

func validOIDCConfigForTest() OIDCConfig {
	return OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "ovumcy",
		ClientSecret: "secret",
		RedirectURL:  "https://ovumcy.example.com/auth/oidc/callback",
		LoginMode:    OIDCLoginModeHybrid,
		LogoutMode:   OIDCLogoutModeLocal,
	}
}

// TestOIDCConfigValidateBranches exercises the security-relevant rejection and
// acceptance branches of OIDCConfig.Validate and its sub-validators (runtime
// modes, required fields, https/path shape of issuer/redirect, same-origin
// post-logout pinning, provisioning-domain allowlist).
func TestOIDCConfigValidateBranches(t *testing.T) {
	cases := []struct {
		name             string
		mutate           func(*OIDCConfig)
		cookieSecure     bool
		registrationOpen bool
		wantErr          bool
	}{
		{name: "valid baseline", cookieSecure: true, registrationOpen: true},
		{name: "disabled ignores other fields", mutate: func(c *OIDCConfig) { *c = OIDCConfig{Enabled: false, IssuerURL: "not-a-url"} }, cookieSecure: false, registrationOpen: false},
		{name: "requires cookie secure", cookieSecure: false, registrationOpen: true, wantErr: true},
		{name: "rejects unknown login mode", mutate: func(c *OIDCConfig) { c.LoginMode = "bogus" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "rejects unknown logout mode", mutate: func(c *OIDCConfig) { c.LogoutMode = "bogus" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "auto-provision requires open registration", mutate: func(c *OIDCConfig) { c.AutoProvision = true }, cookieSecure: true, registrationOpen: false, wantErr: true},
		{name: "missing issuer", mutate: func(c *OIDCConfig) { c.IssuerURL = "" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "missing client id", mutate: func(c *OIDCConfig) { c.ClientID = "" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "missing client secret", mutate: func(c *OIDCConfig) { c.ClientSecret = "" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "missing redirect", mutate: func(c *OIDCConfig) { c.RedirectURL = "" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "issuer not https", mutate: func(c *OIDCConfig) { c.IssuerURL = "http://id.example.com" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "issuer not absolute", mutate: func(c *OIDCConfig) { c.IssuerURL = "id.example.com" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "issuer carries query", mutate: func(c *OIDCConfig) { c.IssuerURL = "https://id.example.com?probe=1" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "redirect wrong path", mutate: func(c *OIDCConfig) { c.RedirectURL = "https://ovumcy.example.com/not-the-callback" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "redirect not https", mutate: func(c *OIDCConfig) { c.RedirectURL = "http://ovumcy.example.com/auth/oidc/callback" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "post-logout cross origin", mutate: func(c *OIDCConfig) { c.PostLogoutRedirectURL = "https://attacker.example.net/login" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "post-logout not https", mutate: func(c *OIDCConfig) { c.PostLogoutRedirectURL = "http://ovumcy.example.com/login" }, cookieSecure: true, registrationOpen: true, wantErr: true},
		{name: "post-logout same origin ok", mutate: func(c *OIDCConfig) { c.PostLogoutRedirectURL = "https://ovumcy.example.com/logout-complete" }, cookieSecure: true, registrationOpen: true},
		{name: "oidc-only login and provider logout ok", mutate: func(c *OIDCConfig) {
			c.LoginMode = OIDCLoginModeOIDCOnly
			c.LogoutMode = OIDCLogoutModeProvider
		}, cookieSecure: true, registrationOpen: true},
		{name: "auto-provision with valid domain ok", mutate: func(c *OIDCConfig) {
			c.AutoProvision = true
			c.AutoProvisionAllowedDomains = []string{"example.com"}
		}, cookieSecure: true, registrationOpen: true},
		{name: "auto-provision with invalid domain", mutate: func(c *OIDCConfig) {
			c.AutoProvision = true
			c.AutoProvisionAllowedDomains = []string{"missing-dot"}
		}, cookieSecure: true, registrationOpen: true, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			config := validOIDCConfigForTest()
			if tc.mutate != nil {
				tc.mutate(&config)
			}
			err := config.Validate(tc.cookieSecure, tc.registrationOpen)
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}

func TestIsValidProvisioningDomain(t *testing.T) {
	cases := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"staff.example.com", true},
		{"missing-dot", false},
		{"", false},
		{"has space.com", false},
		{".leading.com", false},
		{"trailing.com.", false},
	}
	for _, tc := range cases {
		if got := isValidProvisioningDomain(tc.domain); got != tc.want {
			t.Fatalf("isValidProvisioningDomain(%q) = %t, want %t", tc.domain, got, tc.want)
		}
	}
}
