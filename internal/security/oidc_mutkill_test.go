package security

// oidc_mutkill_test.go — targeted kills for surviving gremlins mutants in
// oidc.go that the existing suite left LIVED or NOT_COVERED. Each test pins the
// exact observable behaviour a specific mutation would break and is verified
// red-on-mutant / green-on-original (apply mutation -> run -> revert). Grouped
// by the production concern they defend:
//
//   - loadProvider memoization guard (oidc.go:416)
//   - OIDC HTTP client construction: DefaultTransport clone (659) and the
//     custom-CA-added-to-system-roots pool (668), plus the bounded timeout
//     whose const line (21) Go cannot instrument
//   - AuthCodeURL extra-param sanitation (331)
//   - ExchangeCode end-to-end: error propagation (345/350/360/373), the
//     missing-id_token guard (355), the nonce check (363), and the
//     iat/auth_time zero-guards (378/382)
//
// The provider-facing tests reuse the mock IdP from oidc_runtime_poc_test.go
// (real TLS discovery + JWKS + token) so they exercise the true code path.

import (
	"context"
	"crypto/x509"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// newExchangeTestClient builds an OIDC client pointed at the mock IdP, trusting
// its private test CA — the same wiring a self-hosted operator uses.
func newExchangeTestClient(t *testing.T, mock *mockOIDCProvider, caFile string) *OIDCClient {
	t.Helper()
	return NewOIDCClient(OIDCConfig{
		Enabled:      true,
		IssuerURL:    mock.issuer,
		ClientID:     "ovumcy",
		ClientSecret: "test-secret",
		RedirectURL:  "https://ovumcy.example/auth/oidc/callback",
		CAFile:       caFile,
		LoginMode:    OIDCLoginModeHybrid,
		LogoutMode:   OIDCLogoutModeAuto,
	})
}

// mustSignMockIDToken signs an RS256 id_token with the mock IdP's real key so
// the production verifier (RS256 in oidcSupportedSigningAlgs) accepts it.
func mustSignMockIDToken(t *testing.T, mock *mockOIDCProvider, claims jwt.MapClaims) string {
	t.Helper()
	raw, err := signTestRS256IDToken(mock, claims)
	if err != nil {
		t.Fatalf("sign mock id_token: %v", err)
	}
	return raw
}

// TestLoadProviderMemoizesProvider kills the two CONDITIONALS_NEGATION mutants
// on the memoization guard `client.oauthConfig != nil && client.verifier != nil`
// (oidc.go:416). Negating either half forces a rebuild on every call, so the
// second loadProvider would rediscover the provider and hand back freshly
// allocated *oauth2.Config / *oidc.IDTokenVerifier values. The original returns
// the cached pointers, so pointer identity across two calls pins the guard.
func TestLoadProviderMemoizesProvider(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)
	client := newExchangeTestClient(t, mock, caFile)

	ctx := client.clientContext(context.Background())
	cfg1, ver1, err := client.loadProvider(ctx)
	if err != nil {
		t.Fatalf("first loadProvider: %v", err)
	}
	cfg2, ver2, err := client.loadProvider(ctx)
	if err != nil {
		t.Fatalf("second loadProvider: %v", err)
	}
	if cfg1 != cfg2 {
		t.Fatal("loadProvider must return the memoized *oauth2.Config on the second call, not rebuild it")
	}
	if ver1 != ver2 {
		t.Fatal("loadProvider must return the memoized *oidc.IDTokenVerifier on the second call, not rebuild it")
	}
}

// TestNewOIDCHTTPClientClonesDefaultTransport kills the CONDITIONALS_NEGATION
// mutant on `if transport == nil` (oidc.go:659). DefaultTransport is a
// *http.Transport in practice, so the original takes the else branch and clones
// it, inheriting its tuning (proxy-from-environment, connection-pool timeouts).
// The mutant would instead install a bare &http.Transport{} whose
// IdleConnTimeout is zero, so pinning the cloned tuning distinguishes them.
func TestNewOIDCHTTPClientClonesDefaultTransport(t *testing.T) {
	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok || def.IdleConnTimeout == 0 {
		t.Skip("DefaultTransport is not a *http.Transport with non-zero tuning on this platform")
	}

	client := newOIDCHTTPClient(OIDCConfig{}) // no CAFile -> plain clone path
	got, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected the OIDC client transport to be a *http.Transport, got %T", client.Transport)
	}
	if got.IdleConnTimeout != def.IdleConnTimeout {
		t.Fatalf("OIDC transport must be cloned from DefaultTransport (IdleConnTimeout %s), got %s (a bare transport?)", def.IdleConnTimeout, got.IdleConnTimeout)
	}
}

