package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/cli"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

type runtimeConfig struct {
	Location         *time.Location
	SecretKey        string
	DatabaseConfig   db.Config
	Port             string
	DefaultLanguage  string
	RegistrationMode services.RegistrationMode
	CookieSecure     bool
	OIDC             security.OIDCConfig
	RateLimits       rateLimitSettings
	Proxy            proxySettings
	AuditLogEnabled  bool
}

type rateLimitSettings struct {
	LoginMax             int
	LoginWindow          time.Duration
	ForgotPasswordMax    int
	ForgotPasswordWindow time.Duration
	RegisterMax          int
	RegisterWindow       time.Duration
	LogoutMax            int
	LogoutWindow         time.Duration
	APIMax               int
	APIWindow            time.Duration
}

type proxySettings struct {
	Enabled        bool
	Header         string
	TrustedProxies []string
}

const (
	headerXContentTypeOptions     = "X-Content-Type-Options"
	headerReferrerPolicy          = "Referrer-Policy"
	headerPermissionsPolicy       = "Permissions-Policy"
	headerCrossOriginOpenerPolicy = "Cross-Origin-Opener-Policy"
	headerXFrameOptions           = "X-Frame-Options"
	headerContentSecurityPolicy   = "Content-Security-Policy"
	headerStrictTransportSecurity = "Strict-Transport-Security"

	xContentTypeOptionsNoSniff           = "nosniff"
	referrerPolicyStrictOrigin           = "strict-origin-when-cross-origin"
	permissionsPolicyDefault             = "geolocation=(), camera=(), microphone=(), accelerometer=(), gyroscope=(), payment=(), usb=(), interest-cohort=(), ambient-light-sensor=()"
	crossOriginOpenerPolicyDefault       = "same-origin"
	xFrameOptionsDeny                    = "DENY"
	contentSecurityPolicyDefault         = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; worker-src 'none'"
	strictTransportSecurityDefault       = "max-age=31536000; includeSubDomains"
	maxSecretKeyFileBytes          int64 = 8 << 10
)

func main() {
	handled, err := tryRunCLICommand()
	if err != nil {
		log.Fatal(err)
	}
	if handled {
		return
	}

	location := mustLoadLocation(getEnv("TZ", "Local"))
	time.Local = location

	config := mustLoadRuntimeConfig(location)
	api.SetAuditLogEnabled(config.AuditLogEnabled)
	database := mustOpenDatabase(config.DatabaseConfig)
	i18nManager := mustNewI18nManager(config.DefaultLanguage)
	// codecov:ignore:start -- main() composition-root wiring; runs only in the binary (exercised by e2e). The shared bootstrap.BuildDependencies is unit-tested through the internal/api test helper.
	dependencies := bootstrap.BuildDependencies(db.NewRepositories(database), []byte(config.SecretKey), i18nManager, bootstrap.Options{
		RegistrationMode: config.RegistrationMode,
		OIDCConfig:       config.OIDC,
		LoginAttempts:    bootstrap.AttemptLimit{Max: config.RateLimits.LoginMax, Window: config.RateLimits.LoginWindow},
		RecoveryAttempts: bootstrap.AttemptLimit{Max: config.RateLimits.ForgotPasswordMax, Window: config.RateLimits.ForgotPasswordWindow},
		LogoutAttempts:   &bootstrap.AttemptLimit{Max: config.RateLimits.LogoutMax, Window: config.RateLimits.LogoutWindow},
	})
	// codecov:ignore:end
	handler := mustNewHandler(config, i18nManager, dependencies)
	app := newFiberApp(config, handler)
	stopSignals := installGracefulShutdown(app)
	defer stopSignals()

	logStartup(config)
	if err := app.Listen(":" + config.Port); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func mustLoadRuntimeConfig(location *time.Location) runtimeConfig {
	config, err := loadRuntimeConfig(location)
	if err != nil {
		log.Fatal(err)
	}
	return config
}

func loadRuntimeConfig(location *time.Location) (runtimeConfig, error) {
	secretKey, err := resolveSecretKey()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid SECRET_KEY: %w", err)
	}

	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid database config: %w", err)
	}

	port, err := resolvePort()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid PORT: %w", err)
	}

	proxy, err := resolveProxySettings()
	if err != nil {
		return runtimeConfig{}, err
	}

	registrationMode, err := resolveRegistrationMode()
	if err != nil {
		return runtimeConfig{}, err
	}

	cookieSecure := getEnvBool("COOKIE_SECURE", false)
	oidcConfig, err := resolveOIDCConfig(cookieSecure, registrationMode)
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Location:         location,
		SecretKey:        secretKey,
		DatabaseConfig:   databaseConfig,
		Port:             port,
		DefaultLanguage:  getEnv("DEFAULT_LANGUAGE", "en"),
		RegistrationMode: registrationMode,
		CookieSecure:     cookieSecure,
		OIDC:             oidcConfig,
		RateLimits: rateLimitSettings{
			LoginMax:             getEnvInt("RATE_LIMIT_LOGIN_MAX", 8),
			LoginWindow:          getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", 15*time.Minute),
			RegisterMax:          getEnvInt("RATE_LIMIT_REGISTER_MAX", 8),
			RegisterWindow:       getEnvDuration("RATE_LIMIT_REGISTER_WINDOW", 15*time.Minute),
			ForgotPasswordMax:    getEnvInt("RATE_LIMIT_FORGOT_PASSWORD_MAX", 8),
			ForgotPasswordWindow: getEnvDuration("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", time.Hour),
			LogoutMax:            getEnvInt("RATE_LIMIT_LOGOUT_MAX", 60),
			LogoutWindow:         getEnvDuration("RATE_LIMIT_LOGOUT_WINDOW", 15*time.Minute),
			APIMax:               getEnvInt("RATE_LIMIT_API_MAX", 300),
			APIWindow:            getEnvDuration("RATE_LIMIT_API_WINDOW", time.Minute),
		},
		Proxy:           proxy,
		AuditLogEnabled: getEnvBool("AUDIT_LOG_ENABLED", false),
	}, nil
}

