package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// newTestCAAndLeaf produces a fresh test CA and a leaf certificate
// signed by it that is valid for 127.0.0.1 / ::1 / localhost. This is
// the only way to get an x509 chain that ovumcy's `OIDC_CA_FILE` path
// will trust: httptest.NewTLSServer ships only a self-signed leaf with
// no CA basic-constraints, which fails verification because the leaf
// is not a CA.
func newTestCAAndLeaf(t *testing.T) (caPEM []byte, serverCert tls.Certificate) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey (CA): %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ovumcy-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(2 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate (CA): %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("ParseCertificate (CA): %v", err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey (leaf): %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ovumcy-test-mock-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(2 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		DNSNames:     []string{"localhost"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate (leaf): %v", err)
	}
	serverCert = tls.Certificate{
		Certificate: [][]byte{leafDER, caDER},
		PrivateKey:  leafKey,
	}
	return caPEM, serverCert
}

// These tests are the closest thing to a "Mock IdP runtime PoC" we can
// reasonably build inside a unit-test framework. Rather than spinning up
// ovumcy as a server, fronting it with a TLS reverse proxy, and driving a
// browser through the OIDC dance, we stand up a controlled OIDC provider
// via httptest.NewTLSServer and exercise the exact production code path
// (security.OIDCClient.loadProvider, the resulting *oidc.IDTokenVerifier)
// against malicious-by-construction discovery metadata and a forged ID
// token. The discovery + JWKS + token endpoints are real HTTP over real
// TLS; the assertions land directly on the hardened code from Sprint 1.
//
// Two contracts are pinned:
//
//   - Finding #1: sanitizeOIDCEndSessionEndpoint host-pins the
//     discovery-supplied logout endpoint to the configured issuer URL,
//     so a discovery document pointing at a different host is rejected
//     and the provider falls back to local-only logout.
//
//   - Finding #6: provider.Verifier is configured with an explicit
//     SupportedSigningAlgs allowlist (RS/ES/PS + EdDSA), so a token
//     forged with alg=HS256 using the JWKS public key as the HMAC
//     secret cannot pass verification — closing the classical
//     algorithm-confusion downgrade lane.

type mockOIDCProvider struct {
	server *httptest.Server

	privateKey *rsa.PrivateKey
	keyID      string

	// endSessionEndpoint controls what the malicious discovery document
	// advertises. Tests set this to the URL the audit cares about (same
	// origin / different origin / different port / etc.).
	endSessionEndpoint string

	// jwksURI overrides the advertised jwks_uri (default: same-origin
	// issuer+"/jwks"). Tests set it to a cross-origin value to exercise the
	// jwks_uri origin-pin rejection in loadProvider.
	jwksURI string

	// tokenEndpoint overrides the advertised token_endpoint (default:
	// same-origin issuer+"/token"). Tests set it to a cross-origin value to
	// exercise the token_endpoint origin-pin rejection in loadProvider.
	tokenEndpoint string

	// discoveryRedirectTo, when set, makes the canonical discovery path
	// answer 302 to this URL instead of serving the document. Tests use it
	// to exercise the redirect origin-pin on the OIDC HTTP client (a
	// same-origin target must be followed, a cross-origin one refused).
	discoveryRedirectTo string

	// issuer is the URL returned in discovery and in JWT iss claims.
	// httptest.NewTLSServer assigns it at startup.
	issuer string
}

func newMockOIDCProvider(t *testing.T) (*mockOIDCProvider, []byte) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	mock := &mockOIDCProvider{privateKey: priv, keyID: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", mock.serveDiscovery)
	mux.HandleFunc("/alt-discovery", mock.serveDiscoveryDocument)
	mux.HandleFunc("/jwks", mock.serveJWKS)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "authorize stub: PoC tests do not drive the browser flow", http.StatusNotImplemented)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token stub: PoC tests do not exchange the code", http.StatusNotImplemented)
	})

	caPEM, serverCert := newTestCAAndLeaf(t)
	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	mock.server = server
	mock.issuer = mock.server.URL

	t.Cleanup(mock.server.Close)
	return mock, caPEM
}