// TestNewOIDCHTTPClientAddsCustomCAToSystemRoots kills both CONDITIONALS_NEGATION
// mutants on `if poolErr != nil || roots == nil` (oidc.go:668). On a healthy
// host x509.SystemCertPool succeeds, so the original augments the system roots
// with the custom CA. Negating either half discards the system roots and trusts
// ONLY the custom bundle. CertPool.Equal distinguishes "system roots + bundle"
// from "fresh pool + bundle"; the guard skips when the platform's system pool is
// indistinguishable from empty (so the test never false-fails).
func TestNewOIDCHTTPClientAddsCustomCAToSystemRoots(t *testing.T) {
	sys, err := x509.SystemCertPool()
	if err != nil || sys == nil {
		t.Skip("system cert pool unavailable on this platform")
	}

	caPEM, _ := newTestCAAndLeaf(t)
	caFile := writeIssuerCAFile(t, caPEM)

	want := sys.Clone()
	if !want.AppendCertsFromPEM(caPEM) {
		t.Fatalf("failed to append the test CA to a system-pool clone")
	}
	fresh := x509.NewCertPool()
	fresh.AppendCertsFromPEM(caPEM)
	if want.Equal(fresh) {
		t.Skip("system cert pool is indistinguishable from empty here; system+bundle == fresh+bundle")
	}

	client := newOIDCHTTPClient(OIDCConfig{CAFile: caFile})
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("expected TLSClientConfig.RootCAs to be set when CAFile is provided")
	}
	if !transport.TLSClientConfig.RootCAs.Equal(want) {
		t.Fatal("OIDC HTTP client must ADD the custom CA to the system roots, not replace them with a fresh pool")
	}
}

// TestOIDCHTTPClientTimeoutIsBounded behaviourally pins the effect of the
// `defaultOIDCHTTPTimeout = 10 * time.Second` const (oidc.go:21). The
// ARITHMETIC_BASE mutant (`*`->`/`) collapses the const to a 0 (= unbounded)
// timeout — an SSRF/DoS concern on the server-side OIDC fetches. Go does not
// instrument const declaration lines, so gremlins reports the line NOT_COVERED
// regardless; this assertion pins the concrete, non-zero value the const feeds.
func TestOIDCHTTPClientTimeoutIsBounded(t *testing.T) {
	client := NewOIDCClient(OIDCConfig{Enabled: true})
	if client.httpClient == nil {
		t.Fatal("expected a bounded OIDC http client")
	}
	if client.httpClient.Timeout != 10*time.Second {
		t.Fatalf("OIDC HTTP client timeout must be a bounded 10s, got %s", client.httpClient.Timeout)
	}
}

// TestAuthCodeURLSkipsBlankExtraKeys kills the CONDITIONALS_NEGATION mutant on
// `if key == ""` in AuthCodeURL's extra-param loop (oidc.go:331). The guard
// drops blank keys and keeps real ones; negated, it would drop every real key
// and forward only the blank ones. A real key (prompt=login) alongside
// whitespace/empty keys pins the correct branch.
func TestAuthCodeURLSkipsBlankExtraKeys(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)
	client := newExchangeTestClient(t, mock, caFile)

	raw, err := client.AuthCodeURL(context.Background(), "state-1", "nonce-1", "verifier-1234567890-abcdef", map[string]string{
		"prompt": "login",
		"   ":    "whitespace-key-must-be-skipped",
		"":       "empty-key-must-be-skipped",
	})
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	query := mustParseTestURL(t, raw).Query()
	if query.Get("prompt") != "login" {
		t.Fatalf("a real extra key must be forwarded: prompt=%q (url=%s)", query.Get("prompt"), raw)
	}
	if strings.Contains(raw, "whitespace-key-must-be-skipped") || strings.Contains(raw, "empty-key-must-be-skipped") {
		t.Fatalf("blank/whitespace extra keys must be skipped, not forwarded: %s", raw)
	}
}

