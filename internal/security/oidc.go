package security

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const OIDCCallbackPath = "/auth/oidc/callback"
const defaultOIDCHTTPTimeout = 10 * time.Second
const maxOIDCCABundleBytes int64 = 1 << 20

type OIDCLoginMode string

const (
	OIDCLoginModeHybrid   OIDCLoginMode = "hybrid"
	OIDCLoginModeOIDCOnly OIDCLoginMode = "oidc_only"
)

type OIDCLogoutMode string

const (
	OIDCLogoutModeLocal    OIDCLogoutMode = "local"
	OIDCLogoutModeProvider OIDCLogoutMode = "provider"
	OIDCLogoutModeAuto     OIDCLogoutMode = "auto"
)

// OIDCResponseMode selects how the provider returns the authorization code on
// the callback. form_post (the default) has the IdP auto-POST the code in the
// request body; query has it appended to the redirect URL as a GET. query is an
// explicit opt-in for providers that cannot form_post (better-auth, Dex,
// Pocket ID <2.7): the code lands in the URL, but it is unusable without the
// PKCE verifier, which never leaves the sealed HttpOnly state cookie (it is
// never in transport). See docs/SECURITY_INVARIANTS.md → OIDC response mode.
type OIDCResponseMode string

const (
	OIDCResponseModeFormPost OIDCResponseMode = "form_post"
	OIDCResponseModeQuery    OIDCResponseMode = "query"
)

type OIDCConfig struct {
	Enabled                     bool
	IssuerURL                   string
	ClientID                    string
	ClientSecret                string
	RedirectURL                 string
	CAFile                      string
	AutoProvision               bool
	LoginMode                   OIDCLoginMode
	LogoutMode                  OIDCLogoutMode
	ResponseMode                OIDCResponseMode
	PostLogoutRedirectURL       string
	AutoProvisionAllowedDomains []string
}

type OIDCClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	// IssuedAt is the ID token "iat" claim (always present per RFC).
	IssuedAt time.Time
	// AuthTime is the ID token "auth_time" claim. Zero when the provider did
	// not include the claim (it is REQUIRED only when max_age was requested).
	AuthTime time.Time
}

type OIDCSession struct {
	EndSessionEndpoint string
	IDTokenHint        string
}

type OIDCExchangeResult struct {
	Claims  OIDCClaims
	Session OIDCSession
}

type OIDCClient struct {
	config OIDCConfig

	mu          sync.Mutex
	httpClient  *http.Client
	provider    *oidc.Provider
	metadata    oidcProviderMetadata
	oauthConfig *oauth2.Config
	verifier    *oidc.IDTokenVerifier
}

type oidcProviderMetadata struct {
	EndSessionEndpoint string `json:"end_session_endpoint"`
	JWKSURI            string `json:"jwks_uri"`
}

func NewOIDCClient(config OIDCConfig) *OIDCClient {
	config = sanitizeOIDCConfig(config)
	return &OIDCClient{
		config:     config,
		httpClient: newOIDCHTTPClient(config),
	}
}

func (config OIDCConfig) Validate(cookieSecure bool, registrationOpen bool) error {
	config = sanitizeOIDCConfig(config)
	if !config.Enabled {
		return nil
	}
	if err := config.validateRuntimeModes(cookieSecure, registrationOpen); err != nil {
		return err
	}
	if err := config.validateRequiredFields(); err != nil {
		return err
	}
	if err := config.validateIssuerURL(); err != nil {
		return err
	}
	redirectURL, err := config.validateRedirectURL()
	if err != nil {
		return err
	}
	if err := config.validatePostLogoutRedirectURL(redirectURL); err != nil {
		return err
	}
	if err := config.validateCABundle(); err != nil {
		return err
	}
	return config.validateProvisioningDomains()
}

