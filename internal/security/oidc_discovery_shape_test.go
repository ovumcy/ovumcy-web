package security

import (
	"context"
	"net/url"
	"testing"
)

// An https URL with an empty host parses as absolute, and Go's dialer resolves
// an empty host to loopback: `https://:8443` as the issuer would post the client
// secret and authorization code to whatever listens locally on that port.
func TestValidateOIDCHTTPSURLRejectsEmptyHost(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{"https://:8443", "https:///.well-known", "https://:8443/auth/oidc/callback"} {
		if _, err := validateOIDCHTTPSURL(rawURL, "OIDC_TEST_URL"); err == nil {
			t.Fatalf("validateOIDCHTTPSURL(%q) accepted an empty host", rawURL)
		}
	}
}

// The origin pins compare hostnames; two empty hostnames must not count as the
// same origin, or an empty-host issuer would pin every empty-host endpoint (and
// loopback) to itself.
func TestSameOriginURLRefusesEmptyHosts(t *testing.T) {
	t.Parallel()

	emptyHost, err := url.Parse("https://:8443/token")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	otherEmptyHost, err := url.Parse("https://:8443")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	named, err := url.Parse("https://id.example.com:8443")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if sameOriginURL(emptyHost, otherEmptyHost) {
		t.Fatal("two empty-host URLs were treated as the same origin")
	}
	if sameOriginURL(emptyHost, named) || sameOriginURL(named, emptyHost) {
		t.Fatal("an empty-host URL was treated as the origin of a named host")
	}
	if got := sanitizeOIDCEndSessionEndpoint("https://:8443/logout", "https://:8443"); got != "" {
		t.Fatalf("end_session_endpoint on an empty-host issuer survived the pin: %q", got)
	}
	if err := validateDiscoveredTokenEndpoint("https://:8443/token", "https://:8443"); err == nil {
		t.Fatal("token_endpoint on an empty-host issuer passed the pin")
	}
	if err := validateDiscoveredJWKSURI("https://:8443/jwks", "https://:8443"); err == nil {
		t.Fatal("jwks_uri on an empty-host issuer passed the pin")
	}
}

// A discovery document whose metadata does not decode into the pinned struct is
// refused, not pinned on the fields encoding/json managed to fill before the
// type error: loadProvider must fail, and the client must hold no metadata.
func TestOIDC_RuntimePoC_UndecodableDiscoveryMetadataRefused(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryOverrides = map[string]any{"end_session_endpoint": 8443}

	caFile := writeIssuerCAFile(t, caPEM)
	client := NewOIDCClient(OIDCConfig{
		Enabled:      true,
		IssuerURL:    mock.issuer,
		ClientID:     "ovumcy",
		ClientSecret: "test-secret",
		RedirectURL:  "https://ovumcy.example/auth/oidc/callback",
		CAFile:       caFile,
		LoginMode:    OIDCLoginModeHybrid,
		LogoutMode:   OIDCLogoutModeAuto,
	})

	if _, _, err := client.loadProvider(client.clientContext(context.Background())); err == nil {
		t.Fatal("loadProvider accepted a discovery document whose metadata does not decode")
	}
	if client.oauthConfig != nil || client.verifier != nil || client.provider != nil {
		t.Fatal("a refused discovery document left provider state behind")
	}
}

// Positive control for the test above: the same document with a well-typed,
// same-origin end_session_endpoint loads and keeps the endpoint.
func TestOIDC_RuntimePoC_DecodableDiscoveryMetadataLoads(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryOverrides = map[string]any{"end_session_endpoint": mock.issuer + "/logout"}

	caFile := writeIssuerCAFile(t, caPEM)
	client := NewOIDCClient(OIDCConfig{
		Enabled:      true,
		IssuerURL:    mock.issuer,
		ClientID:     "ovumcy",
		ClientSecret: "test-secret",
		RedirectURL:  "https://ovumcy.example/auth/oidc/callback",
		CAFile:       caFile,
		LoginMode:    OIDCLoginModeHybrid,
		LogoutMode:   OIDCLogoutModeAuto,
	})

	if _, _, err := client.loadProvider(client.clientContext(context.Background())); err != nil {
		t.Fatalf("loadProvider refused a well-typed discovery document: %v", err)
	}
	if got := client.metadata.EndSessionEndpoint; got != mock.issuer+"/logout" {
		t.Fatalf("same-origin end_session_endpoint lost: got %q", got)
	}
}