func (m *mockOIDCProvider) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	if m.discoveryRedirectTo != "" {
		http.Redirect(w, r, m.discoveryRedirectTo, http.StatusFound)
		return
	}
	m.serveDiscoveryDocument(w, r)
}

func (m *mockOIDCProvider) serveDiscoveryDocument(w http.ResponseWriter, r *http.Request) {
	jwksURI := m.issuer + "/jwks"
	if m.jwksURI != "" {
		jwksURI = m.jwksURI
	}
	tokenEndpoint := m.issuer + "/token"
	if m.tokenEndpoint != "" {
		tokenEndpoint = m.tokenEndpoint
	}
	payload := map[string]any{
		"issuer":                                m.issuer,
		"authorization_endpoint":                m.issuer + "/authorize",
		"token_endpoint":                        tokenEndpoint,
		"jwks_uri":                              jwksURI,
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	if m.endSessionEndpoint != "" {
		payload["end_session_endpoint"] = m.endSessionEndpoint
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (m *mockOIDCProvider) serveJWKS(w http.ResponseWriter, r *http.Request) {
	pub := m.privateKey.PublicKey
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	payload := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": m.keyID,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// writeIssuerCAFile writes the test CA PEM that signed the mock IdP's
// leaf certificate to a temp file. ovumcy reads CA bundles through
// OIDC_CA_FILE / security.OIDCConfig.CAFile, so this is the natural
// integration point — exactly the same code path a self-hosted
// operator would use with a private IdP CA.
func writeIssuerCAFile(t *testing.T, caPEM []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "issuer-ca.pem")
	if err := os.WriteFile(path, caPEM, 0o600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}
	return path
}

// TestOIDC_RuntimePoC_HostPinRejectsCrossOriginEndSessionEndpoint is the
// runtime contract for Finding #1: even a fully-valid discovery document
// served over real TLS by the configured issuer cannot trick Ovumcy into
// adopting a logout endpoint on a different host. The malicious endpoint
// satisfies every shape check (HTTPS, no fragment, absolute) — the only
// thing that catches it is the issuer host-pin added by sanitizeOIDCEnd
// SessionEndpoint(rawEndpoint, issuerURL).
func TestOIDC_RuntimePoC_HostPinRejectsCrossOriginEndSessionEndpoint(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	// Same-shape but different host — exactly the attacker payload the
	// audit cared about.
	mock.endSessionEndpoint = "https://attacker.example/logout?client=ovumcy"

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
		t.Fatalf("loadProvider against mock IdP: %v", err)
	}
	if got := client.metadata.EndSessionEndpoint; got != "" {
		t.Fatalf("host-pin failed: malicious end_session_endpoint %q was accepted (got %q); the discovery document should have been stripped to fall back to local logout", mock.endSessionEndpoint, got)
	}
}

// TestOIDC_RuntimePoC_HostPinAcceptsSameOriginEndSessionEndpoint is the
// "happy path" companion to the test above. A legitimate IdP that
// publishes its logout endpoint on the same origin as its issuer must
// flow through, otherwise the host-pin would also break provider logout
// for normal deployments.
func TestOIDC_RuntimePoC_HostPinAcceptsSameOriginEndSessionEndpoint(t *testing.T) {
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
		t.Fatalf("loadProvider against mock IdP: %v", err)
	}
	if got := client.metadata.EndSessionEndpoint; got != mock.endSessionEndpoint {
		t.Fatalf("same-origin end_session_endpoint should pass the host-pin; got %q, want %q", got, mock.endSessionEndpoint)
	}
}

// TestOIDC_RuntimePoC_JWKSOriginPinRejectsCrossOrigin is the runtime contract
// for the jwks_uri SSRF pin: a discovery document served over real TLS by the
// configured issuer cannot point the server-side key fetch at a different
// origin. loadProvider must refuse before the verifier ever fetches the JWKS.
func TestOIDC_RuntimePoC_JWKSOriginPinRejectsCrossOrigin(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.jwksURI = "https://attacker.example/jwks"

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
		t.Fatal("jwks_uri origin pin failed: a cross-origin jwks_uri was accepted; loadProvider must refuse it")
	}
}