func (config OIDCConfig) validateRuntimeModes(cookieSecure bool, registrationOpen bool) error {
	if !cookieSecure {
		return errors.New("OIDC_ENABLED=true requires COOKIE_SECURE=true")
	}
	if config.LoginMode != OIDCLoginModeHybrid && config.LoginMode != OIDCLoginModeOIDCOnly {
		return errors.New("OIDC_LOGIN_MODE must be hybrid or oidc_only")
	}
	if config.LogoutMode != OIDCLogoutModeLocal && config.LogoutMode != OIDCLogoutModeProvider && config.LogoutMode != OIDCLogoutModeAuto {
		return errors.New("OIDC_LOGOUT_MODE must be local, provider, or auto")
	}
	if config.ResponseMode != OIDCResponseModeFormPost && config.ResponseMode != OIDCResponseModeQuery {
		return errors.New("OIDC_RESPONSE_MODE must be form_post or query")
	}
	if config.AutoProvision && !registrationOpen {
		return errors.New("OIDC_AUTO_PROVISION=true requires REGISTRATION_MODE=open")
	}
	return nil
}

func (config OIDCConfig) validateRequiredFields() error {
	switch {
	case config.IssuerURL == "":
		return errors.New("OIDC_ISSUER_URL is required when OIDC_ENABLED=true")
	case config.ClientID == "":
		return errors.New("OIDC_CLIENT_ID is required when OIDC_ENABLED=true")
	case config.ClientSecret == "":
		return errors.New("OIDC_CLIENT_SECRET is required when OIDC_ENABLED=true")
	case config.RedirectURL == "":
		return errors.New("OIDC_REDIRECT_URL is required when OIDC_ENABLED=true")
	default:
		return nil
	}
}

func (config OIDCConfig) validateIssuerURL() error {
	_, err := validateOIDCHTTPSURL(config.IssuerURL, "OIDC_ISSUER_URL")
	return err
}

func (config OIDCConfig) validateRedirectURL() (*url.URL, error) {
	redirectURL, err := validateOIDCHTTPSURL(config.RedirectURL, "OIDC_REDIRECT_URL")
	if err != nil {
		return nil, err
	}
	if path.Clean(strings.TrimSpace(redirectURL.Path)) != OIDCCallbackPath {
		return nil, fmt.Errorf("OIDC_REDIRECT_URL path must be %s", OIDCCallbackPath)
	}
	return redirectURL, nil
}

func (config OIDCConfig) validatePostLogoutRedirectURL(redirectURL *url.URL) error {
	if config.PostLogoutRedirectURL == "" {
		return nil
	}
	postLogoutURL, err := validateOIDCHTTPSURL(config.PostLogoutRedirectURL, "OIDC_POST_LOGOUT_REDIRECT_URL")
	if err != nil {
		return err
	}
	if !sameOriginURL(redirectURL, postLogoutURL) {
		return errors.New("OIDC_POST_LOGOUT_REDIRECT_URL must match the OIDC redirect origin")
	}
	return nil
}

func (config OIDCConfig) validateCABundle() error {
	if config.CAFile == "" {
		return nil
	}
	return validateOIDCCABundle(config.CAFile)
}

func (config OIDCConfig) validateProvisioningDomains() error {
	for _, domain := range config.AutoProvisionAllowedDomains {
		if !isValidProvisioningDomain(domain) {
			return fmt.Errorf("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS contains invalid domain %q", domain)
		}
	}
	return nil
}

func validateOIDCHTTPSURL(rawURL string, envName string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || !parsedURL.IsAbs() {
		return nil, fmt.Errorf("%s must be an absolute URL", envName)
	}
	if !strings.EqualFold(parsedURL.Scheme, "https") {
		return nil, fmt.Errorf("%s must use https", envName)
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("%s must not include query or fragment", envName)
	}
	// A non-ASCII host is one HostDialsThisMachine refuses too, but naming it
	// separately tells the operator the actual fix: spell the host in its ASCII
	// `xn--` form.
	if hostHasNonASCII(parsedURL.Hostname()) {
		return nil, fmt.Errorf("%s host must be ASCII: write an internationalized domain in its xn-- (punycode) form", envName)
	}
	if HostDialsThisMachine(parsedURL.Hostname()) {
		return nil, fmt.Errorf("%s must name a remote host", envName)
	}
	return parsedURL, nil
}

