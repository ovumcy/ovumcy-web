package security

// oidc_mutation_coverage_test.go — closes gremlins gaps in oidc.go that pure
// (non-network) helpers left uncovered or under-asserted: the required-field
// switch, the nil-client accessor guards, and the "@ position" guard in
// AllowsAutoProvision. The provider-facing paths (loadProvider, ExchangeCode,
// AuthCodeURL) are exercised by the e2e OIDC lanes, not Go units.

import (
	"reflect"
	"testing"
)

// TestValidateRequiredFields covers the required-field switch (oidc.go:144–150),
// which no test reached.
func TestValidateRequiredFields(t *testing.T) {
	t.Parallel()

	complete := OIDCConfig{
		IssuerURL:    "https://id.example.com",
		ClientID:     "ovumcy",
		ClientSecret: "secret",
		RedirectURL:  "https://ovumcy.example.com/auth/oidc/callback",
	}
	if err := complete.validateRequiredFields(); err != nil {
		t.Fatalf("complete config should pass required-field validation, got %v", err)
	}

	missing := []struct {
		name   string
		mutate func(*OIDCConfig)
	}{
		{name: "issuer", mutate: func(c *OIDCConfig) { c.IssuerURL = "" }},
		{name: "client id", mutate: func(c *OIDCConfig) { c.ClientID = "" }},
		{name: "client secret", mutate: func(c *OIDCConfig) { c.ClientSecret = "" }},
		{name: "redirect url", mutate: func(c *OIDCConfig) { c.RedirectURL = "" }},
	}
	for _, tc := range missing {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := complete
			tc.mutate(&config)
			if err := config.validateRequiredFields(); err == nil {
				t.Fatalf("missing %s should fail required-field validation", tc.name)
			}
		})
	}
}

// TestOIDCClientNilReceiverGetters covers the nil-client guards on the client
// accessors (oidc.go:218/273/280). A nil *OIDCClient is the "OIDC disabled"
// sentinel and must answer safely without dereferencing.
func TestOIDCClientNilReceiverGetters(t *testing.T) {
	t.Parallel()

	var nilClient *OIDCClient
	if nilClient.Enabled() {
		t.Fatal("nil client must report Enabled() == false")
	}
	if !nilClient.LocalPublicAuthEnabled() {
		t.Fatal("nil client must report LocalPublicAuthEnabled() == true")
	}
	if got := nilClient.Config(); !reflect.DeepEqual(got, OIDCConfig{}) {
		t.Fatalf("nil client must return a zero OIDCConfig, got %#v", got)
	}

	disabled := &OIDCClient{config: OIDCConfig{Enabled: false}}
	if disabled.Enabled() {
		t.Fatal("non-nil client with Enabled=false must report Enabled() == false")
	}

	enabled := &OIDCClient{config: OIDCConfig{Enabled: true, LoginMode: OIDCLoginModeOIDCOnly}}
	if !enabled.Enabled() {
		t.Fatal("enabled client must report Enabled() == true")
	}
	if enabled.LocalPublicAuthEnabled() {
		t.Fatal("oidc_only client must report LocalPublicAuthEnabled() == false")
	}
	if got := enabled.Config(); !reflect.DeepEqual(got, enabled.config) {
		t.Fatalf("client Config() must return its own config, got %#v", got)
	}
}

// TestAllowsAutoProvisionGatesOnDomainNotLocalPart pins that auto-provision is
// decided solely by the email domain. It covers the "@ position" guard on
// oidc.go:260, including the atIndex==0 boundary (an `@`-led address) that the
// existing cases did not reach — killing the `atIndex < 0` boundary mutant.
func TestAllowsAutoProvisionGatesOnDomainNotLocalPart(t *testing.T) {
	t.Parallel()

	config := OIDCConfig{
		AutoProvision:               true,
		AutoProvisionAllowedDomains: []string{"example.com"},
	}

	cases := []struct {
		name  string
		email string
		want  bool
	}{
		{name: "matching domain", email: "owner@example.com", want: true},
		{name: "non-matching domain", email: "owner@other.com", want: false},
		{name: "no at sign", email: "owner.example.com", want: false},
		{name: "trailing at sign", email: "owner@", want: false},
		{name: "empty local part still gates on domain", email: "@example.com", want: true},
		{name: "case is normalized before matching", email: "Owner@Example.COM", want: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := config.AllowsAutoProvision(tc.email); got != tc.want {
				t.Fatalf("AllowsAutoProvision(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}
