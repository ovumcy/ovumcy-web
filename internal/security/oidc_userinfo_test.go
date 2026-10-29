package security

// oidc_userinfo_test.go — the discovery-supplied userinfo_endpoint is pinned to
// the issuer origin, and UserInfoClaims reads the email scope's claims from it.
// Unlike jwks_uri and token_endpoint, an endpoint that fails the pin is dropped
// rather than refused, the way end_session_endpoint is: the provider still
// signs in on what its ID tokens carry.

import (
	"context"
	"errors"
	"net/http"
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

// exchangeWithoutEmail runs the real code exchange against the mock with an ID
// token that carries no email claim — the shape a conforming provider issues
// for the code flow — and returns the client and its result.
func exchangeWithoutEmail(t *testing.T, mock *mockOIDCProvider, caPEM []byte) (*OIDCClient, OIDCExchangeResult) {
	t.Helper()
	mock.idToken = mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "nonce-userinfo"))
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))
	exchange, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-userinfo")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if exchange.Claims.Email != "" || exchange.Claims.EmailVerified {
		t.Fatalf("the control ID token must carry no email claims, got %+v", exchange.Claims)
	}
	return client, exchange
}

// TestOIDCUserInfoClaimsReadsTheEmailScopeClaims: the UserInfo response's email
// and email_verified replace the ID token's (absent) ones, the identity key is
// untouched, and the request carries the access token the exchange returned.
func TestOIDCUserInfoClaimsReadsTheEmailScopeClaims(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.userInfoBody = map[string]any{"sub": "user-123", "email": "  Owner@Example.com  ", "email_verified": true}
	client, exchange := exchangeWithoutEmail(t, mock, caPEM)

	claims, err := client.UserInfoClaims(context.Background(), exchange)
	if err != nil {
		t.Fatalf("UserInfoClaims: %v", err)
	}
	if claims.Email != "Owner@Example.com" || !claims.EmailVerified {
		t.Fatalf("email claims = (%q, %v), want the trimmed UserInfo address, verified", claims.Email, claims.EmailVerified)
	}
	if claims.Issuer != exchange.Claims.Issuer || claims.Subject != exchange.Claims.Subject || !claims.IssuedAt.Equal(exchange.Claims.IssuedAt) {
		t.Fatalf("UserInfoClaims must keep the ID token's identity and timestamps, got %+v want %+v", claims, exchange.Claims)
	}
	if got := mock.userInfoRequests.Load(); got != 1 {
		t.Fatalf("the UserInfo endpoint saw %d requests, want 1", got)
	}
	if got, _ := mock.userInfoAuthorization.Load().(string); got != "Bearer test-access-token" {
		t.Fatalf("UserInfo Authorization = %q, want the exchange's access token as a bearer token", got)
	}
}

// TestOIDCUserInfoClaimsReadsEmailVerifiedInEveryShape: email_verified is taken
// from the response as sent — a JSON boolean, the string form some providers
// emit, or absent (unverified) — and never carried over from anywhere else.
func TestOIDCUserInfoClaimsReadsEmailVerifiedInEveryShape(t *testing.T) {
	for name, testCase := range map[string]struct {
		body map[string]any
		want bool
	}{
		"boolean true":  {body: map[string]any{"sub": "user-123", "email": "owner@example.com", "email_verified": true}, want: true},
		"string true":   {body: map[string]any{"sub": "user-123", "email": "owner@example.com", "email_verified": "true"}, want: true},
		"boolean false": {body: map[string]any{"sub": "user-123", "email": "owner@example.com", "email_verified": false}, want: false},
		"absent":        {body: map[string]any{"sub": "user-123", "email": "owner@example.com"}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			mock.userInfoBody = testCase.body
			client, exchange := exchangeWithoutEmail(t, mock, caPEM)

			claims, err := client.UserInfoClaims(context.Background(), exchange)
			if err != nil {
				t.Fatalf("UserInfoClaims: %v", err)
			}
			if claims.EmailVerified != testCase.want {
				t.Fatalf("EmailVerified = %v, want %v", claims.EmailVerified, testCase.want)
			}
		})
	}
}