func resolveRegistrationMode() (services.RegistrationMode, error) {
	mode, err := services.ParseRegistrationMode(getEnv("REGISTRATION_MODE", string(services.RegistrationModeOpen)))
	if err != nil {
		return "", err
	}
	return mode, nil
}

func resolveOIDCConfig(cookieSecure bool, registrationMode services.RegistrationMode) (security.OIDCConfig, error) {
	config := security.OIDCConfig{
		Enabled:                     getEnvBool("OIDC_ENABLED", false),
		IssuerURL:                   getEnv("OIDC_ISSUER_URL", ""),
		ClientID:                    getEnv("OIDC_CLIENT_ID", ""),
		ClientSecret:                getEnv("OIDC_CLIENT_SECRET", ""),
		RedirectURL:                 getEnv("OIDC_REDIRECT_URL", ""),
		CAFile:                      getEnv("OIDC_CA_FILE", ""),
		AutoProvision:               getEnvBool("OIDC_AUTO_PROVISION", false),
		LoginMode:                   security.OIDCLoginMode(getEnv("OIDC_LOGIN_MODE", string(security.OIDCLoginModeHybrid))),
		LogoutMode:                  security.OIDCLogoutMode(getEnv("OIDC_LOGOUT_MODE", string(security.OIDCLogoutModeLocal))),
		PostLogoutRedirectURL:       getEnv("OIDC_POST_LOGOUT_REDIRECT_URL", ""),
		AutoProvisionAllowedDomains: parseCSV(getEnv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "")),
	}
	if err := config.Validate(cookieSecure, registrationMode == services.RegistrationModeOpen); err != nil {
		return security.OIDCConfig{}, err
	}
	return config, nil
}

func resolveProxySettings() (proxySettings, error) {
	settings := proxySettings{
		Enabled:        getEnvBool("TRUST_PROXY_ENABLED", false),
		Header:         strings.TrimSpace(getEnv("PROXY_HEADER", fiber.HeaderXForwardedFor)),
		TrustedProxies: parseCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1")),
	}

	if !settings.Enabled {
		return settings, nil
	}
	if settings.Header == "" {
		settings.Header = fiber.HeaderXForwardedFor
	}
	if len(settings.TrustedProxies) == 0 {
		return proxySettings{}, fmt.Errorf("TRUST_PROXY_ENABLED=true requires at least one TRUSTED_PROXIES entry")
	}
	return settings, nil
}