// TestOIDC_RuntimePoC_JWKSOriginPinAcceptsSameOrigin is the happy-path companion:
// the default same-origin jwks_uri must continue to load, otherwise the pin
// would break normal self-hosted providers (Keycloak / authentik / Authelia).
func TestOIDC_RuntimePoC_JWKSOriginPinAcceptsSameOrigin(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	// jwksURI left empty → defaults to the same-origin issuer+"/jwks".

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
		t.Fatalf("same-origin jwks_uri must pass the pin: %v", err)
	}
}

// TestOIDC_RuntimePoC_TokenEndpointOriginPinRejectsCrossOrigin is the runtime
// contract for the token_endpoint SSRF pin: a discovery document served over
// real TLS by the configured issuer cannot point the server-side code
// exchange (which carries the client secret and authorization code) at a
// different origin. loadProvider must refuse before any exchange can run.
func TestOIDC_RuntimePoC_TokenEndpointOriginPinRejectsCrossOrigin(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.tokenEndpoint = "https://attacker.example/token"

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
		t.Fatal("token_endpoint origin pin failed: a cross-origin token_endpoint was accepted; loadProvider must refuse it")
	}
}

// TestOIDC_RuntimePoC_DiscoveryRedirectCrossOriginRefused is the runtime
// contract for the redirect origin-pin on the OIDC HTTP client. The
// endpoints from discovery are origin-pinned as URLs, but without a
// CheckRedirect policy the HTTP requests themselves could still be steered
// off-origin by a redirecting response. A discovery fetch answered with a
// 302 to another host must fail closed — the policy refuses before any
// cross-origin request is sent.
func TestOIDC_RuntimePoC_DiscoveryRedirectCrossOriginRefused(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryRedirectTo = "https://attacker.example/.well-known/openid-configuration"

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
		t.Fatal("redirect origin pin failed: a cross-origin discovery redirect was followed; loadProvider must refuse it")
	}
}

// TestOIDC_RuntimePoC_DiscoveryRedirectSameOriginFollowed is the happy-path
// companion: a same-origin redirect (an IdP normalizing its discovery path)
// must still be followed, otherwise the redirect pin would break legitimate
// deployments whose well-known path answers with a local redirect.
func TestOIDC_RuntimePoC_DiscoveryRedirectSameOriginFollowed(t *testing.T) {
	mock, caPEM := newMockOIDCProvider(t)
	mock.discoveryRedirectTo = mock.issuer + "/alt-discovery"

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
		t.Fatalf("same-origin discovery redirect must be followed: %v", err)
	}
}

