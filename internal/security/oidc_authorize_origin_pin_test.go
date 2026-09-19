package security

import (
	"context"
	"fmt"
	"testing"

	"golang.org/x/net/idna"
)

// The authorization_endpoint is pinned to the issuer origin at provider load,
// as jwks_uri and token_endpoint are. It is the browser hop carrying state,
// nonce, client_id and redirect_uri, so a discovery document naming a foreign
// origin there leaves SSO unavailable rather than sending the owner to a
// sign-in page the issuer never served. The same-origin document is the
// positive control: the pin refuses the foreign host, not every document.
func TestOIDC_RuntimePoC_ForeignAuthorizationEndpointIsRefusedAtProviderLoad(t *testing.T) {
	for name, authorize := range map[string]string{
		"same origin":            "%[1]s/authorize",
		"foreign host":           "https://sso.example.net/authorize",
		"issuer host other port": "https://127.0.0.1:1/authorize",
	} {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			mock.discoveryRawBody = fmt.Sprintf(`{
				"issuer": %[1]q,
				"authorization_endpoint": "`+authorize+`",
				"token_endpoint": "%[1]s/token",
				"jwks_uri": "%[1]s/jwks",
				"response_types_supported": ["code"],
				"subject_types_supported": ["public"],
				"id_token_signing_alg_values_supported": ["RS256"]
			}`, mock.issuer)
			client := NewOIDCClient(OIDCConfig{
				Enabled:      true,
				IssuerURL:    mock.issuer,
				ClientID:     "ovumcy",
				ClientSecret: "test-secret",
				RedirectURL:  "https://ovumcy.example/auth/oidc/callback",
				CAFile:       writeIssuerCAFile(t, caPEM),
				LoginMode:    OIDCLoginModeHybrid,
				LogoutMode:   OIDCLogoutModeAuto,
			})

			oauthConfig, _, err := client.loadProvider(client.clientContext(context.Background()))
			if name == "same origin" {
				if err != nil {
					t.Fatalf("a same-origin authorize URL was refused: %v", err)
				}
				if want := mock.issuer + "/authorize"; oauthConfig.Endpoint.AuthURL != want {
					t.Fatalf("AuthURL = %q, want %q", oauthConfig.Endpoint.AuthURL, want)
				}
				return
			}
			if err == nil {
				t.Fatalf("discovery with an off-origin authorization_endpoint was accepted (AuthURL %q)", oauthConfig.Endpoint.AuthURL)
			}
			if client.oauthConfig != nil {
				t.Fatal("a refused discovery document still installed an oauth config")
			}
		})
	}
}

// Full-width and other non-ASCII host spellings. Go's HTTP transport, like a
// browser, maps a host through IDNA before dialing it, so a spelling the check
// would read as an ordinary name can dial this machine. The premise is asserted
// through that same mapping. The refusal is driven where it matters most: an
// issuer is the origin every endpoint pin compares against, so an issuer
// spelled this way would carry every endpoint spelled the same way past its pin.
func TestHostDialsThisMachineRefusesNonASCIIHostSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		rawURL       string
		dialsLocally bool
	}{
		{rawURL: "https://０.０.０.０:8443", dialsLocally: true},
		{rawURL: "https://%EF%BC%90.%EF%BC%90.%EF%BC%90.%EF%BC%90:8443", dialsLocally: true},
		{rawURL: "https://０x０:8443", dialsLocally: true},
		{rawURL: "https://bücher.example"},
	} {
		issuer := mustParseTestURL(t, tc.rawURL)
		host := issuer.Hostname()
		if tc.dialsLocally {
			mapped, err := idna.Lookup.ToASCII(host)
			if err != nil || !HostDialsThisMachine(mapped) {
				t.Fatalf("premise broken: %q maps to %q (err %v), which does not dial this machine", host, mapped, err)
			}
		}
		if !HostDialsThisMachine(host) {
			t.Fatalf("HostDialsThisMachine(%q) = false for %s", host, tc.rawURL)
		}
		if _, err := validateOIDCHTTPSURL(tc.rawURL, "OIDC_ISSUER_URL"); err == nil {
			t.Fatalf("OIDC_ISSUER_URL %s was accepted", tc.rawURL)
		}
		if sameOriginURL(mustParseTestURL(t, tc.rawURL+"/authorize"), issuer) {
			t.Fatalf("an endpoint on issuer %s passed the origin pin", tc.rawURL)
		}
		if got := sanitizeOIDCEndSessionEndpoint(tc.rawURL+"/logout", ""); got != "" {
			t.Fatalf("end_session_endpoint on %s survived the unpinned sanitizer: %q", tc.rawURL, got)
		}
	}
	// The ASCII (xn--) spelling of an internationalized name stays usable.
	if HostDialsThisMachine("xn--bcher-kva.example") {
		t.Fatal("the A-label spelling of an internationalized host was refused")
	}
}

// Spellings of 0.0.0.0 that exist only once a whole URL is parsed: a trailing
// root dot, an empty label, and the IPv4-mapped IPv6 literal inside brackets.
// Each is driven through url.Parse and Hostname() — what every caller hands the
// check — and through the configured-URL validator that refuses it at boot.
func TestHostDialsThisMachineRefusesUnspecifiedSpellingsThroughAFullURL(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"https://0.0.0.0.:8443",
		"https://0..0:8443",
		"https://[::ffff:0.0.0.0]:8443",
	} {
		host := mustParseTestURL(t, rawURL).Hostname()
		if !HostDialsThisMachine(host) {
			t.Fatalf("HostDialsThisMachine(%q) = false for %s", host, rawURL)
		}
		if _, err := validateOIDCHTTPSURL(rawURL, "OIDC_ISSUER_URL"); err == nil {
			t.Fatalf("OIDC_ISSUER_URL %s was accepted", rawURL)
		}
	}
}
