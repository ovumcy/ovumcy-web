package security

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func TestNewOIDCClientConfiguresBoundedHTTPClient(t *testing.T) {
	client := NewOIDCClient(OIDCConfig{Enabled: true})
	if client == nil {
		t.Fatal("expected OIDC client")
	}
	if client.httpClient == nil {
		t.Fatal("expected bounded OIDC http client")
	}
	if client.httpClient.Timeout != defaultOIDCHTTPTimeout {
		t.Fatalf("expected OIDC http timeout %s, got %s", defaultOIDCHTTPTimeout, client.httpClient.Timeout)
	}
}

func TestOIDCClientContextInjectsConfiguredHTTPClient(t *testing.T) {
	client := NewOIDCClient(OIDCConfig{Enabled: true})

	ctx := client.clientContext(context.Background())
	httpClient, ok := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if !ok {
		t.Fatal("expected oauth2 http client in context")
	}
	if httpClient != client.httpClient {
		t.Fatal("expected OIDC context to reuse configured http client")
	}

	type contextKey string
	parentKey := contextKey("parent")
	parent := context.WithValue(context.Background(), parentKey, "value")
	ctx = client.clientContext(parent)
	if got := ctx.Value(parentKey); got != "value" {
		t.Fatalf("expected parent context values to be preserved, got %#v", got)
	}
}

func TestOIDCConfigAllowsAutoProvisionHonorsDomainAllowlist(t *testing.T) {
	config := OIDCConfig{
		Enabled:                     true,
		AutoProvision:               true,
		AutoProvisionAllowedDomains: []string{"example.com", "staff.example.com"},
	}

	if !config.AllowsAutoProvision("Owner@Example.com") {
		t.Fatal("expected normalized allowlisted email domain to pass auto-provision check")
	}
	if config.AllowsAutoProvision("owner@other.example.com") {
		t.Fatal("did not expect non-allowlisted email domain to pass auto-provision check")
	}
}

func TestOIDCConfigResolvedPostLogoutRedirectURL(t *testing.T) {
	config := OIDCConfig{
		Enabled:     true,
		RedirectURL: "https://ovumcy.example.com/auth/oidc/callback",
	}
	if got := config.ResolvedPostLogoutRedirectURL(); got != "https://ovumcy.example.com/login" {
		t.Fatalf("expected default post-logout redirect to /login, got %q", got)
	}

	config.PostLogoutRedirectURL = "https://ovumcy.example.com/logout-complete"
	if got := config.ResolvedPostLogoutRedirectURL(); got != "https://ovumcy.example.com/logout-complete" {
		t.Fatalf("expected explicit post-logout redirect URL, got %q", got)
	}
}

func TestOIDCConfigValidateRejectsInvalidCAFile(t *testing.T) {
	config := OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "ovumcy",
		ClientSecret: "secret",
		RedirectURL:  "https://ovumcy.example.com/auth/oidc/callback",
		CAFile:       filepath.Join(t.TempDir(), "missing-ca.pem"),
	}

	if err := config.Validate(true, true); err == nil {
		t.Fatal("expected invalid OIDC CA file to fail validation")
	}
}

func TestOIDCConfigValidateRejectsDirectoryCAFile(t *testing.T) {
	config := OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "ovumcy",
		ClientSecret: "secret",
		RedirectURL:  "https://ovumcy.example.com/auth/oidc/callback",
		CAFile:       t.TempDir(),
	}

	if err := config.Validate(true, true); err == nil {
		t.Fatal("expected directory OIDC CA path to fail validation")
	}
}

func TestReadOIDCCABundleRejectsDirectory(t *testing.T) {
	if _, err := readOIDCCABundle(t.TempDir()); err == nil {
		t.Fatal("expected directory OIDC CA path to fail")
	}
}