func mustOpenDatabase(databaseConfig db.Config) *gorm.DB {
	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	return database
}

func mustNewI18nManager(defaultLanguage string) *i18n.Manager {
	i18nManager, err := i18n.NewManager(defaultLanguage, filepath.Join("internal", "i18n", "locales"))
	if err != nil {
		log.Fatalf("i18n init failed: %v", err)
	}
	return i18nManager
}

func mustNewHandler(config runtimeConfig, i18nManager *i18n.Manager, dependencies api.Dependencies) *api.Handler {
	handler, err := api.NewHandler(config.SecretKey, filepath.Join("internal", "templates"), config.Location, i18nManager, config.CookieSecure, dependencies)
	if err != nil {
		log.Fatalf("handler init failed: %v", err)
	}
	return handler
}

func newFiberApp(config runtimeConfig, handler *api.Handler) *fiber.App {
	app := fiber.New(fiberConfig(config.Proxy))
	configureFiberMiddleware(app, config, handler)
	registerStaticContentTypes()
	app.Static("/static", filepath.Join("web", "static"))
	api.RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app
}

func registerStaticContentTypes() {
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Printf("register .webmanifest MIME type: %v", err)
	}
}

func fiberConfig(proxy proxySettings) fiber.Config {
	appConfig := fiber.Config{
		AppName:               "Ovumcy",
		DisableStartupMessage: true,
		ErrorHandler:          ovumcyErrorHandler,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          60 * time.Second,
		IdleTimeout:           120 * time.Second,
	}
	if !proxy.Enabled {
		return appConfig
	}
	appConfig.ProxyHeader = proxy.Header
	appConfig.EnableTrustedProxyCheck = true
	appConfig.EnableIPValidation = true
	appConfig.TrustedProxies = proxy.TrustedProxies
	return appConfig
}

// trustedProxyMatcher classifies an address as a trusted proxy using the same
// rules fiber applies when building its own trusted set (App.handleTrustedProxy):
// a TRUSTED_PROXIES entry containing "/" is parsed as a CIDR range, anything
// else is matched by exact net.IP string. Keeping our own copy lets the rate-
// limit key generator reuse fiber's exact trust boundary, so an address we treat
// as "a proxy hop" is one fiber would also have trusted as the peer.
type trustedProxyMatcher struct {
	exact  map[string]struct{}
	ranges []*net.IPNet
}

func newTrustedProxyMatcher(entries []string) trustedProxyMatcher {
	matcher := trustedProxyMatcher{exact: make(map[string]struct{}, len(entries))}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, ipNet, err := net.ParseCIDR(entry); err == nil {
				matcher.ranges = append(matcher.ranges, ipNet)
			}
			continue
		}
		matcher.exact[entry] = struct{}{}
	}
	return matcher
}

func (m trustedProxyMatcher) contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if _, ok := m.exact[ip.String()]; ok {
		return true
	}
	for _, ipNet := range m.ranges {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// rightmostUntrustedIP walks an X-Forwarded-For chain from the right (the hop
// closest to us, appended by our own trusted proxy) toward the left and returns
// the first address that is not itself a trusted proxy: the real client as seen
// from the edge of our trusted chain. Entries further left are client-supplied
// and spoofable, so they are ignored. Returns "" when every hop is trusted or
// the list is empty, letting the caller fall back to the direct peer.
func rightmostUntrustedIP(forwarded []string, trusted trustedProxyMatcher) string {
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(forwarded[i]))
		if ip != nil && !trusted.contains(ip) {
			return ip.String()
		}
	}
	return ""
}

