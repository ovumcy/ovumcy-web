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