func TestOIDCConfigValidateAcceptsValidCAFile(t *testing.T) {
	const certPEM = `-----BEGIN CERTIFICATE-----
MIIC9DCCAdygAwIBAgIBATANBgkqhkiG9w0BAQsFADAUMRIwEAYDVQQDEwkxMjcu
MC4wLjEwHhcNMjYwMzI4MTczMDIzWhcNMzYwMzI4MTgzMDIzWjAUMRIwEAYDVQQD
EwkxMjcuMC4wLjEwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCfTEzc
4zujGs9PmM6PHzFQIphEOjPAQmcaoUmCBRWjSWiVA1hIzbe0AM2wKGVE7pz8xDeY
qGAgrXP0aUF98U+gFcNLihw8xMVkAW6R+FkV+PXAuMW7ZQAmrvq6fOkHfMWEA3/3
4pA73uNPQDtWPOIjTz77jNRNOvymCvaUhy/bt3PqvnEzWNa9PdVSOTcLTaydGkx+
9eq8b/Do/Tlca8pncZ7Luy+SEQAQlTPVMe4h8WWKSlyW1YVloZm5XX5Wvj4xzMmh
oHwDwLU+wojt9hl2I6nEF8LJi3YMfYcuaUXrxC9DxToI13gzWXJqAnH40fJC7QC2
wMZPD73wTU/nb36TAgMBAAGjUTBPMA4GA1UdDwEB/wQEAwIFoDATBgNVHSUEDDAK
BggrBgEFBQcDATAMBgNVHRMBAf8EAjAAMBoGA1UdEQQTMBGCCWxvY2FsaG9zdIcE
fwAAATANBgkqhkiG9w0BAQsFAAOCAQEANg0b76OBe8BtSG4JcCeQhT2IiIeIWmVs
KLD4o/u7EzQ5d9PRodCFkkBVkP2B6fg7z1GAl/H1tKKBFidovJAbXQ/yJHqhT7IC
QrCubmlgRkIl9YUJvaOsW0rPBLlWqz2emJH0xftH3QNPWHBVnP3R3BjrIqUG/1xU
ADS7/yMYzyqEmi2+/nnyVMcDvPQfA9K+D32fHHteAsX8HhF2W4YAg1TlsUjpIsYc
lILxHyF4qIl18bap1H7cTSH4ABA2fmMkIl7uqGapSzeJaMkxgSq8RxUy6k43dWOm
Al6FYKyHksUwdVrLUsSoFtlfM7w8UhjdXDF/fvAvqvwWm9bPVCahEg==
-----END CERTIFICATE-----`

	path := filepath.Join(t.TempDir(), "oidc-ca.pem")
	if err := os.WriteFile(path, []byte(certPEM), 0o600); err != nil {
		t.Fatalf("write oidc ca file: %v", err)
	}

	config := OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "ovumcy",
		ClientSecret: "secret",
		RedirectURL:  "https://ovumcy.example.com/auth/oidc/callback",
		CAFile:       path,
	}

	if err := config.Validate(true, true); err != nil {
		t.Fatalf("expected valid OIDC CA file to pass validation, got %v", err)
	}
}

// TestOIDCConfigAllowsAutoProvisionBranches pins the branches the single
// allowlist test left uncovered: auto-provision disabled, an empty allowlist
// (permits any verified email but still rejects a blank one), and malformed
// emails under a non-empty allowlist. Auto-provisioning is a security-relevant
// gate, so each decision must be asserted, not just the happy/denied domain.
func TestOIDCConfigAllowsAutoProvisionBranches(t *testing.T) {
	// AutoProvision disabled always denies, even for an allowlisted domain.
	off := OIDCConfig{Enabled: true, AutoProvision: false, AutoProvisionAllowedDomains: []string{"example.com"}}
	if off.AllowsAutoProvision("owner@example.com") {
		t.Fatal("auto-provision disabled must deny")
	}

	// Empty allowlist permits any non-empty email but still rejects a blank one.
	open := OIDCConfig{Enabled: true, AutoProvision: true}
	if !open.AllowsAutoProvision("owner@anything.example") {
		t.Fatal("empty allowlist must permit any domain")
	}
	if open.AllowsAutoProvision("   ") {
		t.Fatal("blank email must be denied even with an empty allowlist")
	}

	// Non-empty allowlist rejects malformed emails (no domain to match).
	restricted := OIDCConfig{Enabled: true, AutoProvision: true, AutoProvisionAllowedDomains: []string{"example.com"}}
	for _, bad := range []string{"no-at-sign", "trailing@"} {
		if restricted.AllowsAutoProvision(bad) {
			t.Fatalf("malformed email %q must be denied under an allowlist", bad)
		}
	}
}

// TestOIDCConfigProviderLogoutEnabled pins the logout-mode predicate that
// decides whether logout redirects to the provider's end-session endpoint.
func TestOIDCConfigProviderLogoutEnabled(t *testing.T) {
	cases := []struct {
		mode OIDCLogoutMode
		want bool
	}{
		{OIDCLogoutModeProvider, true},
		{OIDCLogoutModeAuto, true},
		{OIDCLogoutModeLocal, false},
	}
	for _, tc := range cases {
		if got := (OIDCConfig{LogoutMode: tc.mode}).ProviderLogoutEnabled(); got != tc.want {
			t.Fatalf("ProviderLogoutEnabled(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// TestValidateDiscoveredJWKSURI pins the jwks_uri origin check: same-origin and
// empty pass; cross-origin or non-https (the SSRF vectors) are rejected before
// go-oidc ever fetches the key set.
func TestValidateDiscoveredJWKSURI(t *testing.T) {
	const issuer = "https://id.example.com"

	if err := validateDiscoveredJWKSURI("https://id.example.com/keys", issuer); err != nil {
		t.Fatalf("same-origin jwks_uri must pass: %v", err)
	}
	if err := validateDiscoveredJWKSURI("", issuer); err != nil {
		t.Fatalf("empty jwks_uri must defer to go-oidc, not error here: %v", err)
	}
	for _, bad := range []string{
		"https://evil.example.net/keys",    // cross-host
		"http://id.example.com/keys",       // non-https
		"https://169.254.169.254/keys",     // internal/metadata host
		"https://id.example.com:8443/keys", // cross-port
	} {
		if err := validateDiscoveredJWKSURI(bad, issuer); err == nil {
			t.Fatalf("jwks_uri %q must be rejected", bad)
		}
	}
}