// rateLimitKeyGenerator builds the per-request key for fiber's rate limiters.
//
// Fiber's default key is c.IP(). With ProxyHeader=X-Forwarded-For and a trusted
// peer, c.IP() returns the LEFTMOST X-Forwarded-For token — the value the
// original client supplied — so an attacker behind an appending proxy can rotate
// that token to mint a fresh rate-limit bucket per request and defeat the limit
// entirely. These edge limiters key on IP alone (unlike the per-identity auth
// limiters in internal/services, which also bucket on email/user id), so the IP
// key must be attacker-proof on its own.
//
// The key is derived from our trust boundary outward:
//   - Proxy support off, or the direct peer is not a trusted proxy: the only
//     attacker-proof address is the socket peer, so key on it and ignore every
//     forwarded header (all client-controlled in that position).
//   - Peer is a trusted proxy and the header is X-Forwarded-For: key on the
//     rightmost untrusted hop (see rightmostUntrustedIP); fall back to the peer
//     when every hop is trusted.
//   - Peer is a trusted proxy and the header is a single-value header the proxy
//     overwrites (for example X-Real-IP): defer to fiber's parsed c.IP().
func rateLimitKeyGenerator(proxy proxySettings) func(*fiber.Ctx) string {
	trusted := newTrustedProxyMatcher(proxy.TrustedProxies)
	headerIsXForwardedFor := strings.EqualFold(strings.TrimSpace(proxy.Header), fiber.HeaderXForwardedFor)
	return func(c *fiber.Ctx) string {
		peer := c.Context().RemoteIP()
		if !proxy.Enabled || !trusted.contains(peer) {
			return peer.String()
		}
		if !headerIsXForwardedFor {
			return c.IP()
		}
		if client := rightmostUntrustedIP(c.IPs(), trusted); client != "" {
			return client
		}
		return peer.String() // codecov:ignore -- main() IP-resolution fallback; exercised by e2e
	}
}

// ovumcyErrorHandler is the top-level Fiber error handler. It preserves the
// status and message of explicit *fiber.Error values (app-controlled and safe,
// for example the 403 raised by the CSRF middleware) but never forwards a raw
// error or recovered panic value to the client, since those can carry internal
// detail such as table names, file paths, or driver messages.
func ovumcyErrorHandler(c *fiber.Ctx, err error) error {
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return c.Status(fiberErr.Code).SendString(fiberErr.Message)
	}
	return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
}

func configureFiberMiddleware(app *fiber.App, config runtimeConfig, handler *api.Handler) {
	// keyGen resolves the real client IP for every limiter so a spoofed
	// X-Forwarded-For prefix cannot mint fresh per-IP buckets. See
	// rateLimitKeyGenerator for the trust-boundary derivation.
	keyGen := rateLimitKeyGenerator(config.Proxy)
	app.Use(securityHeadersMiddleware(config.CookieSecure))
	app.Use(recover.New())
	app.Use(newRequestLogger(nil))
	app.Use(compress.New())
	app.Use(limiter.New(limiter.Config{
		Next:         rateLimitOnlyFor(fiber.MethodDelete, "/api/v1/sessions/current"),
		Max:          config.RateLimits.LogoutMax,
		Expiration:   config.RateLimits.LogoutWindow,
		KeyGenerator: keyGen,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_logout_attempts",
		}),
	}))
	app.Use(limiter.New(limiter.Config{
		Next:         rateLimitOnlyFor(fiber.MethodPost, "/api/v1/sessions"),
		Max:          config.RateLimits.LoginMax,
		Expiration:   config.RateLimits.LoginWindow,
		KeyGenerator: keyGen,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_login_attempts",
		}),
	}))
	app.Use(limiter.New(limiter.Config{
		Next:         rateLimitOnlyFor(fiber.MethodPost, "/api/v1/users"),
		Max:          config.RateLimits.RegisterMax,
		Expiration:   config.RateLimits.RegisterWindow,
		KeyGenerator: keyGen,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_register_attempts",
		}),
	}))
	app.Use(limiter.New(limiter.Config{
		Next:         rateLimitOnlyFor(fiber.MethodPost, "/api/v1/password-resets"),
		Max:          config.RateLimits.ForgotPasswordMax,
		Expiration:   config.RateLimits.ForgotPasswordWindow,
		KeyGenerator: keyGen,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_forgot_password_attempts",
		}),
	}))
	app.Use("/auth/oidc", limiter.New(limiter.Config{
		Max:          config.RateLimits.LoginMax,
		Expiration:   config.RateLimits.LoginWindow,
		KeyGenerator: keyGen,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_sso_attempts",
		}),
	}))
	app.Use("/api", limiter.New(limiter.Config{
		Max:          config.RateLimits.APIMax,
		Expiration:   config.RateLimits.APIWindow,
		KeyGenerator: keyGen,
		LimitReached: newAPIRateLimitHandler(handler),
	}))
	app.Use(handler.LanguageMiddleware)
	app.Use(csrf.New(csrfMiddlewareConfig(config.CookieSecure)))
}

