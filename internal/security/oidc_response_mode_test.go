package security

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// oidc_response_mode_test.go pins the OIDC_RESPONSE_MODE contract: form_post is
// the default and continues to pin response_mode=form_post on the authorize
// URL; query is an explicit opt-in that omits the parameter so the provider
// falls back to its query-redirect default. The PKCE challenge and nonce must
// be present in BOTH modes — the query opt-in must never weaken them, because
// the security argument for allowing the code in the URL is precisely that the
// code is unusable without the verifier that never leaves the sealed cookie.

func TestSanitizeOIDCConfigResponseModeDefaultsAndNormalizes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   OIDCResponseMode
		want OIDCResponseMode
	}{
		{name: "empty defaults to form_post", in: "", want: OIDCResponseModeFormPost},
		{name: "form_post preserved", in: "form_post", want: OIDCResponseModeFormPost},
		{name: "query preserved", in: "query", want: OIDCResponseModeQuery},
		{name: "uppercase normalized", in: "QUERY", want: OIDCResponseModeQuery},
		{name: "surrounding space trimmed", in: "  Form_Post  ", want: OIDCResponseModeFormPost},
		// An unknown value is normalized (lower/trim) but NOT coerced to a
		// default — sanitize leaves it for Validate to reject.
		{name: "unknown normalized but not coerced", in: " Bogus ", want: OIDCResponseMode("bogus")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeOIDCConfig(OIDCConfig{ResponseMode: tc.in}).ResponseMode
			if got != tc.want {
				t.Fatalf("sanitizeOIDCConfig ResponseMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestOIDCConfigValidateResponseMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mode    OIDCResponseMode
		wantErr bool
	}{
		{name: "empty is accepted (defaults to form_post)", mode: ""},
		{name: "form_post accepted", mode: OIDCResponseModeFormPost},
		{name: "query accepted", mode: OIDCResponseModeQuery},
		{name: "uppercase query accepted", mode: "QUERY"},
		{name: "unknown rejected", mode: "bogus", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := validOIDCConfigForTest()
			config.ResponseMode = tc.mode
			err := config.Validate(true, true)
			if tc.wantErr && err == nil {
				t.Fatalf("Validate with response mode %q expected an error, got nil", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate with response mode %q expected success, got %v", tc.mode, err)
			}
		})
	}
}

// TestOIDCVerifierNeverInTransport is the machine-checked backing for the
// reworded "No USABLE secret in transport" invariant: the query opt-in is only
// safe because the authorization code is inert without the PKCE verifier, and
// the verifier never appears in transport. In BOTH response modes the authorize
// URL must carry the S256 code_challenge (the hash) and NEVER the raw verifier;
// the verifier lives only in the sealed HttpOnly state cookie (asserted at the
// transport layer in internal/api/auth_oidc_response_mode_test.go).
func TestOIDCVerifierNeverInTransport(t *testing.T) {
	const verifier = "test-pkce-verifier-value-1234567890-abcdefghij"

	for _, mode := range []OIDCResponseMode{OIDCResponseModeFormPost, OIDCResponseModeQuery} {
		t.Run(string(mode), func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
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
				ResponseMode: mode,
			})

			raw, err := client.AuthCodeURL(context.Background(), "state-123", "nonce-abc", verifier, nil)
			if err != nil {
				t.Fatalf("AuthCodeURL: %v", err)
			}
			// The raw verifier must not leak anywhere in the URL (query or path).
			if strings.Contains(raw, verifier) {
				t.Fatalf("PKCE verifier leaked into the authorize URL in %s mode: %s", mode, raw)
			}
			query, err := url.ParseQuery(mustURLRawQuery(t, raw))
			if err != nil {
				t.Fatalf("parse authorize URL query: %v", err)
			}
			if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
				t.Fatalf("expected S256 code_challenge in %s mode, got method=%q challenge=%q", mode, query.Get("code_challenge_method"), query.Get("code_challenge"))
			}
			if query.Get("code_challenge") == verifier {
				t.Fatalf("code_challenge must be the S256 hash, not the raw verifier (%s mode)", mode)
			}
		})
	}
}

func mustURLRawQuery(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	return parsed.RawQuery
}

// TestAuthCodeURLResponseModeParam drives the real AuthCodeURL against the mock
// IdP and asserts the authorize URL carries response_mode=form_post in the
// default (and form_post) mode and omits it in query mode — while the PKCE
// S256 challenge and the nonce are present in every mode.
func TestAuthCodeURLResponseModeParam(t *testing.T) {
	cases := []struct {
		name             string
		mode             OIDCResponseMode
		wantResponseMode bool
	}{
		{name: "default (unset) pins form_post", mode: "", wantResponseMode: true},
		{name: "form_post pins form_post", mode: OIDCResponseModeFormPost, wantResponseMode: true},
		{name: "query omits response_mode", mode: OIDCResponseModeQuery, wantResponseMode: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
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
				ResponseMode: tc.mode,
			})

			raw, err := client.AuthCodeURL(context.Background(), "state-123", "nonce-abc", "verifier-xyz", nil)
			if err != nil {
				t.Fatalf("AuthCodeURL: %v", err)
			}
			parsed, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("parse authorize URL: %v", err)
			}
			query := parsed.Query()

			if got := query.Get("response_mode"); tc.wantResponseMode && got != "form_post" {
				t.Fatalf("expected response_mode=form_post, got %q (url=%s)", got, raw)
			} else if !tc.wantResponseMode && query.Has("response_mode") {
				t.Fatalf("query mode must not send response_mode, got %q (url=%s)", got, raw)
			}

			// PKCE and nonce must never be dropped, regardless of response mode.
			if query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
				t.Fatalf("PKCE S256 challenge missing in %s mode (url=%s)", tc.mode, raw)
			}
			if query.Get("nonce") != "nonce-abc" {
				t.Fatalf("nonce missing/altered in %s mode: %q (url=%s)", tc.mode, query.Get("nonce"), raw)
			}
		})
	}
}
