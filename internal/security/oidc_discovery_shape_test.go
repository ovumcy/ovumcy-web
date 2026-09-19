package security

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// The origin pins compare hostnames, and a host that names no peer — an empty
// host, `0.0.0.0`, `[::]` — must never satisfy one: an issuer of that shape
// would otherwise pin every endpoint of the same shape to itself, and so to
// whatever listens on this machine. The config-time rejection of the same
// shapes is a row of TestValidateOIDCHTTPSURLRejectsUnsafeInputs.
func TestSameOriginURLRefusesHostsThatDialThisMachine(t *testing.T) {
	t.Parallel()

	// The spellings past the first three are the ones a check written as
	// `net.ParseIP(host).IsUnspecified()` would wave through: the rest of
	// 0.0.0.0/8, a zone identifier, and the IPv4-mapped form. The last four are
	// what Go declines to parse as an address at all and a platform resolver
	// with inet_aton semantics still reads as an address — 0.0.0.0/8 in octal,
	// short or hex form, and an octal-looking private address whose meaning
	// depends on the resolver.
	for _, host := range []string{
		"", "0.0.0.0", "::", "0.0.0.1", "0.1.2.3", "::%eth0", "::ffff:0.0.0.1",
		"0", "0.1", "00.0.0.0", "0.0.0.00", "0x0", "0x00000000", "0X0.0", "192.168.001.010",
	} {
		t.Run("refuses "+host, func(t *testing.T) {
			t.Parallel()
			if !HostDialsThisMachine(host) {
				t.Fatalf("HostDialsThisMachine(%q) = false", host)
			}
		})
	}
	// A self-hosted issuer on loopback or a LAN address is a supported
	// deployment, so neither may be confused with an address that names no peer:
	// this check bounds a misconfigured or hostile OIDC URL, it is not an SSRF
	// egress gate.
	for _, host := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.10", "id.example.com"} {
		if HostDialsThisMachine(host) {
			t.Fatalf("HostDialsThisMachine(%q) = true", host)
		}
	}

	named := mustParseTestURL(t, "https://id.example.com:8443")
	for _, issuer := range []string{"https://:8443", "https://0.0.0.0:8443", "https://[::]:8443"} {
		withPath := mustParseTestURL(t, issuer+"/token")
		bare := mustParseTestURL(t, issuer)
		if sameOriginURL(withPath, bare) {
			t.Fatalf("%s was treated as its own origin", issuer)
		}
		if sameOriginURL(withPath, named) || sameOriginURL(named, withPath) {
			t.Fatalf("%s was treated as the origin of a named host", issuer)
		}
		if got := sanitizeOIDCEndSessionEndpoint(issuer+"/logout", issuer); got != "" {
			t.Fatalf("end_session_endpoint on issuer %s survived the pin: %q", issuer, got)
		}
		// The unpinned mode (no issuer) enforces shape only, and a host that
		// dials this machine is not a shape the sanitizer may keep.
		if got := sanitizeOIDCEndSessionEndpoint(issuer+"/logout", ""); got != "" {
			t.Fatalf("end_session_endpoint %s survived the unpinned sanitizer: %q", issuer, got)
		}
		if err := validateDiscoveredTokenEndpoint(issuer+"/token", issuer); err == nil {
			t.Fatalf("token_endpoint on issuer %s passed the pin", issuer)
		}
		if err := validateDiscoveredJWKSURI(issuer+"/jwks", issuer); err == nil {
			t.Fatalf("jwks_uri on issuer %s passed the pin", issuer)
		}
		if err := validateDiscoveredAuthorizationEndpoint(issuer + "/authorize"); err == nil {
			t.Fatalf("authorization_endpoint on issuer %s passed the host check", issuer)
		}
	}
}

// The authorize URL is the one discovery endpoint the browser navigates to, and
// the only one this change checks without pinning it to the issuer origin. Both
// halves are deliberate and both are pinned here: an off-origin https authorize
// URL is accepted (so nobody reads its acceptance as an oversight of the loop
// above), a plaintext or machine-dialing one is not.
func TestValidateDiscoveredAuthorizationEndpoint(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{"https://id.example.com/authorize", "https://sso.example.net/authorize"} {
		if err := validateDiscoveredAuthorizationEndpoint(endpoint); err != nil {
			t.Fatalf("authorization_endpoint %q was refused: %v", endpoint, err)
		}
	}
	// An absent endpoint is refused, not deferred: oauth2.AuthCodeURL does not
	// validate its AuthURL, so an empty one composes a relative
	// "?client_id=…&state=…" that walks the owner back into ovumcy with state
	// and nonce in the query instead of failing sign-in.
	for _, endpoint := range []string{
		"",
		"   ",
		"http://id.example.com/authorize",
		"/authorize",
		"https://0:8443/authorize",
		"https://0.0.0.0:8443/authorize",
		"https://[::]:8443/authorize",
		"https://:8443/authorize",
		"https://0.0.0.1/authorize",
	} {
		if err := validateDiscoveredAuthorizationEndpoint(endpoint); err == nil {
			t.Fatalf("authorization_endpoint %q was accepted", endpoint)
		}
	}
}