// TestOIDCUserInfoClaimsBindsTheResponseToThisSignIn: a UserInfo response is
// used only when it is about the ID token's subject (OIDC Core 1.0 §5.3.2) and,
// where it says who issued it and for whom, names this issuer and this client.
// Every refusal is an error, never ErrOIDCUserInfoUnavailable: the request was
// made and its answer is unusable.
func TestOIDCUserInfoClaimsBindsTheResponseToThisSignIn(t *testing.T) {
	const email = "owner@example.com"
	for name, testCase := range map[string]struct {
		body   func(issuer string) map[string]any
		signed bool
		accept bool
	}{
		"matching sub": {
			body:   func(string) map[string]any { return map[string]any{"sub": "user-123", "email": email} },
			accept: true,
		},
		"another sub": {
			body: func(string) map[string]any { return map[string]any{"sub": "user-456", "email": email} },
		},
		"sub differing only in whitespace": {
			body: func(string) map[string]any { return map[string]any{"sub": " user-123", "email": email} },
		},
		"no sub": {
			body: func(string) map[string]any { return map[string]any{"email": email} },
		},
		"this issuer and a string aud naming this client": {
			body: func(issuer string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "iss": issuer, "aud": "ovumcy"}
			},
			accept: true,
		},
		"an aud array including this client": {
			body: func(issuer string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "iss": issuer, "aud": []string{"other", "ovumcy"}}
			},
			accept: true,
		},
		"a null aud": {
			body: func(issuer string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "aud": nil}
			},
			accept: true,
		},
		"another issuer": {
			body: func(string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "iss": "https://other-issuer.example"}
			},
		},
		"an iss that is not a string": {
			body: func(string) map[string]any { return map[string]any{"sub": "user-123", "email": email, "iss": 7} },
		},
		"a string aud naming another client": {
			body: func(string) map[string]any { return map[string]any{"sub": "user-123", "email": email, "aud": "other"} },
		},
		"an aud array without this client": {
			body: func(string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "aud": []string{"other", "another"}}
			},
		},
		"an aud that is neither a string nor an array of strings": {
			body: func(string) map[string]any { return map[string]any{"sub": "user-123", "email": email, "aud": 7} },
		},
		"signed for this issuer and client": {
			body: func(issuer string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "iss": issuer, "aud": "ovumcy"}
			},
			signed: true,
			accept: true,
		},
		"signed for another client": {
			body: func(issuer string) map[string]any {
				return map[string]any{"sub": "user-123", "email": email, "iss": issuer, "aud": "other"}
			},
			signed: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			mock.userInfoBody = testCase.body(mock.issuer)
			mock.userInfoSigned = testCase.signed
			client, exchange := exchangeWithoutEmail(t, mock, caPEM)

			claims, err := client.UserInfoClaims(context.Background(), exchange)
			if testCase.accept {
				if err != nil {
					t.Fatalf("UserInfoClaims refused a response bound to this sign-in: %v", err)
				}
				if claims.Email != email {
					t.Fatalf("Email = %q, want %q", claims.Email, email)
				}
				return
			}
			if err == nil {
				t.Fatalf("UserInfoClaims accepted a response not bound to this sign-in, returning %+v", claims)
			}
			if errors.Is(err, ErrOIDCUserInfoUnavailable) {
				t.Fatalf("a refused response must not read as an unavailable endpoint: %v", err)
			}
			if got := mock.userInfoRequests.Load(); got != 1 {
				t.Fatalf("the UserInfo endpoint saw %d requests, want 1", got)
			}
		})
	}
}

// TestOIDCUserInfoClaimsRefusesAFailedRequest: a UserInfo answer other than 200
// fails the call, and is not mistaken for a provider without the endpoint.
func TestOIDCUserInfoClaimsRefusesAFailedRequest(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.userInfoStatus = http.StatusUnauthorized
	client, exchange := exchangeWithoutEmail(t, mock, caPEM)

	_, err := client.UserInfoClaims(context.Background(), exchange)
	if err == nil || errors.Is(err, ErrOIDCUserInfoUnavailable) {
		t.Fatalf("a 401 from UserInfo must fail the call as a request failure, got %v", err)
	}
	if err.Error() != "fetch oidc userinfo failed" {
		t.Fatalf("a failed UserInfo request must not carry the provider's status or body, got %q", err)
	}
}

// TestOIDCUserInfoClaimsReportsAnAbsentEndpointWithoutARequest: a provider that
// advertises no userinfo_endpoint gets ErrOIDCUserInfoUnavailable and no
// request is made.
func TestOIDCUserInfoClaimsReportsAnAbsentEndpointWithoutARequest(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.omitUserInfoEndpoint = true
	mock.userInfoBody = map[string]any{"sub": "user-123", "email": "owner@example.com", "email_verified": true}
	client, exchange := exchangeWithoutEmail(t, mock, caPEM)

	if _, err := client.UserInfoClaims(context.Background(), exchange); !errors.Is(err, ErrOIDCUserInfoUnavailable) {
		t.Fatalf("UserInfoClaims error = %v, want ErrOIDCUserInfoUnavailable", err)
	}
	if got := mock.userInfoRequests.Load(); got != 0 {
		t.Fatalf("the UserInfo handler saw %d requests; a provider with no advertised endpoint must get none", got)
	}
}

// TestOIDCUserInfoClaimsNeedsAnExchangeThisClientMinted: a result assembled
// outside ExchangeCode carries no access token, so it reaches no endpoint.
func TestOIDCUserInfoClaimsNeedsAnExchangeThisClientMinted(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.userInfoBody = map[string]any{"sub": "user-123", "email": "owner@example.com", "email_verified": true}
	client, exchange := exchangeWithoutEmail(t, mock, caPEM)

	forged := OIDCExchangeResult{Claims: exchange.Claims, Session: exchange.Session}
	_, err := client.UserInfoClaims(context.Background(), forged)
	if err == nil || errors.Is(err, ErrOIDCUserInfoUnavailable) {
		t.Fatalf("an exchange with no access token must fail the call, got %v", err)
	}
	if got := mock.userInfoRequests.Load(); got != 0 {
		t.Fatalf("the UserInfo endpoint saw %d requests for an exchange with no access token, want 0", got)
	}
}

// TestOIDCUserInfoClaimsRefusesBeforeAnyProvider: a disabled client, and an
// enabled one whose issuer cannot be discovered, fail without a result.
func TestOIDCUserInfoClaimsRefusesBeforeAnyProvider(t *testing.T) {
	if _, err := NewOIDCClient(OIDCConfig{}).UserInfoClaims(context.Background(), OIDCExchangeResult{}); err == nil {
		t.Fatal("a disabled client must refuse UserInfoClaims")
	}

	unreachable := NewOIDCClient(OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://127.0.0.1:1",
		ClientID:     "ovumcy",
		ClientSecret: "test-secret",
		RedirectURL:  "https://ovumcy.example/auth/oidc/callback",
	})
	_, err := unreachable.UserInfoClaims(context.Background(), OIDCExchangeResult{})
	if err == nil || errors.Is(err, ErrOIDCUserInfoUnavailable) {
		t.Fatalf("an undiscoverable provider must fail UserInfoClaims as a discovery failure, got %v", err)
	}
}