// TestExchangeCodeHappyPath drives the real code exchange end-to-end against the
// mock IdP and kills the cluster of survivors on the ExchangeCode success path:
//   - 345 (`if err != nil` after loadProvider): the negation returns an empty
//     result early; asserting the decoded claims catches it.
//   - 350 (`if err != nil` after Exchange) and 360 (after Verify) and 373
//     (after Claims): each negation returns an error on success; asserting a
//     nil error + populated claims catches them.
//   - 355 (`|| TrimSpace(rawIDToken) == ""`): the negation rejects a valid,
//     non-empty id_token as "missing".
//   - 363 (`Nonce != expectedNonce`): the negation rejects a MATCHING nonce.
//   - 378/382 (`iat > 0` / `auth_time > 0`, CONDITIONALS_NEGATION): the
//     negation drops present, positive timestamps; asserting the exact times
//     catches it.
func TestExchangeCodeHappyPath(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)

	iat := time.Now().Add(-time.Minute).Unix()
	authTime := time.Now().Add(-2 * time.Minute).Unix()
	mock.idToken = mustSignMockIDToken(t, mock, jwt.MapClaims{
		"iss":            mock.issuer,
		"sub":            "user-123",
		"aud":            "ovumcy",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            iat,
		"auth_time":      authTime,
		"nonce":          "nonce-abc",
		"email":          "  Owner@Example.com  ",
		"email_verified": true,
	})

	client := newExchangeTestClient(t, mock, caFile)
	res, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-abc")
	if err != nil {
		t.Fatalf("ExchangeCode happy path: %v", err)
	}

	if res.Claims.Issuer != mock.issuer {
		t.Fatalf("issuer = %q, want %q", res.Claims.Issuer, mock.issuer)
	}
	if res.Claims.Subject != "user-123" {
		t.Fatalf("subject = %q, want user-123", res.Claims.Subject)
	}
	if res.Claims.Email != "Owner@Example.com" {
		t.Fatalf("email = %q, want trimmed Owner@Example.com", res.Claims.Email)
	}
	if !res.Claims.EmailVerified {
		t.Fatal("email_verified must decode as true")
	}
	if res.Claims.IssuedAt.IsZero() || res.Claims.IssuedAt.Unix() != iat {
		t.Fatalf("IssuedAt = %v, want unix %d", res.Claims.IssuedAt, iat)
	}
	if res.Claims.AuthTime.IsZero() || res.Claims.AuthTime.Unix() != authTime {
		t.Fatalf("AuthTime = %v, want unix %d", res.Claims.AuthTime, authTime)
	}
	if res.Session.IDTokenHint != mock.idToken {
		t.Fatal("IDTokenHint must carry the raw id_token for provider logout")
	}
}

// TestExchangeCodeNonceMismatchRejected is the reject-branch companion to the
// happy path for the nonce check (oidc.go:363). The negated `!=` would ACCEPT a
// token whose nonce does not match the one bound to the sealed state cookie —
// defeating replay protection — so a mismatched nonce MUST error.
func TestExchangeCodeNonceMismatchRejected(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)

	mock.idToken = mustSignMockIDToken(t, mock, jwt.MapClaims{
		"iss":   mock.issuer,
		"sub":   "user-123",
		"aud":   "ovumcy",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": "issued-nonce",
		"email": "owner@example.com",
	})

	client := newExchangeTestClient(t, mock, caFile)
	_, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "expected-different-nonce")
	if err == nil {
		t.Fatal("a nonce that does not match the expected nonce must be rejected")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("expected a nonce-mismatch error, got %v", err)
	}
}

// TestExchangeCodeZeroTimestampsStayZero is the boundary companion for the
// iat/auth_time guards (oidc.go:378/382). CONDITIONALS_BOUNDARY turns `> 0` into
// `>= 0`, so a claim of exactly 0 would be converted to the 1970 epoch instead
// of staying the zero time. A token carrying iat=0 and auth_time=0 must yield
// zero (absent) timestamps, not 1970-01-01.
func TestExchangeCodeZeroTimestampsStayZero(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)

	mock.idToken = mustSignMockIDToken(t, mock, jwt.MapClaims{
		"iss":       mock.issuer,
		"sub":       "user-123",
		"aud":       "ovumcy",
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       0,
		"auth_time": 0,
		"nonce":     "nonce-zero",
		"email":     "owner@example.com",
	})

	client := newExchangeTestClient(t, mock, caFile)
	res, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-zero")
	if err != nil {
		t.Fatalf("ExchangeCode with zero timestamps: %v", err)
	}
	if !res.Claims.IssuedAt.IsZero() {
		t.Fatalf("iat=0 must yield a zero IssuedAt, got %v", res.Claims.IssuedAt)
	}
	if !res.Claims.AuthTime.IsZero() {
		t.Fatalf("auth_time=0 must yield a zero AuthTime, got %v", res.Claims.AuthTime)
	}
}