// TestOIDC_RuntimePoC_AlgorithmConfusionRejected is the runtime contract
// for Finding #6: even when an attacker controls the upstream IdP's
// discovery + JWKS + token responses, they cannot trick the verifier
// into accepting a token signed with HS256 (using the JWKS RSA public
// key as the HMAC secret) — the classical algorithm-confusion downgrade
// path. The fix is the explicit SupportedSigningAlgs allowlist passed
// to provider.Verifier; this test forges exactly that token and
// asserts the verifier refuses to open it.
func TestOIDC_RuntimePoC_AlgorithmConfusionRejected(t *testing.T) {
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
	})

	_, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider: %v", err)
	}

	// Forge an ID token with alg=HS256, using the marshaled RSA public
	// key as the symmetric secret. This is the textbook
	// algorithm-confusion payload.
	pubBytes := x509.MarshalPKCS1PublicKey(&mock.privateKey.PublicKey)
	hmacSecret := pubBytes

	claims := jwt.MapClaims{
		"iss":   mock.issuer,
		"sub":   "attacker",
		"aud":   "ovumcy",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": "any-nonce",
	}
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	forged.Header["kid"] = mock.keyID
	rawForged, err := forged.SignedString(hmacSecret)
	if err != nil {
		t.Fatalf("sign forged HS256 token: %v", err)
	}

	if _, err := verifier.Verify(context.Background(), rawForged); err == nil {
		t.Fatal("algorithm confusion succeeded: verifier accepted an HS256-forged token that was signed with the JWKS RSA public key as the HMAC secret. The SupportedSigningAlgs allowlist is not being applied.")
	}
	// Also assert that the verifier still accepts a properly signed RS256
	// token from the same mock IdP — otherwise the test would pass for
	// the wrong reason (a verifier that rejects everything).
	rs256, err := signTestRS256IDToken(mock, claims)
	if err != nil {
		t.Fatalf("sign RS256 control token: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), rs256); err != nil {
		t.Fatalf("control RS256 token rejected by verifier (the allowlist is too strict): %v", err)
	}
}

// TestOIDC_RuntimePoC_AlgorithmNoneRejected is a companion contract:
// the historical alg=none lane (no signature at all) must also be
// rejected by the verifier. go-oidc has refused alg=none for years, but
// the explicit allowlist makes the contract test trivial — and if a
// future library swap silently re-enables it the test catches it.
func TestOIDC_RuntimePoC_AlgorithmNoneRejected(t *testing.T) {
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
	})
	_, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider: %v", err)
	}

	// Hand-craft an unsigned JWT (alg=none).
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := fmt.Sprintf(`{"iss":%q,"sub":"attacker","aud":"ovumcy","exp":%d,"iat":%d,"nonce":"any"}`,
		mock.issuer, time.Now().Add(time.Hour).Unix(), time.Now().Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(body))
	unsigned := header + "." + payload + "."

	if _, err := verifier.Verify(context.Background(), unsigned); err == nil {
		t.Fatal("verifier accepted an alg=none token; SupportedSigningAlgs allowlist must exclude it")
	}
}

// signTestRS256IDToken signs a control token with the mock IdP's real
// RSA key so the algorithm-confusion test can assert positive behaviour
// on the safe path.
func signTestRS256IDToken(mock *mockOIDCProvider, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = mock.keyID
	return token.SignedString(mock.privateKey)
}

// Sanity helper: confirm that the mock IdP serves a valid discovery
// document. Useful if a future change to the test fixture breaks the
// happy-path tests and we need to bisect.
func TestOIDC_RuntimePoC_MockProviderReachable(t *testing.T) {
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
	})
	oauthCfg, verifier, err := client.loadProvider(client.clientContext(context.Background()))
	if err != nil {
		t.Fatalf("loadProvider: %v", err)
	}
	if oauthCfg.Endpoint.AuthURL == "" {
		t.Fatal("mock provider did not surface an authorize endpoint")
	}
	if verifier == nil {
		t.Fatal("mock provider did not produce an ID token verifier")
	}
	if _, err := url.Parse(mock.issuer); err != nil {
		t.Fatalf("mock issuer URL malformed: %v", err)
	}
}

// allowedAlgs is the spec ovumcy ships in oidcSupportedSigningAlgs.
// Kept in this file so a code reader can see at a glance what the
// runtime PoC expects to find on the verifier; if the production list
// is expanded or trimmed, regenerate this slice to match.
var _ = oidc.Config{SupportedSigningAlgs: oidcSupportedSigningAlgs()}

// Make sure the test helpers below compile even if the asn1 / pkix
// imports get tree-shaken; this is a no-op at runtime.
var _ = pkix.Name{}
