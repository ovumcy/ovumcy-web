package security

// oidc_foreign_origin_no_request_test.go — no server-side OIDC request ever
// reaches a foreign origin. The refusal tests in oidc_runtime_poc_test.go show
// that a foreign jwks_uri, token_endpoint or cross-origin discovery redirect
// ends in an error; a check that refused only AFTER fetching would pass them
// too, having already sent the request — and, for the token endpoint, the
// client secret and authorization code with it. So each fetch the client makes
// is pointed at a second TLS server on another origin that counts every
// request it receives, the whole server-side sign-in (discovery, code
// exchange, id_token verification) is driven through ExchangeCode, and the
// count must be zero.
//
// Two controls keep that zero from being vacuous:
//
//   - reachability: after the sign-in, the same production-built HTTP client
//     requests the foreign server directly and must get through, and its
//     counter must then read one. The foreign server's CA sits in the same
//     OIDC_CA_FILE bundle as the issuer's, so it is reachable and trusted; a
//     zero before the probe is therefore the origin pin at work, not a TLS,
//     DNS or dial failure.
//   - the fetch happens: the same counter mounted on the issuer origin
//     (mockOIDCProvider.countedOnIssuer) receives the request when the endpoint
//     points there, so the operation does perform the fetch the foreign case
//     says it withheld.
//
// The authorization_endpoint and end_session_endpoint are not server fetches:
// the client only composes those URLs and hands them to the browser, so there
// is no server request to count. Their pins are covered by
// TestOIDC_RuntimePoC_ForeignAuthorizationEndpointIsRefusedAtProviderLoad and
// TestOIDC_RuntimePoC_HostPinRejectsCrossOriginEndSessionEndpoint.

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// originRequestCounter answers every request, on any path and method, with an
// empty JSON object and counts it.
type originRequestCounter struct {
	requests atomic.Int64
}

func (counter *originRequestCounter) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	counter.requests.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

// newForeignOriginServer starts a counting TLS server on its own loopback port
// — a different origin from the mock issuer — whose leaf is signed by a CA of
// its own, returned so the caller can trust it.
func newForeignOriginServer(t *testing.T) (*httptest.Server, *originRequestCounter, []byte) {
	t.Helper()
	caPEM, serverCert := newTestCAAndLeaf(t)
	counter := &originRequestCounter{}
	server := httptest.NewUnstartedServer(counter)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, counter, caPEM
}

// runOIDCSignInWithFetchOn builds a client that trusts both the mock issuer and
// the foreign server, lets point aim one server-side fetch at origin, and drives
// the full server-side sign-in. The mock issues a valid id_token, so every
// fetch the pins allow does happen.
func runOIDCSignInWithFetchOn(t *testing.T, mock *mockOIDCProvider, issuerCA []byte, foreignCA []byte, origin string, point func(mock *mockOIDCProvider, origin string)) (*OIDCClient, error) {
	t.Helper()
	point(mock, origin)
	mock.idToken = mustSignMockIDToken(t, mock, oidcLimitTestClaims(mock.issuer, "nonce-foreign-origin"))
	client := newExchangeTestClient(t, mock, writeIssuerCAFile(t, slices.Concat(issuerCA, foreignCA)))
	_, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-foreign-origin")
	return client, err
}

func requireOIDCFetchNeverLeavesTheIssuerOrigin(t *testing.T, fetch string, refusal string, point func(mock *mockOIDCProvider, origin string)) {
	t.Helper()

	t.Run("a foreign origin receives no request", func(t *testing.T) {
		mock, issuerCA := newMockOIDCProvider(t)
		foreign, foreignCounter, foreignCA := newForeignOriginServer(t)

		client, err := runOIDCSignInWithFetchOn(t, mock, issuerCA, foreignCA, foreign.URL, point)
		if got := foreignCounter.requests.Load(); got != 0 {
			t.Fatalf("%s on a foreign origin: the sign-in sent %d request(s) to %s; the origin pin must refuse before any request leaves the issuer origin", fetch, got, foreign.URL)
		}
		// The sign-in must end at the pin itself: any earlier failure would
		// leave the fetch unattempted and the zero above vacuous.
		if err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("%s on a foreign origin: sign-in error = %v, want the origin-pin refusal %q", fetch, err, refusal)
		}

		// Reachability control: the very client that sent nothing can reach and
		// trust the foreign server, so the zero above is the pin, not a failure
		// to connect.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, foreign.URL+"/reachability-probe", nil)
		if err != nil {
			t.Fatalf("build reachability probe: %v", err)
		}
		resp, err := client.httpClient.Do(req)
		if err != nil {
			t.Fatalf("reachability control: the OIDC client cannot reach the foreign server, so a zero count proves nothing: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("reachability control: foreign server answered %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if got := foreignCounter.requests.Load(); got != 1 {
			t.Fatalf("reachability control: foreign counter reads %d after one direct request, want 1", got)
		}
	})

	t.Run("the issuer origin receives the request", func(t *testing.T) {
		mock, issuerCA := newMockOIDCProvider(t)

		// The counter answers {} rather than a real document, so the sign-in
		// fails after the fetch; what matters here is that the fetch arrived.
		_, _ = runOIDCSignInWithFetchOn(t, mock, issuerCA, nil, mock.issuer+"/counted", point)
		if got := mock.countedOnIssuer.requests.Load(); got < 1 {
			t.Fatalf("%s on the issuer origin: the counter saw %d requests; the sign-in must perform this fetch, or the foreign-origin zero is vacuous", fetch, got)
		}
	})
}

// TestOIDC_RuntimePoC_CrossOriginDiscoveryRedirectSendsNoRequest: a discovery
// response redirecting to another origin is refused by the client's
// CheckRedirect before the redirected request is sent.
func TestOIDC_RuntimePoC_CrossOriginDiscoveryRedirectSendsNoRequest(t *testing.T) {
	requireOIDCFetchNeverLeavesTheIssuerOrigin(t, "discovery redirect", "oidc http redirect left the issuer origin", func(mock *mockOIDCProvider, origin string) {
		mock.discoveryRedirectTo = origin + "/.well-known/openid-configuration"
	})
}

// TestOIDC_RuntimePoC_ForeignJWKSURIGetsNoRequest: a foreign jwks_uri is
// refused at provider load, so id_token verification never fetches keys from
// it.
func TestOIDC_RuntimePoC_ForeignJWKSURIGetsNoRequest(t *testing.T) {
	requireOIDCFetchNeverLeavesTheIssuerOrigin(t, "jwks_uri", "oidc jwks_uri origin must match the issuer origin", func(mock *mockOIDCProvider, origin string) {
		mock.jwksURI = origin + "/jwks"
	})
}

// TestOIDC_RuntimePoC_ForeignTokenEndpointGetsNoRequest: a foreign
// token_endpoint is refused at provider load, so the code exchange never POSTs
// the client secret and authorization code to it.
func TestOIDC_RuntimePoC_ForeignTokenEndpointGetsNoRequest(t *testing.T) {
	requireOIDCFetchNeverLeavesTheIssuerOrigin(t, "token_endpoint", "oidc token_endpoint origin must match the issuer origin", func(mock *mockOIDCProvider, origin string) {
		mock.tokenEndpoint = origin + "/token"
	})
}