// hostHasNonASCII reports whether host holds any byte outside ASCII — the
// spelling HostDialsThisMachine refuses because IDNA mapping decides what it
// dials.
func hostHasNonASCII(host string) bool {
	for index := range len(host) {
		if host[index] >= utf8.RuneSelf {
			return true
		}
	}
	return false
}

func (client *OIDCClient) Enabled() bool {
	return client != nil && client.config.Enabled
}

func (config OIDCConfig) LocalPublicAuthEnabled() bool {
	if !config.Enabled {
		return true
	}
	return config.LoginMode != OIDCLoginModeOIDCOnly
}

func (config OIDCConfig) ProviderLogoutEnabled() bool {
	return config.LogoutMode == OIDCLogoutModeAuto || config.LogoutMode == OIDCLogoutModeProvider
}

func (config OIDCConfig) ResolvedPostLogoutRedirectURL() string {
	config = sanitizeOIDCConfig(config)
	if config.PostLogoutRedirectURL != "" {
		return config.PostLogoutRedirectURL
	}

	redirectURL, err := url.Parse(config.RedirectURL)
	if err != nil || !redirectURL.IsAbs() {
		return ""
	}
	redirectURL.Path = "/login"
	redirectURL.RawQuery = ""
	redirectURL.Fragment = ""
	return redirectURL.String()
}

func (config OIDCConfig) AllowsAutoProvision(email string) bool {
	if !config.AutoProvision {
		return false
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return false
	}
	if len(config.AutoProvisionAllowedDomains) == 0 {
		return true
	}
	atIndex := strings.LastIndex(normalizedEmail, "@")
	if atIndex < 0 || atIndex == len(normalizedEmail)-1 {
		return false
	}
	domain := normalizedProvisioningDomain(normalizedEmail[atIndex+1:])
	for _, allowedDomain := range config.AutoProvisionAllowedDomains {
		if domain == allowedDomain {
			return true
		}
	}
	return false
}

func (client *OIDCClient) LocalPublicAuthEnabled() bool {
	if client == nil {
		return true
	}
	return client.config.LocalPublicAuthEnabled()
}

func (client *OIDCClient) Config() OIDCConfig {
	if client == nil {
		return OIDCConfig{}
	}
	return client.config
}

// AuthCodeURL builds the provider authorize URL. Additional extra parameters
// (e.g. prompt=login, max_age=0) are passed through verbatim via
// oauth2.SetAuthURLParam so callers can force a fresh re-authentication for
// step-up flows without touching the base login parameters.
func (client *OIDCClient) AuthCodeURL(ctx context.Context, state string, nonce string, codeVerifier string, extra map[string]string) (string, error) {
	if !client.Enabled() {
		return "", errors.New("oidc is disabled")
	}
	oauthConfig, _, err := client.loadProvider(client.clientContext(ctx))
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(strings.TrimSpace(nonce)),
		oauth2.S256ChallengeOption(strings.TrimSpace(codeVerifier)),
	}
	// Only pin response_mode=form_post when that mode is in effect. In query
	// mode we intentionally omit the parameter so the provider falls back to its
	// query-redirect default (the whole point of the query opt-in). form_post
	// stays the default and is sent explicitly. The code returned in the query
	// is unusable without the PKCE verifier, which never leaves the sealed state
	// cookie — see the response-mode note in the security constitution.
	if client.config.ResponseMode != OIDCResponseModeQuery {
		opts = append(opts, oauth2.SetAuthURLParam("response_mode", "form_post"))
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		opts = append(opts, oauth2.SetAuthURLParam(key, value))
	}
	return oauthConfig.AuthCodeURL(strings.TrimSpace(state), opts...), nil
}

