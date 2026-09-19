package security

// oidc_response_limits_test.go — the OIDC HTTP client bounds every response
// it reads, in body and in headers, and an oversized one is an error rather
// than a truncated document that parses. The reads happen inside go-oidc
// (discovery, JWKS) and oauth2 (token), so each site is driven through the
// production path against the TLS mock IdP from oidc_runtime_poc_test.go.
// Positive control for normal-size responses: TestExchangeCodeHappyPath
// (discovery + token + JWKS verification end-to-end).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func requireOIDCResponseTooLarge(t *testing.T, site string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: a response body over %d bytes was accepted; the OIDC HTTP client must cap every body it returns (oidcBoundedBodyTransport)", site, oidcResponseBodyLimit)
	}
	// go-oidc and oauth2 format the read error with %v, so the chain is lost
	// and only the text survives.
	if !strings.Contains(err.Error(), errOIDCResponseTooLarge.Error()) {
		t.Fatalf("%s: refused for another reason than the body cap: %v", site, err)
	}
}

func oidcLimitTestClaims(issuer string, nonce string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   issuer,
		"sub":   "user-123",
		"aud":   "ovumcy",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": nonce,
	}
}

func TestOIDCResponseLimitRefusesOversizedDiscoveryBody(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryPadding = int(oidcResponseBodyLimit)
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	_, _, err := client.loadProvider(client.clientContext(context.Background()))
	requireOIDCResponseTooLarge(t, "discovery", err)
}

func TestOIDCResponseLimitRefusesOversizedJWKSBody(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.jwksPadding = int(oidcResponseBodyLimit)
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	_, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider: %v", err)
	}
	// go-oidc fetches the JWKS lazily, on the first verification, through the
	// client captured at NewProvider — that fetch is the one under test.
	_, err = verifier.Verify(context.Background(), mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "n")))
	requireOIDCResponseTooLarge(t, "jwks", err)
}

// A JWKS well past any real key set but under the cap still loads and
// verifies, so the cap cannot be tightened into refusing legitimate IdPs.
func TestOIDCResponseLimitAcceptsLargeJWKSUnderTheCap(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.jwksPadding = int(oidcResponseBodyLimit) - 4096
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	_, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "n"))); err != nil {
		t.Fatalf("a JWKS under the %d-byte cap must verify: %v", oidcResponseBodyLimit, err)
	}
}

// The token response is padded past oauth2's own 1 MiB LimitReader: without
// the client cap that read truncates silently and the form-encoded prefix —
// access_token and a valid id_token — parses into a completed sign-in.
func TestOIDCResponseLimitRefusesOversizedTokenBody(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.idToken = mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "nonce-token"))
	mock.tokenPadding = 1<<20 + 1024
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	_, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-token")
	requireOIDCResponseTooLarge(t, "token", err)
}

func TestOIDCResponseLimitRefusesOversizedResponseHeaders(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryHeaderPadding = int(oidcResponseHeaderLimit)
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, caPEM))

	_, _, err := client.loadProvider(client.clientContext(context.Background()))
	if err == nil {
		t.Fatalf("a discovery response with over %d bytes of headers was accepted; the OIDC transport must set MaxResponseHeaderBytes", oidcResponseHeaderLimit)
	}
	if !strings.Contains(err.Error(), "response headers exceeded") {
		t.Fatalf("refused for another reason than the header cap: %v", err)
	}
}

// The cap is exact: a body of the limit reads to a clean EOF, one byte more
// is an error, and the bytes handed out never pass the limit.
func TestOIDCBoundedBodyBoundary(t *testing.T) {
	const limit = 16
	read := func(size int) ([]byte, error) {
		body := &oidcBoundedBody{body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), size))), remaining: limit}
		return io.ReadAll(body)
	}

	got, err := read(limit)
	if err != nil || len(got) != limit {
		t.Fatalf("a body of exactly the limit must read whole: %d bytes, err %v", len(got), err)
	}
	got, err = read(limit + 1)
	if !errors.Is(err, errOIDCResponseTooLarge) {
		t.Fatalf("a body one byte over the limit must fail with errOIDCResponseTooLarge, got %v", err)
	}
	if len(got) > limit {
		t.Fatalf("the bounded body handed out %d bytes past a %d-byte limit", len(got), limit)
	}

	// A caller that reads again after the refusal must not see a clean EOF:
	// with a body of exactly limit+1 bytes the underlying reader is drained,
	// and only the sticky error keeps the truncated prefix from ending well.
	body := &oidcBoundedBody{body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), limit+1))), remaining: limit}
	if _, err := io.ReadAll(body); !errors.Is(err, errOIDCResponseTooLarge) {
		t.Fatalf("first read past the limit must fail with errOIDCResponseTooLarge, got %v", err)
	}
	if n, err := body.Read(make([]byte, 8)); n != 0 || !errors.Is(err, errOIDCResponseTooLarge) {
		t.Fatalf("a read after the refusal returned %d bytes, err %v; it must keep failing with errOIDCResponseTooLarge", n, err)
	}
}

// The refusal tests pad relative to the constants, so they move with them;
// this pins the values SECURITY.md, docs/oidc.md and the changelog promise,
// on the client newOIDCHTTPClient actually builds.
func TestOIDCResponseLimitsAreTheDocumentedValues(t *testing.T) {
	client := newOIDCHTTPClient(OIDCConfig{})
	bounded, ok := client.Transport.(*oidcBoundedBodyTransport)
	if !ok {
		t.Fatalf("OIDC client transport is %T, want *oidcBoundedBodyTransport", client.Transport)
	}
	if bounded.limit != 512<<10 {
		t.Fatalf("OIDC response body cap is %d bytes; the documented value is 512 KiB", bounded.limit)
	}
	base, ok := oidcClientBaseTransport(client)
	if !ok || base.MaxResponseHeaderBytes != 64<<10 {
		t.Fatalf("OIDC response header cap must be the documented 64 KiB, got transport %T", bounded.base)
	}
}
