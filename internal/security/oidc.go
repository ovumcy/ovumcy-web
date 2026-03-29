package security

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

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
	PostLogoutRedirectURL       string
	AutoProvisionAllowedDomains []string
}

type OIDCClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
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
	if !cookieSecure {
		return errors.New("OIDC_ENABLED=true requires COOKIE_SECURE=true")
	}
	if config.LoginMode != OIDCLoginModeHybrid && config.LoginMode != OIDCLoginModeOIDCOnly {
		return errors.New("OIDC_LOGIN_MODE must be hybrid or oidc_only")
	}
	if config.LogoutMode != OIDCLogoutModeLocal && config.LogoutMode != OIDCLogoutModeProvider && config.LogoutMode != OIDCLogoutModeAuto {
		return errors.New("OIDC_LOGOUT_MODE must be local, provider, or auto")
	}
	if config.AutoProvision {
		if !registrationOpen {
			return errors.New("OIDC_AUTO_PROVISION=true requires REGISTRATION_MODE=open")
		}
	}
	if config.IssuerURL == "" {
		return errors.New("OIDC_ISSUER_URL is required when OIDC_ENABLED=true")
	}
	if config.ClientID == "" {
		return errors.New("OIDC_CLIENT_ID is required when OIDC_ENABLED=true")
	}
	if config.ClientSecret == "" {
		return errors.New("OIDC_CLIENT_SECRET is required when OIDC_ENABLED=true")
	}
	if config.RedirectURL == "" {
		return errors.New("OIDC_REDIRECT_URL is required when OIDC_ENABLED=true")
	}

	issuerURL, err := url.Parse(config.IssuerURL)
	if err != nil || !issuerURL.IsAbs() {
		return errors.New("OIDC_ISSUER_URL must be an absolute URL")
	}
	if !strings.EqualFold(issuerURL.Scheme, "https") {
		return errors.New("OIDC_ISSUER_URL must use https")
	}
	if issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return errors.New("OIDC_ISSUER_URL must not include query or fragment")
	}

	redirectURL, err := url.Parse(config.RedirectURL)
	if err != nil || !redirectURL.IsAbs() {
		return errors.New("OIDC_REDIRECT_URL must be an absolute URL")
	}
	if !strings.EqualFold(redirectURL.Scheme, "https") {
		return errors.New("OIDC_REDIRECT_URL must use https")
	}
	if redirectURL.RawQuery != "" || redirectURL.Fragment != "" {
		return errors.New("OIDC_REDIRECT_URL must not include query or fragment")
	}
	if path.Clean(strings.TrimSpace(redirectURL.Path)) != OIDCCallbackPath {
		return fmt.Errorf("OIDC_REDIRECT_URL path must be %s", OIDCCallbackPath)
	}
	if config.PostLogoutRedirectURL != "" {
		postLogoutURL, err := url.Parse(config.PostLogoutRedirectURL)
		if err != nil || !postLogoutURL.IsAbs() {
			return errors.New("OIDC_POST_LOGOUT_REDIRECT_URL must be an absolute URL")
		}
		if !strings.EqualFold(postLogoutURL.Scheme, "https") {
			return errors.New("OIDC_POST_LOGOUT_REDIRECT_URL must use https")
		}
		if postLogoutURL.RawQuery != "" || postLogoutURL.Fragment != "" {
			return errors.New("OIDC_POST_LOGOUT_REDIRECT_URL must not include query or fragment")
		}
		if !sameOriginURL(redirectURL, postLogoutURL) {
			return errors.New("OIDC_POST_LOGOUT_REDIRECT_URL must match the OIDC redirect origin")
		}
	}
	if config.CAFile != "" {
		if err := validateOIDCCABundle(config.CAFile); err != nil {
			return err
		}
	}
	for _, domain := range config.AutoProvisionAllowedDomains {
		if !isValidProvisioningDomain(domain) {
			return fmt.Errorf("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS contains invalid domain %q", domain)
		}
	}

	return nil
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

func (client *OIDCClient) AuthCodeURL(ctx context.Context, state string, nonce string, codeVerifier string) (string, error) {
	if !client.Enabled() {
		return "", errors.New("oidc is disabled")
	}
	oauthConfig, _, err := client.loadProvider(client.clientContext(ctx))
	if err != nil {
		return "", err
	}
	return oauthConfig.AuthCodeURL(
		strings.TrimSpace(state),
		oidc.Nonce(strings.TrimSpace(nonce)),
		oauth2.S256ChallengeOption(strings.TrimSpace(codeVerifier)),
		oauth2.SetAuthURLParam("response_mode", "form_post"),
	), nil
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
	}
	if err := idToken.Claims(&claims); err != nil {
		return OIDCExchangeResult{}, fmt.Errorf("decode oidc id_token claims: %w", err)
	}

	return OIDCExchangeResult{
		Claims: OIDCClaims{
			Issuer:        strings.TrimSpace(idToken.Issuer),
			Subject:       strings.TrimSpace(idToken.Subject),
			Email:         strings.TrimSpace(claims.Email),
			EmailVerified: claims.EmailVerified,
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

	metadata := oidcProviderMetadata{}
	if claimsErr := provider.Claims(&metadata); claimsErr == nil {
		metadata.EndSessionEndpoint = sanitizeOIDCEndSessionEndpoint(metadata.EndSessionEndpoint)
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
		ClientID: client.config.ClientID,
	})

	return client.oauthConfig, client.verifier, nil
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

func sanitizeOIDCEndSessionEndpoint(rawEndpoint string) string {
	endpoint := strings.TrimSpace(rawEndpoint)
	if endpoint == "" {
		return ""
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Fragment != "" {
		return ""
	}
	return parsed.String()
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

	return &http.Client{
		Timeout:   defaultOIDCHTTPTimeout,
		Transport: transport,
	}
}

func sameOriginURL(left *url.URL, right *url.URL) bool {
	if left == nil || right == nil {
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