func (client *OIDCClient) ExchangeCode(ctx context.Context, code string, codeVerifier string, expectedNonce string) (OIDCExchangeResult, error) {
	if !client.Enabled() {
		return OIDCExchangeResult{}, errors.New("oidc is disabled")
	}
	ctx = client.clientContext(ctx)
	oauthConfig, verifier, err := client.loadProvider(ctx)
	if err != nil {
		return OIDCExchangeResult{}, err
	}

	token, err := oauthConfig.Exchange(ctx, strings.TrimSpace(code), oauth2.VerifierOption(strings.TrimSpace(codeVerifier)))
	if err != nil {
		return OIDCExchangeResult{}, fmt.Errorf("exchange oidc authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return OIDCExchangeResult{}, errors.New("oidc token response is missing id_token")
	}

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return OIDCExchangeResult{}, fmt.Errorf("verify oidc id_token: %w", err)
	}
	if strings.TrimSpace(idToken.Nonce) != strings.TrimSpace(expectedNonce) {
		return OIDCExchangeResult{}, errors.New("oidc nonce mismatch")
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		IssuedAt      int64  `json:"iat"`
		AuthTime      int64  `json:"auth_time"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return OIDCExchangeResult{}, fmt.Errorf("decode oidc id_token claims: %w", err)
	}

	var issuedAt time.Time
	if claims.IssuedAt > 0 {
		issuedAt = time.Unix(claims.IssuedAt, 0).UTC()
	}
	var authTime time.Time
	if claims.AuthTime > 0 {
		authTime = time.Unix(claims.AuthTime, 0).UTC()
	}

	return OIDCExchangeResult{
		Claims: OIDCClaims{
			Issuer:        strings.TrimSpace(idToken.Issuer),
			Subject:       strings.TrimSpace(idToken.Subject),
			Email:         strings.TrimSpace(claims.Email),
			EmailVerified: claims.EmailVerified,
			IssuedAt:      issuedAt,
			AuthTime:      authTime,
		},
		Session: OIDCSession{
			EndSessionEndpoint: client.metadata.EndSessionEndpoint,
			IDTokenHint:        strings.TrimSpace(rawIDToken),
		},
	}, nil
}

func (client *OIDCClient) clientContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil || client.httpClient == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, client.httpClient)
}

func (client *OIDCClient) loadProvider(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.oauthConfig != nil && client.verifier != nil {
		return client.oauthConfig, client.verifier, nil
	}

	provider, err := oidc.NewProvider(ctx, client.config.IssuerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("discover oidc provider: %w", err)
	}

	// The sanitizer runs whether or not Claims reports a decode error:
	// encoding/json keeps filling the remaining fields after a type error, and a
	// field that already decoded keeps the value it decoded — a document
	// repeating end_session_endpoint as a cross-origin string and then as a
	// number errors while leaving that string in place. So a decode error is not
	// a reason to skip the pins; it is the case that most needs them.
	metadata := oidcProviderMetadata{}
	_ = provider.Claims(&metadata)
	metadata.EndSessionEndpoint = sanitizeOIDCEndSessionEndpoint(metadata.EndSessionEndpoint, client.config.IssuerURL)

	// Pin the discovery-supplied jwks_uri to the issuer origin, mirroring the
	// end_session_endpoint host-pin above. go-oidc fetches the verification keys
	// from this URL on the first ID-token verification; refusing a cross-origin
	// jwks_uri here stops a malicious or compromised discovery document from
	// steering that server-side key fetch at an internal or attacker host (SSRF)
	// before any verifier is built. Same-origin jwks_uri (the self-hosted norm:
	// Keycloak / authentik / Authelia) passes unchanged.
	if err := validateDiscoveredJWKSURI(metadata.JWKSURI, client.config.IssuerURL); err != nil {
		return nil, nil, err
	}

	// Pin the discovery-supplied token_endpoint the same way. The code
	// exchange POSTs the client secret and authorization code to this URL
	// server-side, so a malicious or compromised discovery document could
	// otherwise exfiltrate both to an attacker host or steer the request at
	// internal infrastructure (SSRF).
	if err := validateDiscoveredTokenEndpoint(provider.Endpoint().TokenURL, client.config.IssuerURL); err != nil {
		return nil, nil, err
	}

	// Pin the discovery-supplied authorization_endpoint the same way. It is the
	// browser hop rather than a server-side fetch, and it carries state, nonce,
	// client_id and redirect_uri: a discovery document naming a foreign origin
	// here would send the owner to a look-alike sign-in page the issuer never
	// served, and one naming a host that dials this machine would send the
	// owner's browser to whatever listens locally on that port.
	if err := validateDiscoveredAuthorizationEndpoint(provider.Endpoint().AuthURL, client.config.IssuerURL); err != nil {
		return nil, nil, err
	}

	client.provider = provider
	client.metadata = metadata
	client.oauthConfig = &oauth2.Config{
		ClientID:     client.config.ClientID,
		ClientSecret: client.config.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  client.config.RedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	client.verifier = provider.Verifier(&oidc.Config{
		ClientID:             client.config.ClientID,
		SupportedSigningAlgs: oidcSupportedSigningAlgs(),
	})

	return client.oauthConfig, client.verifier, nil
}

// oidcSupportedSigningAlgs returns the asymmetric JWS algorithms Ovumcy
// accepts on ID tokens. Symmetric algorithms (HS*) and "none" are excluded so
// a malicious or downgraded provider cannot trick the verifier into accepting
// a token signed with a known-public RSA/EC JWKS key as if it were a shared
// HMAC secret (algorithm confusion).
func oidcSupportedSigningAlgs() []string {
	return []string{
		oidc.RS256,
		oidc.RS384,
		oidc.RS512,
		oidc.ES256,
		oidc.ES384,
		oidc.ES512,
		oidc.PS256,
		oidc.PS384,
		oidc.PS512,
		oidc.EdDSA,
	}
}

func sanitizeOIDCConfig(config OIDCConfig) OIDCConfig {
	config.IssuerURL = strings.TrimSpace(config.IssuerURL)
	config.ClientID = strings.TrimSpace(config.ClientID)
	config.ClientSecret = strings.TrimSpace(config.ClientSecret)
	config.RedirectURL = strings.TrimSpace(config.RedirectURL)
	config.CAFile = strings.TrimSpace(config.CAFile)
	config.LoginMode = OIDCLoginMode(strings.ToLower(strings.TrimSpace(string(config.LoginMode))))
	if config.LoginMode == "" {
		config.LoginMode = OIDCLoginModeHybrid
	}
	config.LogoutMode = OIDCLogoutMode(strings.ToLower(strings.TrimSpace(string(config.LogoutMode))))
	if config.LogoutMode == "" {
		config.LogoutMode = OIDCLogoutModeLocal
	}
	config.ResponseMode = OIDCResponseMode(strings.ToLower(strings.TrimSpace(string(config.ResponseMode))))
	if config.ResponseMode == "" {
		config.ResponseMode = OIDCResponseModeFormPost
	}
	config.PostLogoutRedirectURL = strings.TrimSpace(config.PostLogoutRedirectURL)
	config.AutoProvisionAllowedDomains = sanitizeProvisioningDomains(config.AutoProvisionAllowedDomains)
	return config
}

func sanitizeProvisioningDomains(rawDomains []string) []string {
	if len(rawDomains) == 0 {
		return nil
	}

	result := make([]string, 0, len(rawDomains))
	seen := make(map[string]struct{}, len(rawDomains))
	for _, rawDomain := range rawDomains {
		domain := normalizedProvisioningDomain(rawDomain)
		if domain == "" {
			continue
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		result = append(result, domain)
	}
	return result
}

func normalizedProvisioningDomain(rawDomain string) string {
	domain := strings.ToLower(strings.TrimSpace(rawDomain))
	domain = strings.TrimPrefix(domain, "@")
	return strings.TrimSpace(domain)
}

func isValidProvisioningDomain(rawDomain string) bool {
	domain := normalizedProvisioningDomain(rawDomain)
	if domain == "" || strings.Contains(domain, " ") || !strings.Contains(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	return true
}

// sanitizeOIDCEndSessionEndpoint validates a provider-supplied
// `end_session_endpoint` from discovery metadata. The endpoint MUST be:
//   - absolute, HTTPS, no fragment;
//   - same origin (scheme + host + effective port) as the configured
//     issuer URL.
//
// The same-origin pin is critical: without it, a malicious or compromised
// discovery document could redirect the user's logout flow (including any
// `id_token_hint` carried in the URL) to an attacker-controlled host. The
// rest of the OIDC code assumes that whatever survives this function comes
// from the same authority that issued the ID token.
//
// When issuerURL is empty (constant-time discovery / tests / legacy callers
// that have no issuer to pin against), the function enforces the shape alone —
// HTTPS, no fragment, and a host that does not dial this machine — and returns
// the endpoint unchanged.
func sanitizeOIDCEndSessionEndpoint(rawEndpoint string, issuerURL string) string {
	endpoint := strings.TrimSpace(rawEndpoint)
	if endpoint == "" {
		return ""
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Fragment != "" || HostDialsThisMachine(parsed.Hostname()) {
		return ""
	}

	issuer := strings.TrimSpace(issuerURL)
	if issuer == "" {
		return parsed.String()
	}
	parsedIssuer, err := url.Parse(issuer)
	if err != nil || !parsedIssuer.IsAbs() {
		return ""
	}
	if !sameOriginURL(parsed, parsedIssuer) {
		return ""
	}
	return parsed.String()
}

// validateDiscoveredJWKSURI pins the discovery-supplied jwks_uri to the issuer
// origin. An empty jwks_uri is left for go-oidc to reject during verification
// (there are no keys to fetch); a non-empty one must be an absolute https URL on
// the same origin (scheme + host + effective port) as the configured issuer.
// This is the SSRF companion to sanitizeOIDCEndSessionEndpoint.
func validateDiscoveredJWKSURI(jwksURI string, issuerURL string) error {
	uri := strings.TrimSpace(jwksURI)
	if uri == "" {
		return nil
	}
	parsed, err := url.Parse(uri)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("oidc jwks_uri must be an absolute https URL")
	}
	parsedIssuer, err := url.Parse(strings.TrimSpace(issuerURL))
	if err != nil || !parsedIssuer.IsAbs() {
		return errors.New("oidc issuer URL is invalid")
	}
	if !sameOriginURL(parsed, parsedIssuer) {
		return errors.New("oidc jwks_uri origin must match the issuer origin")
	}
	return nil
}

// validateDiscoveredTokenEndpoint pins the discovery-supplied token_endpoint
// to the issuer origin. An empty endpoint is left for the oauth2 exchange to
// reject (there is nowhere to send the code); a non-empty one must be an
// absolute https URL on the same origin (scheme + host + effective port) as
// the configured issuer. This is the SSRF companion to
// validateDiscoveredJWKSURI for the server-side POST that carries the client
// secret and authorization code.
func validateDiscoveredTokenEndpoint(tokenEndpoint string, issuerURL string) error {
	endpoint := strings.TrimSpace(tokenEndpoint)
	if endpoint == "" {
		return nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("oidc token_endpoint must be an absolute https URL")
	}
	parsedIssuer, err := url.Parse(strings.TrimSpace(issuerURL))
	if err != nil || !parsedIssuer.IsAbs() {
		return errors.New("oidc issuer URL is invalid")
	}
	if !sameOriginURL(parsed, parsedIssuer) {
		return errors.New("oidc token_endpoint origin must match the issuer origin")
	}
	return nil
}

// validateDiscoveredAuthorizationEndpoint pins the discovery-supplied
// authorization_endpoint to the issuer origin, exactly as jwks_uri and
// token_endpoint are pinned: an absolute https URL on the same origin (scheme +
// host + effective port) as the configured issuer, whose host does not dial
// this machine. An empty endpoint is refused here rather than deferred to the
// flow, unlike the siblings above: oauth2.Config.AuthCodeURL never validates
// Endpoint.AuthURL, so an empty one composes the relative
// "?client_id=…&state=…" and sends the owner back into ovumcy carrying state
// and nonce instead of failing the sign-in.
func validateDiscoveredAuthorizationEndpoint(authorizationEndpoint string, issuerURL string) error {
	endpoint := strings.TrimSpace(authorizationEndpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("oidc authorization_endpoint must be an absolute https URL")
	}
	if HostDialsThisMachine(parsed.Hostname()) {
		return errors.New("oidc authorization_endpoint must name a remote host")
	}
	parsedIssuer, err := url.Parse(strings.TrimSpace(issuerURL))
	if err != nil || !parsedIssuer.IsAbs() {
		return errors.New("oidc issuer URL is invalid")
	}
	if !sameOriginURL(parsed, parsedIssuer) {
		return errors.New("oidc authorization_endpoint origin must match the issuer origin")
	}
	return nil
}

// OnIssuerOrigin reports whether endpoint sits on the configured issuer's
// origin (scheme + host + effective port) — the same comparison every
// discovered endpoint is pinned with. It exists for state read back from
// storage after discovery has already run, such as the provider-logout
// end-session URL. An issuer that does not parse as an absolute URL pins
// nothing, so no endpoint is on its origin.
func OnIssuerOrigin(endpoint *url.URL, issuerURL string) bool {
	parsedIssuer, err := url.Parse(strings.TrimSpace(issuerURL))
	if err != nil || !parsedIssuer.IsAbs() {
		return false
	}
	return sameOriginURL(endpoint, parsedIssuer)
}

func validateOIDCCABundle(path string) error {
	content, err := readOIDCCABundle(path)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(content); !ok {
		return errors.New("OIDC_CA_FILE must contain at least one PEM certificate")
	}
	return nil
}

func readOIDCCABundle(path string) ([]byte, error) {
	return ReadBoundedRegularFile(path, "OIDC_CA_FILE", maxOIDCCABundleBytes)
}

func newOIDCHTTPClient(config OIDCConfig) *http.Client {
	transport, _ := http.DefaultTransport.(*http.Transport)
	if transport == nil {
		transport = &http.Transport{}
	} else {
		transport = transport.Clone()
	}

	if config.CAFile != "" {
		if bundle, err := readOIDCCABundle(config.CAFile); err == nil {
			roots, poolErr := x509.SystemCertPool()
			if poolErr != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if roots.AppendCertsFromPEM(bundle) {
				transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
			}
		}
	}
	transport.MaxResponseHeaderBytes = oidcResponseHeaderLimit

	return &http.Client{
		Timeout:       defaultOIDCHTTPTimeout,
		Transport:     &oidcBoundedBodyTransport{base: transport, limit: oidcResponseBodyLimit},
		CheckRedirect: oidcRedirectPolicy(config.IssuerURL),
	}
}

// oidcRedirectPolicy is the CheckRedirect policy for the OIDC HTTP client.
// Every outbound OIDC request (discovery, JWKS fetch, code exchange) starts
// at a URL that is origin-pinned to the configured issuer; this extends the
// same pin to HTTP redirects, so a redirecting response cannot steer a
// request — or the client secret and authorization code the exchange
// carries — off the issuer origin after the initial URL validation passed
// (SSRF via redirect). Same-origin redirects (an IdP normalizing its
// discovery path) follow normally; the stdlib ten-hop cap is re-applied
// because installing a custom CheckRedirect replaces the default that
// enforced it.
func oidcRedirectPolicy(issuerURL string) func(req *http.Request, via []*http.Request) error {
	parsedIssuer, err := url.Parse(strings.TrimSpace(issuerURL))
	issuerPinnable := err == nil && parsedIssuer.IsAbs()
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("oidc http request stopped after 10 redirects")
		}
		if !issuerPinnable {
			return errors.New("oidc http redirect refused: issuer URL is not pinnable")
		}
		if !sameOriginURL(req.URL, parsedIssuer) {
			return errors.New("oidc http redirect left the issuer origin")
		}
		return nil
	}
}

// HostDialsThisMachine reports whether a URL host resolves to the machine doing
// the dialing rather than to a named peer: an empty host (`https://:8443`) and
// the unspecified addresses `0.0.0.0` / `[::]` all do. An OIDC endpoint of that
// shape parses as a valid absolute https URL, so nothing but this check stops
// the client secret and authorization code from being posted to whatever
// listens locally on that port. Loopback literals are deliberately not included:
// a self-hosted issuer on 127.0.0.1 is a supported deployment, as is one on a
// LAN address — so this is NOT an SSRF egress gate and must not be reused as
// one. The gate that refuses private and link-local destinations outright is
// `internal/services/webhook_delivery.go`.
//
// A host holding any non-ASCII byte is refused as well, because what it dials
// is decided by a mapping this check does not run: Go's HTTP transport passes
// the host through IDNA lookup mapping before dialing, and so does a browser, so
// the full-width `０.０.０.０` is dialed as `0.0.0.0`. An internationalized
// deployment spells its host in the ASCII (`xn--`) form, which is what those
// mappings produce anyway.
func HostDialsThisMachine(host string) bool {
	if host == "" {
		return true
	}
	if hostHasNonASCII(host) {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		// Go parses only canonical dotted-quad literals, but a resolver with
		// inet_aton semantics reads `0`, `0.1`, `00.0.0.0` and `0x0` as addresses
		// in 0.0.0.0/8 — the very block below. No registered hostname is written
		// that way (a top-level label is never a number), so refusing every
		// numeric spelling Go cannot parse costs no deployment and leaves no
		// spelling of this block for a platform resolver to accept behind the
		// check.
		return isNumericAddressSpelling(host)
	}
	// A zone identifier (`https://[::%25eth0]`) is not part of the address the
	// dialer resolves, and an IPv4-mapped form is the same address wearing a v6
	// shape: strip both before classifying, or either spelling walks past this.
	address = address.WithZone("").Unmap()
	if address.IsUnspecified() {
		return true
	}
	// Every IPv4 address whose first octet is zero — RFC 1122 "this network",
	// 0.0.0.0/8 — is refused, not only the 0.0.0.0 that IsUnspecified matches.
	// Only 0.0.0.0 itself is the address a connect() is known to map to the
	// local host; the rest of the block is refused because it is not a valid
	// destination for any peer, so no working issuer names one and no spelling
	// of the block is left to a platform stack's handling of it. The webhook
	// egress gate refuses the same prefix (`internal/services/webhook_delivery.go`).
	return address.Is4() && address.As4()[0] == 0
}

// isNumericAddressSpelling reports whether every dot-separated label of a host
// is a number in one of the bases inet_aton accepts — decimal, octal (a leading
// zero) or hex (`0x`) — so the host is an address in some spelling, canonical
// or not, and never a hostname. `192.168.001.010` lands here too: its
// octal-looking labels name a different address under inet_aton than they
// appear to, and an ambiguous numeric spelling is refused rather than guessed.
// The caller has already classified the empty host.
func isNumericAddressSpelling(host string) bool {
	for _, label := range strings.Split(host, ".") {
		digits, base := label, "0123456789"
		if len(label) >= 2 && label[0] == '0' && (label[1] == 'x' || label[1] == 'X') {
			digits, base = label[2:], "0123456789abcdefABCDEF"
		}
		for _, char := range digits {
			if !strings.ContainsRune(base, char) {
				return false
			}
		}
	}
	return true
}

func sameOriginURL(left *url.URL, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	// Two hosts that both dial this machine are not the same origin: an issuer
	// and an endpoint that both name no host would otherwise pin to each other.
	if HostDialsThisMachine(left.Hostname()) || HostDialsThisMachine(right.Hostname()) {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(strings.TrimSpace(value.Scheme)) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}