const requestLoggerFormat = "${time} | ${status} | ${latency} | ${method} | ${request_path} | ${safe_error}\n"

func newRequestLogger(output io.Writer) fiber.Handler {
	config := logger.Config{
		Format: requestLoggerFormat,
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return buffer.WriteString(api.SafeRequestLogPath(c))
			},
			"safe_error": func(buffer logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return buffer.WriteString(api.SafeLogError(data.ChainErr))
			},
		},
	}
	if output != nil {
		config.Output = output
	}
	return logger.New(config)
}

func securityHeadersMiddleware(enableStrictTransportSecurity bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(headerXContentTypeOptions, xContentTypeOptionsNoSniff)
		c.Set(headerReferrerPolicy, referrerPolicyStrictOrigin)
		c.Set(headerPermissionsPolicy, permissionsPolicyDefault)
		c.Set(headerCrossOriginOpenerPolicy, crossOriginOpenerPolicyDefault)
		c.Set(headerXFrameOptions, xFrameOptionsDeny)
		c.Set(headerContentSecurityPolicy, contentSecurityPolicyDefault)
		if enableStrictTransportSecurity {
			c.Set(headerStrictTransportSecurity, strictTransportSecurityDefault)
		}
		if !strings.HasPrefix(c.Path(), "/static") {
			c.Set("Cache-Control", "no-store")
		}
		return c.Next()
	}
}

func installGracefulShutdown(app *fiber.App) context.CancelFunc {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Printf("server shutdown failed: %v", err)
		}
	}()
	return stopSignals
}

func logStartup(config runtimeConfig) {
	log.Printf(
		"Ovumcy listening on http://0.0.0.0:%s (rev: %s, tz: %s, registration=%s, oidc=%t, audit_log=%t, rate_limits: login=%d/%s api=%d/%s, trusted_proxy=%t)",
		config.Port,
		buildRevision(),
		config.Location.String(),
		config.RegistrationMode,
		config.OIDC.Enabled,
		config.AuditLogEnabled,
		config.RateLimits.LoginMax,
		config.RateLimits.LoginWindow,
		config.RateLimits.APIMax,
		config.RateLimits.APIWindow,
		config.Proxy.Enabled,
	)
	if config.Proxy.Enabled {
		log.Printf("trusted proxy config: header=%s trusted_proxy_count=%d", config.Proxy.Header, len(config.Proxy.TrustedProxies))
	}
	if warning := proxyHeaderRateLimitWarning(config.Proxy); warning != "" {
		log.Printf("%s", warning)
	}
	if !config.CookieSecure {
		log.Printf("WARNING: COOKIE_SECURE=false — auth cookies are sent without the Secure flag and can be intercepted over plain HTTP. Set COOKIE_SECURE=true when serving over HTTPS (directly or behind a TLS-terminating proxy).")
	}
	if !config.Proxy.Enabled {
		log.Printf("WARNING: TRUST_PROXY_ENABLED=false — edge rate limiters key on the direct socket peer; behind a reverse proxy every client shares one bucket. Set TRUST_PROXY_ENABLED=true, TRUSTED_PROXIES, and a proxy-overwritten PROXY_HEADER (for example X-Real-IP) when deployed behind a proxy.")
	}
}

