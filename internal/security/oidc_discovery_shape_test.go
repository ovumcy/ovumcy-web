package security

import (
	"context"
	"testing"
)

// The origin pins compare hostnames; two empty hostnames must not count as the
// same origin, or an empty-host issuer would pin every empty-host endpoint (and
// loopback) to itself. The config-time rejection of the same shape is a row of
// TestValidateOIDCHTTPSURLRejectsUnsafeInputs.
func TestSameOriginURLRefusesEmptyHosts(t *testing.T) {
	t.Parallel()

	emptyHost := mustParseTestURL(t, "https://:8443/token")
	otherEmptyHost := mustParseTestURL(t, "https://:8443")
	named := mustParseTestURL(t, "https://id.example.com:8443")
	if sameOriginURL(emptyHost, otherEmptyHost) {
		t.Fatal("two empty-host URLs were treated as the same origin")
	}
	if sameOriginURL(emptyHost, named) || sameOriginURL(named, emptyHost) {
		t.Fatal("an empty-host URL was treated as the origin of a named host")
	}
	if got := sanitizeOIDCEndSessionEndpoint("https://:8443/logout", "https://:8443"); got != "" {
		t.Fatalf("end_session_endpoint on an empty-host issuer survived the pin: %q", got)
	}
	// The unpinned mode (no issuer) enforces shape only, and an empty host is
	// not a shape the sanitizer may keep.
	if got := sanitizeOIDCEndSessionEndpoint("https://:8443/logout", ""); got != "" {
		t.Fatalf("empty-host end_session_endpoint survived the unpinned sanitizer: %q", got)
	}
	if err := validateDiscoveredTokenEndpoint("https://:8443/token", "https://:8443"); err == nil {
		t.Fatal("token_endpoint on an empty-host issuer passed the pin")
	}
	if err := validateDiscoveredJWKSURI("https://:8443/jwks", "https://:8443"); err == nil {
		t.Fatal("jwks_uri on an empty-host issuer passed the pin")
	}
}

// A discovery document whose end_session_endpoint does not decode keeps the
// provider loadable — the field is optional and its absence means local-only
// logout — while the same-origin jwks_uri in the same document is still pinned
// (the verifier is built). Pins the contract that a decode error is not a way
// to skip the sanitizer: the sanitizer runs on whatever decoded.
func TestOIDC_RuntimePoC_UndecodableEndSessionEndpointDegradesToLocalLogout(t *testing.T) {
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

	oauthConfig, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider refused a document whose optional end_session_endpoint does not decode: %v", err)
	}
	if oauthConfig == nil || verifier == nil {
		t.Fatal("loadProvider returned no oauth config or verifier")
	}
	if got := client.metadata.EndSessionEndpoint; got != "" {
		t.Fatalf("an undecodable end_session_endpoint produced a logout endpoint: %q", got)
	}
}

// Positive control: the same document with a well-typed, same-origin
// end_session_endpoint keeps the endpoint.
func TestOIDC_RuntimePoC_DecodableDiscoveryMetadataLoads(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.endSessionEndpoint = mock.issuer + "/logout"

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
