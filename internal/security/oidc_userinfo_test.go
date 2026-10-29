package security

// oidc_userinfo_test.go — the discovery-supplied userinfo_endpoint is pinned to
// the issuer origin. Unlike jwks_uri and token_endpoint, an endpoint that fails
// the pin is dropped rather than refused, the way end_session_endpoint is: the
// provider still signs in on what its ID tokens carry.

import (
	"context"
	"testing"
)

// TestSanitizeOIDCUserInfoEndpoint pins the userinfo_endpoint origin check: a
// same-origin https endpoint survives unchanged; every other shape is dropped.
func TestSanitizeOIDCUserInfoEndpoint(t *testing.T) {
	const issuer = "https://id.example.com"

	if got := sanitizeOIDCUserInfoEndpoint("https://id.example.com/userinfo", issuer); got != "https://id.example.com/userinfo" {
		t.Fatalf("a same-origin userinfo_endpoint must survive, got %q", got)
	}
	if got := sanitizeOIDCUserInfoEndpoint("https://id.example.com:443/userinfo", issuer); got != "https://id.example.com:443/userinfo" {
		t.Fatalf("an explicit default port is the issuer origin and must survive, got %q", got)
	}

	for name, endpoint := range map[string]string{
		"empty":               "",
		"cross-host":          "https://evil.example.net/userinfo",
		"look-alike host":     "https://id.example.com.evil.example.net/userinfo",
		"cross-port":          "https://id.example.com:8443/userinfo",
		"non-https":           "http://id.example.com/userinfo",
		"fragment":            "https://id.example.com/userinfo#frag",
		"relative":            "/userinfo",
		"leading whitespace":  " https://id.example.com/userinfo",
		"internal host":       "https://169.254.169.254/userinfo",
		"dials this machine":  "https://0.0.0.0/userinfo",
		"unparseable":         "https://id.example.com/%zz",
		"non-ASCII lookalike": "https://ｉｄ.example.com/userinfo",
	} {
		t.Run(name, func(t *testing.T) {
			if got := sanitizeOIDCUserInfoEndpoint(endpoint, issuer); got != "" {
				t.Fatalf("userinfo_endpoint %q must be dropped, got %q", endpoint, got)
			}
		})
	}

	if got := sanitizeOIDCUserInfoEndpoint("https://id.example.com/userinfo", "issuer-without-scheme"); got != "" {
		t.Fatalf("an issuer that does not parse as an absolute URL pins nothing, got %q", got)
	}
	if got := sanitizeOIDCUserInfoEndpoint("https://id.example.com/userinfo", ""); got != "" {
		t.Fatalf("an empty issuer pins nothing, got %q", got)
	}
}

// TestOIDC_RuntimePoC_UserInfoEndpointOnIssuerOriginIsKept: a same-origin
// userinfo_endpoint from real discovery is stored on the client.
func TestOIDC_RuntimePoC_UserInfoEndpointOnIssuerOriginIsKept(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	if _, _, err := client.loadProvider(client.clientContext(context.Background())); err != nil {
		t.Fatalf("loadProvider against mock IdP: %v", err)
	}
	if want := mock.issuer + "/userinfo"; client.userInfoEndpoint != want {
		t.Fatalf("userInfoEndpoint = %q, want the same-origin %q", client.userInfoEndpoint, want)
	}
}

// TestOIDC_RuntimePoC_UnusableUserInfoEndpointIsDroppedNotRefused: a foreign or
// absent userinfo_endpoint leaves the client without one, and the code exchange
// still completes — the pin never costs a provider its sign-in.
func TestOIDC_RuntimePoC_UnusableUserInfoEndpointIsDroppedNotRefused(t *testing.T) {
	for name, point := range map[string]func(mock *mockOIDCProvider){
		"foreign origin": func(mock *mockOIDCProvider) { mock.userInfoEndpoint = "https://attacker.example/userinfo" },
		"absent":         func(mock *mockOIDCProvider) { mock.omitUserInfoEndpoint = true },
	} {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			point(mock)
			mock.idToken = mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "nonce-userinfo-drop"))
			client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

			result, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-userinfo-drop")
			if err != nil {
				t.Fatalf("an unusable userinfo_endpoint must not refuse the sign-in: %v", err)
			}
			if result.Claims.Subject != "user-123" {
				t.Fatalf("subject = %q, want user-123", result.Claims.Subject)
			}
			if client.userInfoEndpoint != "" {
				t.Fatalf("userInfoEndpoint = %q, want it dropped", client.userInfoEndpoint)
			}
		})
	}
}