// proxyHeaderRateLimitWarning returns a non-empty operator note when trust-proxy
// is enabled but PROXY_HEADER resolves to X-Forwarded-For. The edge rate limiters
// no longer trust that header blindly — rateLimitKeyGenerator keys on the
// rightmost untrusted X-Forwarded-For hop, so a spoofed prefix cannot defeat
// them. The residual is that fiber's c.IP() still returns the client-supplied
// leftmost token; c.IP() only feeds the secondary per-client auth-attempt
// buckets in internal/services, which sit behind spoof-proof per-identity
// buckets, so brute-force protection holds. PROXY_HEADER should still name a
// header the proxy overwrites with the real client IP (for example X-Real-IP)
// for spoof-proof c.IP() values.
func proxyHeaderRateLimitWarning(proxy proxySettings) string {
	if proxy.Enabled && strings.EqualFold(strings.TrimSpace(proxy.Header), fiber.HeaderXForwardedFor) {
		return "NOTE: TRUST_PROXY_ENABLED=true with PROXY_HEADER=X-Forwarded-For — the rate limiters now key on the rightmost untrusted X-Forwarded-For hop and are not spoofable, but fiber's c.IP() still returns the client-supplied leftmost entry. c.IP() only feeds the secondary per-client auth-attempt buckets (the per-identity buckets that actually cap brute force are unaffected). Set PROXY_HEADER to a header your proxy overwrites with the real client IP (for example X-Real-IP) for spoof-proof c.IP() values."
	}
	return ""
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "unknown"
	}

	revision := "unknown"
	modified := "false"
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value)
		}
	}

	if modified == "true" {
		return revision + "-dirty"
	}
	return revision
}

func tryRunCLICommand() (bool, error) {
	return tryRunCLICommandWithHandlers(os.Args[1:], cliCommandHandlers{
		runResetPassword: cli.RunResetPasswordCommand,
		runUsers:         cli.RunUsersCommand,
		runHealthcheck:   cli.RunHealthcheckCommand,
	})
}

type cliCommandHandlers struct {
	runResetPassword func(databaseConfig db.Config, email string) error
	runUsers         func(databaseConfig db.Config, args []string) error
	runHealthcheck   func(port string, timeout time.Duration) error
}