// The same refusal, driven through discovery: a provider whose document omits
// authorization_endpoint, or names one that dials this machine or is not https,
// leaves SSO unavailable instead of handing oauth2 an AuthURL to compose the
// browser redirect from. The mock issuer listens on 127.0.0.1, so the accepted
// case is also the loopback deployment staying supported.
func TestOIDC_RuntimePoC_DiscoveredAuthorizationEndpointIsChecked(t *testing.T) {
	const accepted = "loopback same-origin"
	documents := map[string]string{
		"absent":        "",
		"unspecified":   `"authorization_endpoint": "https://0.0.0.0:8443/authorize",`,
		"hex this-host": `"authorization_endpoint": "https://0x0:8443/authorize",`,
		"empty host":    `"authorization_endpoint": "https://:8443/authorize",`,
		"plaintext":     `"authorization_endpoint": "http://id.example.com/authorize",`,
		"relative":      `"authorization_endpoint": "/authorize",`,
		accepted:        `"authorization_endpoint": "%[1]s/authorize",`,
	}
	for name, field := range documents {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			mock.discoveryRawBody = fmt.Sprintf(`{
				"issuer": %[1]q,
				`+field+`
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
			if name == accepted {
				if err != nil {
					t.Fatalf("a loopback issuer's same-origin authorize URL was refused: %v", err)
				}
				if want := mock.issuer + "/authorize"; oauthConfig.Endpoint.AuthURL != want {
					t.Fatalf("AuthURL = %q, want %q", oauthConfig.Endpoint.AuthURL, want)
				}
				return
			}
			if err == nil {
				t.Fatalf("discovery with authorization_endpoint %s was accepted (AuthURL %q)", name, oauthConfig.Endpoint.AuthURL)
			}
			if client.oauthConfig != nil {
				t.Fatal("a refused discovery document still installed an oauth config")
			}
		})
	}
}

// encoding/json keeps a field it has already decoded when a later occurrence of
// the same key fails to decode, and reports the error afterwards. A discovery
// document naming end_session_endpoint twice — first as a cross-origin string,
// then as a number — therefore hands loadProvider both a decode error and the
// attacker's endpoint. Treating that error as a reason to skip the sanitizer is
// what published the endpoint; running the sanitizer unconditionally is what
// this pins. A document where the field merely fails to decode is not a
// regression test: it yields the empty string either way.
func TestOIDC_RuntimePoC_DuplicateEndSessionEndpointKeyIsStillSanitized(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryRawBody = fmt.Sprintf(`{
		"issuer": %[1]q,
		"authorization_endpoint": "%[1]s/authorize",
		"token_endpoint": "%[1]s/token",
		"jwks_uri": "%[1]s/jwks",
		"response_types_supported": ["code"],
		"subject_types_supported": ["public"],
		"id_token_signing_alg_values_supported": ["RS256"],
		"end_session_endpoint": "https://evil.example/logout",
		"end_session_endpoint": 8443
	}`, mock.issuer)

	// The premise above is a property of encoding/json, not of ovumcy: assert it
	// directly, or a future decoder that dropped the field would leave this test
	// passing while exercising nothing.
	probe := oidcProviderMetadata{}
	if err := json.Unmarshal([]byte(mock.discoveryRawBody), &probe); err == nil {
		t.Fatal("the duplicated key produced no decode error")
	}
	if probe.EndSessionEndpoint != "https://evil.example/logout" {
		t.Fatalf("premise broken: the decode error dropped the endpoint (%q)", probe.EndSessionEndpoint)
	}

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
		t.Fatalf("loadProvider refused a document whose only fault is an optional field: %v", err)
	}
	if oauthConfig == nil || verifier == nil {
		t.Fatal("loadProvider returned no oauth config or verifier")
	}
	if got := client.metadata.EndSessionEndpoint; got != "" {
		t.Fatalf("a cross-origin end_session_endpoint survived a partial decode: %q", got)
	}
}