func tryRunCLICommandWithHandlers(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	command := strings.TrimSpace(args[0])
	switch command {
	case "reset-password":
		if len(args) != 2 {
			return true, fmt.Errorf("usage: ovumcy reset-password <email>")
		}
		if handlers.runResetPassword == nil {
			return true, fmt.Errorf("reset-password handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		email := strings.TrimSpace(args[1])
		return true, handlers.runResetPassword(databaseConfig, email)
	case "users":
		if len(args) < 2 {
			return true, fmt.Errorf("usage: ovumcy users <list|delete>")
		}
		if handlers.runUsers == nil {
			return true, fmt.Errorf("users handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		return true, handlers.runUsers(databaseConfig, args[1:])
	case "healthcheck":
		if len(args) != 1 {
			return true, fmt.Errorf("usage: ovumcy healthcheck")
		}
		if handlers.runHealthcheck == nil {
			return true, fmt.Errorf("healthcheck handler is required")
		}
		port, err := resolvePort()
		if err != nil {
			return true, fmt.Errorf("invalid PORT: %w", err)
		}
		return true, handlers.runHealthcheck(port, 0)
	default:
		return false, nil
	}
}

func resolveDatabaseConfig() (db.Config, error) {
	driver := db.Driver(strings.ToLower(strings.TrimSpace(getEnv("DB_DRIVER", string(db.DriverSQLite)))))
	config := db.Config{
		Driver:      driver,
		SQLitePath:  getEnv("DB_PATH", filepath.Join("data", "ovumcy.db")),
		PostgresURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
	}
	if err := config.Validate(); err != nil {
		return db.Config{}, err
	}
	return config, nil
}

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid TZ %q, falling back to UTC", name)
		return time.UTC
	}
	return location
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func resolveSecretKey() (string, error) {
	secret := strings.TrimSpace(os.Getenv("SECRET_KEY"))
	if secret == "" {
		keyFilePath := strings.TrimSpace(os.Getenv("SECRET_KEY_FILE"))
		if keyFilePath != "" {
			content, err := readSecretKeyFile(keyFilePath)
			if err != nil {
				return "", fmt.Errorf("failed to read SECRET_KEY_FILE: %w", err)
			}
			secret = strings.TrimSpace(content)
		}
	}

	if secret == "" {
		return "", fmt.Errorf("SECRET_KEY is required")
	}

	lower := strings.ToLower(secret)
	switch lower {
	case "change_me_in_production", "replace_with_at_least_32_random_characters", "replace_me", "changeme":
		return "", fmt.Errorf("SECRET_KEY cannot use placeholder value %q", secret)
	}
	if len(secret) < 32 {
		return "", fmt.Errorf("SECRET_KEY must be at least 32 characters")
	}
	return secret, nil
}

func readSecretKeyFile(path string) (string, error) {
	content, err := security.ReadBoundedRegularFile(path, "SECRET_KEY_FILE", maxSecretKeyFileBytes)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func resolvePort() (string, error) {
	raw := strings.TrimSpace(getEnv("PORT", "8080"))
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("PORT must be a number between 1 and 65535")
	}
	return strconv.Itoa(port), nil
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		log.Printf("invalid %s=%q, using fallback %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Second {
		log.Printf("invalid %s=%q, using fallback %s", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("invalid %s=%q, using fallback %t", key, value, fallback)
		return fallback
	}
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func csrfMiddlewareConfig(cookieSecure bool) csrf.Config {
	return csrf.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Method() == fiber.MethodPost && c.Path() == security.OIDCCallbackPath
		},
		KeyLookup:      "form:csrf_token",
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		ContextKey:     "csrf",
		Extractor:      api.CSRFTokenExtractor,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			api.LogSecurityEvent(c, "csrf", "denied", api.SecurityEventField{
				Key:   "reason",
				Value: api.CSRFFailureReason(err),
			})
			return fiber.ErrForbidden
		},
	}
}

type authRateLimitConfig struct {
	ErrorCode string
}

func newAuthRateLimitHandler(handler *api.Handler, config authRateLimitConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)
		api.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "auth"},
			api.SecurityEventField{Key: "reason", Value: config.ErrorCode},
		)
		return handler.RespondAuthRateLimited(c, config.ErrorCode)
	}
}

func newAPIRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)
		api.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: rateLimitScope(c)},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return handler.RespondAPIRateLimited(c)
	}
}

func rateLimitScope(c *fiber.Ctx) string {
	path := c.Path()
	switch {
	case strings.HasPrefix(path, "/api/v1/users/current"):
		return "settings"
	case isV1AuthPath(path), strings.HasPrefix(path, "/auth/oidc"):
		return "auth"
	default:
		return "api"
	}
}

// isV1AuthPath returns true for the v1 auth surface (sessions, users
// creation, password-reset flow). Used by rateLimitScope to classify rate-
// limit events so the security log preserves the "auth" scope across the
// /api/v1/* migration.
func isV1AuthPath(path string) bool {
	switch path {
	case "/api/v1/users", "/api/v1/sessions", "/api/v1/sessions/current",
		"/api/v1/sessions/2fa-challenge", "/api/v1/password-resets",
		"/api/v1/password-resets/redeem":
		return true
	}
	return false
}

// rateLimitOnlyFor returns a Next predicate for fiber's limiter middleware
// that lets the limiter run only when the request's method and path match
// exactly. Fiber's Use() is path-prefix-matched and method-agnostic; without
// this filter a limiter wired to "/api/v1/sessions" would also fire on
// sibling routes such as POST /api/v1/sessions/2fa-challenge that share the
// prefix, silently broadening the rate-limit budget.
func rateLimitOnlyFor(method, path string) func(*fiber.Ctx) bool {
	return func(c *fiber.Ctx) bool {
		return !(c.Method() == method && c.Path() == path)
	}
}

func logRateLimitHit(c *fiber.Ctx) {
	retryAfter := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if retryAfter == "" {
		retryAfter = "unknown"
	}

	log.Printf("rate limit reached: method=%s path=%s retry_after=%s", c.Method(), api.SafeRequestLogPath(c), retryAfter)
}
