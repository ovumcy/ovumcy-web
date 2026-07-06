package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/cli"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/reminders"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	staticassets "github.com/ovumcy/ovumcy-web/web"
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
	HSTSEnabled      bool
	OIDC             security.OIDCConfig
	RateLimits       rateLimitSettings
	Proxy            proxySettings
	AuditLogEnabled  bool
	// WebhookBlockPrivate mirrors the off-by-default WEBHOOK_BLOCK_PRIVATE_ADDRESSES
	// egress gate the notify CLI reads, so the built-in scheduler wires the same
	// deliverer hardening.
	WebhookBlockPrivate bool
	ReminderScheduler   reminderSchedulerSettings
}

// reminderSchedulerSettings configures the optional built-in reminder scheduler
// (issue #125). Enabled is DEFAULT FALSE: the always-on outbound component ships
// off and is opted into explicitly via REMINDER_SCHEDULER_ENABLED. Hour is the
// LOCAL hour-of-day (0-23) the daily pass runs at (REMINDER_SCHEDULER_HOUR,
// default 9); the scheduler reuses config.Location, there is no separate
// reminder timezone.
type reminderSchedulerSettings struct {
	Enabled bool
	Hour    int
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

	// maxRequestBodyBytes caps the raw HTTP request body. It is sized for the
	// largest supported JSON restore: services.MaxImportEntries (20000) day
	// records serialize to ~8-12 MiB, so 16 MiB keeps the documented import
	// capacity reachable over HTTP with headroom, while still bounding the body
	// far below fiber's per-connection buffers. Exceeding it yields a mapped 413
	// (ovumcyErrorHandler → api.RespondRequestEntityTooLarge) rather than a bare
	// fasthttp error.
	maxRequestBodyBytes = 16 << 20

	// staticAssetMaxAgeSeconds is the Cache-Control max-age (1 hour) fiber sets
	// on /static responses. Assets are cache-busted by a ?v=<build revision>
	// query on their <link>/<script> URLs (see base.html), so a stale bundle
	// self-heals on the next release; the short TTL bounds how long an
	// unversioned direct fetch can serve stale bytes while still avoiding
	// constant revalidation.
	staticAssetMaxAgeSeconds = 3600
)

// codecov:ignore:start -- main() composition root: this whole function body is
// sequencing/wiring, never business logic. Every function it calls
// (mustLoadLocation, mustLoadRuntimeConfig, mustOpenDatabase,
// mustNewI18nManager, mustNewHandler, newFiberApp, installGracefulShutdown,
// startReminderScheduler, logStartup, runServer, closeDatabase, ...) already
// carries its own direct unit tests or its own narrower codecov:ignore with a
// reason (bootstrap.BuildDependencies is exercised via the internal/api test
// helper; startReminderScheduler's own glue is covered in internal/reminders).
// main() itself is invoked only by the built binary and is exercised by
// image-smoke/e2e, never `go test` — there is no seam to unit-test "does main
// call these in the right order" other than running the real process. Do NOT
// add a per-line ignore here for a new call in this body; it is already
// covered by this region. If you add a CONDITIONAL or a decision (anything
// beyond "call the next already-tested helper") to main(), move that logic
// into its own tested helper function instead of leaving it here uncovered.
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
	database := mustOpenDatabase(config.DatabaseConfig)
	i18nManager := mustNewI18nManager(config.DefaultLanguage)
	dependencies := bootstrap.BuildDependencies(db.NewRepositories(database), []byte(config.SecretKey), i18nManager, bootstrap.Options{
		RegistrationMode: config.RegistrationMode,
		OIDCConfig:       config.OIDC,
		LoginAttempts:    bootstrap.AttemptLimit{Max: config.RateLimits.LoginMax, Window: config.RateLimits.LoginWindow},
		RecoveryAttempts: bootstrap.AttemptLimit{Max: config.RateLimits.ForgotPasswordMax, Window: config.RateLimits.ForgotPasswordWindow},
		LogoutAttempts:   &bootstrap.AttemptLimit{Max: config.RateLimits.LogoutMax, Window: config.RateLimits.LogoutWindow},
		AuditLogEnabled:  config.AuditLogEnabled,
	})
	handler := mustNewHandler(config, i18nManager, dependencies)
	app := newFiberApp(config, handler)
	served := make(chan struct{})
	sigCtx, stopSignals := installGracefulShutdown(app, served)
	defer stopSignals()

	// Optional built-in reminder scheduler (issue #125). Started AFTER the
	// signal context is wired and BEFORE runServer, gated on config (default
	// OFF). It observes sigCtx — the same context that stops the server — and is
	// launched with `go` inside Start, so it can neither delay app.Listen nor
	// touch served/app shutdown. schedulerDone closes when it has fully drained
	// (an already-closed channel when the scheduler is disabled).
	schedulerDone := startReminderScheduler(sigCtx, config, database, i18nManager)

	logStartup(config)
	err = runServer(app, ":"+config.Port)
	close(served)
	// The server has stopped. If the scheduler is running, wait (bounded) for any
	// in-flight pass to finish reading/writing the DB, THEN close the DB — so the
	// database outlives the last reminder access on this single exit path.
	reminders.Drain(schedulerDone, reminders.DefaultStopBudget)
	closeDatabase(database)
	if err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

// codecov:ignore:end

// runServer blocks in app.Listen until the listener fails or a graceful stop
// completes, then returns the listen error. It no longer closes the database:
// the optional reminder scheduler may still be draining an in-flight pass that
// reads/writes the DB, so closeDatabase is sequenced by main AFTER the scheduler
// drain (see main). The database is still closed on BOTH exit paths — there is
// one main return path and main always closes it — SQLite still checkpoints its
// WAL and releases the file before the process exits.
func runServer(app *fiber.App, address string) error {
	// Fiber v3 moved DisableStartupMessage out of fiber.Config and into the
	// per-listen ListenConfig; keep the banner suppressed as before.
	return app.Listen(address, fiber.ListenConfig{DisableStartupMessage: true})
}

// startReminderScheduler builds and launches the optional built-in reminder
// scheduler (issue #125) when REMINDER_SCHEDULER_ENABLED is on, returning a
// channel that closes once the scheduler goroutine has fully drained. When the
// scheduler is OFF (the default) it returns an already-closed channel so the
// caller's drain is an instant no-op and no goroutine, timer, or outbound
// component is created. The scheduler reuses the SAME notify service recipe the
// `ovumcy notify` CLI uses (bootstrap.BuildNotifyService) plus the app_state
// marker repository, and observes sigCtx for shutdown.
//
// codecov:ignore:start -- main() composition-root wiring; the scheduler logic
// (nextRun, catch-up, marker, drain) is unit-tested in internal/reminders and
// this glue only assembles boot-built collaborators.
func startReminderScheduler(sigCtx context.Context, config runtimeConfig, database *gorm.DB, i18nManager *i18n.Manager) <-chan struct{} {
	if !config.ReminderScheduler.Enabled {
		closed := make(chan struct{})
		close(closed)
		return closed
	}

	repositories := db.NewRepositories(database)
	notifyService := bootstrap.BuildNotifyService(repositories, []byte(config.SecretKey), i18nManager, config.WebhookBlockPrivate)
	scheduler := reminders.New(notifyService, repositories.AppState, reminders.Config{
		Hour:     config.ReminderScheduler.Hour,
		Location: config.Location,
	})
	return scheduler.Start(sigCtx)
}

// codecov:ignore:end

func closeDatabase(database *gorm.DB) {
	sqlDB, err := database.DB()
	if err == nil {
		err = sqlDB.Close()
	}
	if err != nil {
		log.Printf("database close: %v", err)
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
	// HSTS defaults to COOKIE_SECURE (preserving the historical coupling where
	// enabling secure cookies also pinned HTTPS) but is an independent switch:
	// HSTS_ENABLED=false lets an operator run secure cookies without pinning
	// browsers to HTTPS for a year, and HSTS_ENABLED=true opts in explicitly.
	hstsEnabled := getEnvBool("HSTS_ENABLED", cookieSecure)
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
		HSTSEnabled:      hstsEnabled,
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
		Proxy:               proxy,
		AuditLogEnabled:     getEnvBool("AUDIT_LOG_ENABLED", false),
		WebhookBlockPrivate: getEnvBool("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", false),
		ReminderScheduler: reminderSchedulerSettings{
			Enabled: getEnvBool("REMINDER_SCHEDULER_ENABLED", false),
			// getEnvInt rejects values <1, so hour 0 (valid) would be lost; use a
			// dedicated range helper that accepts the full 0-23 clock.
			Hour: getEnvIntInRange("REMINDER_SCHEDULER_HOUR", 9, 0, 23),
		},
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
	i18nManager, err := i18n.NewManager(defaultLanguage) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	if err != nil {
		log.Fatalf("i18n init failed: %v", err)
	}
	return i18nManager
}

func mustNewHandler(config runtimeConfig, i18nManager *i18n.Manager, dependencies api.Dependencies) *api.Handler {
	handler, err := api.NewHandler(config.SecretKey, config.Location, i18nManager, config.CookieSecure, dependencies) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	if err != nil {
		log.Fatalf("handler init failed: %v", err)
	}
	// Cache-bust versioned static asset URLs (?v=<token>) so a new build
	// invalidates stale JS/CSS without operator action; resolveAssetVersion
	// falls back ldflags → VCS revision → process start, so even a `go run`
	// deployment never serves a shared constant token across builds.
	handler.SetAssetVersion(resolveAssetVersion()) // codecov:ignore -- main() composition-root wiring; runs only in the binary (exercised by e2e).
	return handler
}

// codecov:ignore:start -- main() composition-root wiring: this function only
// assembles the real Fiber app (middleware registration order, static-asset
// mount, ROUTE REGISTRATION via api.RegisterRoutes, the catch-all NotFound)
// for the actual binary. Every collaborator it calls is independently unit-
// tested (fiberConfig, configureFiberMiddleware, newStaticAssetHandler) or
// exercised through the internal/api test helper that builds its own app and
// calls api.RegisterRoutes directly (registerPageRoutes/registerV1APIRoutes
// live in internal/api/routes.go, not here) — but newFiberApp itself, as the
// exact sequence a new endpoint's route/middleware wiring lands in, is only
// ever invoked by main() and is exercised by image-smoke/e2e. Any FUTURE route
// registration or dependency-construction line added inside this function stays
// covered by this region — do not add a new per-line codecov:ignore for it.
// If new code here starts making a decision (not just wiring an
// already-tested collaborator), pull it into its own tested function instead.
func newFiberApp(config runtimeConfig, handler *api.Handler) *fiber.App {
	app := fiber.New(fiberConfig(config.Proxy))
	configureFiberMiddleware(app, config, handler)
	registerStaticContentTypes()
	app.Use("/static", newStaticAssetHandler())
	api.RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app
}

// codecov:ignore:end

func registerStaticContentTypes() {
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Printf("register .webmanifest MIME type: %v", err)
	}
}

// newStaticAssetHandler serves the browser static assets embedded into the
// binary (staticassets.Files) via fiber's static middleware, so the runtime
// needs no on-disk web/static directory. MaxAge preserves the same public
// Cache-Control max-age (staticAssetMaxAgeSeconds) the handler emitted under
// Fiber v2; assets are cache-busted by the ?v=<build revision> query on their
// URLs (see base.html). The root argument is empty because the assets are
// supplied as an io/fs.FS via Config.FS (Fiber v3 requires an empty root for
// fs.FS-backed serving); on a miss the middleware resets to a clean response
// and calls c.Next(), so unknown /static paths fall through to the app's
// NotFound handler exactly as before.
func newStaticAssetHandler() fiber.Handler {
	assets, err := fs.Sub(staticassets.Files, "static")
	if err != nil {
		log.Fatalf("static assets init failed: %v", err) // codecov:ignore -- unreachable: the embedded static/ subtree always exists at build time.
	}
	return static.New("", static.Config{
		FS:     assets,
		MaxAge: staticAssetMaxAgeSeconds,
	})
}

func fiberConfig(proxy proxySettings) fiber.Config {
	appConfig := fiber.Config{
		AppName:      "Ovumcy",
		ErrorHandler: ovumcyErrorHandler,
		BodyLimit:    maxRequestBodyBytes,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if !proxy.Enabled {
		return appConfig
	}
	// Fiber v3 collapses v2's EnableTrustedProxyCheck+TrustedProxies into
	// TrustProxy (the on/off switch) plus TrustProxyConfig.Proxies (the exact
	// IP/CIDR allowlist). EnableIPValidation keeps the same name and meaning.
	// Proxies must list only literal IPs/CIDRs (no Loopback/Private/LinkLocal
	// convenience flags) so fiber's trusted set stays byte-for-byte identical to
	// the boundary trustedProxyMatcher parses for the rate-limit key generator.
	appConfig.ProxyHeader = proxy.Header
	appConfig.TrustProxy = true
	appConfig.EnableIPValidation = true
	appConfig.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: proxy.TrustedProxies}
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
func rateLimitKeyGenerator(proxy proxySettings) func(fiber.Ctx) string {
	trusted := newTrustedProxyMatcher(proxy.TrustedProxies)
	headerIsXForwardedFor := strings.EqualFold(strings.TrimSpace(proxy.Header), fiber.HeaderXForwardedFor)
	return func(c fiber.Ctx) string {
		peer := c.RequestCtx().RemoteIP()
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
func ovumcyErrorHandler(c fiber.Ctx, err error) error {
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		// A body exceeding BodyLimit is raised by fiber's core before any app
		// middleware/handler runs, so route it through the shared error-spec
		// negotiation (JSON envelope / localized HTMX fragment with a stable
		// key) instead of leaking fasthttp's bare "Request Entity Too Large".
		if fiberErr.Code == fiber.StatusRequestEntityTooLarge {
			return api.RespondRequestEntityTooLarge(c)
		}
		return c.Status(fiberErr.Code).SendString(fiberErr.Message)
	}
	return c.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
}

func configureFiberMiddleware(app *fiber.App, config runtimeConfig, handler *api.Handler) {
	// keyGen resolves the real client IP for every limiter so a spoofed
	// X-Forwarded-For prefix cannot mint fresh per-IP buckets. See
	// rateLimitKeyGenerator for the trust-boundary derivation.
	keyGen := rateLimitKeyGenerator(config.Proxy)
	app.Use(securityHeadersMiddleware(config.HSTSEnabled))
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
	// Per-IP limiter for the cookieless calendar-feed endpoint. It is not under
	// /api, so the /api limiter does not cover it; a public, tokened polling
	// surface must be independently capped so a leaked/guessed URL cannot be
	// hammered. Reuses the same spoof-proof key generator and the API budget.
	app.Use(api.CalendarFeedRateLimitPrefix, limiter.New(limiter.Config{
		Max:          config.RateLimits.APIMax,
		Expiration:   config.RateLimits.APIWindow,
		KeyGenerator: keyGen,
		LimitReached: newCalendarFeedRateLimitHandler(handler),
	}))
	app.Use(handler.LanguageMiddleware)
	app.Use(csrf.New(csrfMiddlewareConfig(config.CookieSecure, handler)))
}

const requestLoggerFormat = "${time} | ${status} | ${latency} | ${method} | ${request_path} | ${safe_error}\n"

func newRequestLogger(output io.Writer) fiber.Handler {
	config := logger.Config{
		Format: requestLoggerFormat,
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return buffer.WriteString(api.SafeRequestLogPath(c))
			},
			"safe_error": func(buffer logger.Buffer, c fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return buffer.WriteString(api.SafeLogError(data.ChainErr))
			},
		},
	}
	if output != nil {
		// Fiber v3 renamed logger.Config.Output to Stream (still an io.Writer).
		config.Stream = output
	}
	return logger.New(config)
}

func securityHeadersMiddleware(enableStrictTransportSecurity bool) fiber.Handler {
	return func(c fiber.Ctx) error {
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

// installGracefulShutdown wires SIGINT/SIGTERM to a graceful stop. served
// must be closed once app.Listen (inside runServer) returns, for any reason —
// it bounds retryShutdown so a signal arriving after the server already
// exited doesn't spin.
//
// It returns both the stop function (to release the signal registration on
// exit) AND the signal context. The context is observed by the optional
// reminder scheduler so it stops on the SAME SIGINT/SIGTERM that stops the
// server. The scheduler only reads that context — it NEVER references served,
// the fiber app, or app shutdown, so it stays entirely clear of the boot-window
// shutdown race that retryShutdown/served exist to bridge.
func installGracefulShutdown(app *fiber.App, served <-chan struct{}) (context.Context, context.CancelFunc) {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		retryShutdown(app, shutdownCtx, served)
	}()
	return sigCtx, stopSignals
}

// shutdownRetryInterval is how often retryShutdown re-attempts the graceful
// stop while it is still being silently no-oped in the boot window. It is a
// named constant (not an inline literal) so the retry loop and the tests that
// drive it deterministically share one source of truth.
const shutdownRetryInterval = 20 * time.Millisecond

// retryShutdown calls app.ShutdownWithContext until it takes effect.
// fasthttp's ShutdownWithContext silently no-ops ("if s.ln == nil { return
// nil }") when called before Serve has registered the listener — the boot
// window between fiber's net.Listen and fasthttp registering it, which fiber
// v3's own OnListen hook fires strictly *before* (listen.go: runOnListenHooks
// precedes app.server.Serve(ln)). A single call can silently lose the stop
// request in that window; retrying until served closes (Listen has returned,
// so either the stop already landed or there's nothing left to stop) bridges
// it without slowing down the common, non-racing case.
func retryShutdown(app *fiber.App, ctx context.Context, served <-chan struct{}) {
	retryShutdownFunc(app.ShutdownWithContext, ctx, served, shutdownRetryInterval)
}

// retryShutdownFunc is the interval-driven retry loop behind retryShutdown,
// with the shutdown call and tick interval injected. Production passes
// app.ShutdownWithContext and shutdownRetryInterval, so retryShutdown's
// behavior is byte-for-byte unchanged; the seam exists purely so tests can
// exercise the retry/log/terminate contract deterministically. A stub
// shutdown func lets the error-branch test force a persistent failure without
// depending on real fasthttp accept timing (whether the raw connection is
// counted as open at the instant of the stop call) — the race that made
// TestRetryShutdownLogsPersistentShutdownError flaky under load.
func retryShutdownFunc(shutdown func(context.Context) error, ctx context.Context, served <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := shutdown(ctx); err != nil {
			log.Printf("server shutdown failed: %v", err)
			return
		}
		select {
		case <-served:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func logStartup(config runtimeConfig) {
	log.Printf(
		"Ovumcy listening on http://0.0.0.0:%s (rev: %s, tz: %s, registration=%s, oidc=%t, audit_log=%t, hsts=%t, rate_limits: login=%d/%s api=%d/%s, trusted_proxy=%t)",
		config.Port,
		buildRevision(),
		config.Location.String(),
		config.RegistrationMode,
		config.OIDC.Enabled,
		config.AuditLogEnabled,
		config.HSTSEnabled,
		config.RateLimits.LoginMax,
		config.RateLimits.LoginWindow,
		config.RateLimits.APIMax,
		config.RateLimits.APIWindow,
		config.Proxy.Enabled,
	)
	if config.HSTSEnabled {
		log.Printf("NOTE: HSTS_ENABLED=true — sending Strict-Transport-Security with a 1-year max-age (max-age=31536000; includeSubDomains). Browsers will refuse plain HTTP for this host for a year; only enable when committed to HTTPS. Set HSTS_ENABLED=false to keep secure cookies without the HTTPS pin.")
	}
	if config.ReminderScheduler.Enabled {
		log.Printf("NOTE: REMINDER_SCHEDULER_ENABLED=true — the built-in daily reminder scheduler is ON and will make outbound webhook calls once per day at local hour %02d:00 (%s) from this process. This is an always-on component; disable it instantly with REMINDER_SCHEDULER_ENABLED=false (delivery also still requires each owner's webhook to be configured and enabled).", config.ReminderScheduler.Hour, config.Location.String())
	}
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

// buildVersion is the release identity injected at build time:
//
//	go build -ldflags "-X main.buildVersion=<version>"
//
// The Dockerfile forwards its BUILD_REVISION build-arg here. It stays empty
// for builds that do not pass the flag (go run, plain go build); the asset
// cache-bust token then falls back to VCS or process-start identity.
var buildVersion string

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

// vcsRevisionFromBuildInfo extracts the raw vcs.revision stamped into info
// and whether the working tree was modified. revision is "" when info is nil
// or carries no usable revision — `go run` never stamps VCS settings, and
// neither does a build from a tree without .git (the Docker build context
// only copies the source directories).
func vcsRevisionFromBuildInfo(info *debug.BuildInfo) (revision string, modified bool) {
	if info == nil {
		return "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value) == "true"
		}
	}
	return revision, modified
}

// assetVersionShortRevisionLength trims a full 40-char commit sha to a short
// prefix so the "-dirty" marker still fits within the api layer's 16-char
// asset-version token cap; a prefix is just as good a cache-bust token as the
// full sha.
const assetVersionShortRevisionLength = 10

// resolveAssetVersion picks the cache-busting token for versioned static
// asset URLs (?v=<token>): the ldflags-injected buildVersion when set (the
// release image path), else the short VCS revision when the binary carries
// one (go build from a git checkout), else a process-start timestamp — so a
// from-source deployment (`go run`, .git-less build) gets a token that
// changes per start instead of the shared constant "unknown" every such
// build used to serve, which let stale cached assets survive upgrades.
func resolveAssetVersion() string {
	info, _ := debug.ReadBuildInfo()
	return assetCacheBustToken(buildVersion, info, time.Now())
}

// assetCacheBustToken implements resolveAssetVersion's fallback chain on
// explicit inputs so each step stays unit-testable.
func assetCacheBustToken(ldflagsVersion string, info *debug.BuildInfo, processStart time.Time) string {
	if version := strings.TrimSpace(ldflagsVersion); version != "" {
		return version
	}
	if revision, modified := vcsRevisionFromBuildInfo(info); revision != "" {
		revision = strings.TrimSpace(revision)
		if len(revision) > assetVersionShortRevisionLength {
			revision = revision[:assetVersionShortRevisionLength]
		}
		if modified {
			return revision + "-dirty"
		}
		return revision
	}
	return "dev-" + strconv.FormatInt(processStart.Unix(), 10)
}

func tryRunCLICommand() (bool, error) {
	return tryRunCLICommandWithHandlers(os.Args[1:], cliCommandHandlers{
		runResetPassword: cli.RunResetPasswordCommand,
		runUsers:         cli.RunUsersCommand,
		runHealthcheck:   cli.RunHealthcheckCommand,
		runNotify:        cli.RunNotifyCommand,  // codecov:ignore -- main() composition-root wiring; this os.Args dispatch wrapper runs only in the binary (the handler is unit-tested via tryRunCLICommandWithHandlers with a stub)
		runWebhook:       cli.RunWebhookCommand, // codecov:ignore -- main() composition-root wiring; this os.Args dispatch wrapper runs only in the binary (the handler is unit-tested via tryRunCLICommandWithHandlers with a stub)
	})
}

type cliCommandHandlers struct {
	runResetPassword func(databaseConfig db.Config, email string) error
	runUsers         func(databaseConfig db.Config, args []string) error
	runHealthcheck   func(port string, timeout time.Duration) error
	runNotify        func(databaseConfig db.Config, secretKey string, defaultLanguage string, location *time.Location, blockPrivateAddresses bool, args []string) error
	runWebhook       func(databaseConfig db.Config, secretKey string, args []string) error
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
			return true, fmt.Errorf("usage: ovumcy users <list|delete|create>")
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
	case "notify":
		if handlers.runNotify == nil {
			return true, fmt.Errorf("notify handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		secretKey, err := resolveSecretKey()
		if err != nil {
			return true, fmt.Errorf("invalid SECRET_KEY: %w", err)
		}
		location := mustLoadLocation(getEnv("TZ", "Local"))
		defaultLanguage := getEnv("DEFAULT_LANGUAGE", "en")
		blockPrivateAddresses := resolveBoolEnv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", false)
		return true, handlers.runNotify(databaseConfig, secretKey, defaultLanguage, location, blockPrivateAddresses, args[1:])
	case "webhook":
		if len(args) < 2 {
			return true, fmt.Errorf("usage: ovumcy webhook <show|set> <email> [flags]")
		}
		if handlers.runWebhook == nil {
			return true, fmt.Errorf("webhook handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		secretKey, err := resolveSecretKey()
		if err != nil {
			return true, fmt.Errorf("invalid SECRET_KEY: %w", err)
		}
		return true, handlers.runWebhook(databaseConfig, secretKey, args[1:])
	default:
		return false, nil
	}
}

// resolveBoolEnv reads a boolean environment flag, returning fallback when the
// variable is unset or unparseable. Used for the off-by-default
// WEBHOOK_BLOCK_PRIVATE_ADDRESSES egress gate.
func resolveBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
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

// getEnvIntInRange parses an integer env var and accepts it only within the
// inclusive [min, max] range, falling back otherwise. Unlike getEnvInt (which
// rejects anything below 1), it admits 0, which is required for
// REMINDER_SCHEDULER_HOUR where 0 is a valid midnight run hour.
func getEnvIntInRange(key string, fallback, minValue, maxValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minValue || parsed > maxValue {
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

func csrfMiddlewareConfig(cookieSecure bool, handler *api.Handler) csrf.Config {
	return csrf.Config{
		Next: func(c fiber.Ctx) bool {
			return c.Method() == fiber.MethodPost && c.Path() == security.OIDCCallbackPath
		},
		// Fiber v3 removed KeyLookup and ContextKey: the token source is now a
		// typed extractors.Extractor (see api.CSRFTokenExtractor, form-then-
		// header) and the token is read back via csrf.TokenFromContext.
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		// Behavior-preserving pin: Fiber v2 used Expiration=1h; v3 renamed this
		// to IdleTimeout and defaults it to 30m. Pin 1h so the token lifetime
		// (and thus form/session validity window) is unchanged by the upgrade.
		IdleTimeout: time.Hour,
		Extractor:   api.CSRFTokenExtractor(),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			handler.LogSecurityEvent(c, "csrf", "denied", api.SecurityEventField{
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
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "auth"},
			api.SecurityEventField{Key: "reason", Value: config.ErrorCode},
		)
		return handler.RespondAuthRateLimited(c, config.ErrorCode)
	}
}

func newAPIRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: rateLimitScope(c)},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return handler.RespondAPIRateLimited(c)
	}
}

// newCalendarFeedRateLimitHandler is the LimitReached handler for the per-IP
// calendar-feed limiter. The feed has no UI, so a 429 needs no HTML/JSON body:
// it returns a bare 429 (preserving the limiter-set Retry-After header) after
// logging the hit and a security event. logRateLimitHit masks the token in the
// path via SafeRequestLogPath, so the rate-limit log line never carries the
// token value.
func newCalendarFeedRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "calendar_feed"},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
}

func rateLimitScope(c fiber.Ctx) string {
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
func rateLimitOnlyFor(method, path string) func(fiber.Ctx) bool {
	return func(c fiber.Ctx) bool {
		return c.Method() != method || c.Path() != path
	}
}

func logRateLimitHit(c fiber.Ctx) {
	retryAfter := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if retryAfter == "" {
		retryAfter = "unknown"
	}

	log.Printf("rate limit reached: method=%s path=%s retry_after=%s", c.Method(), api.SafeRequestLogPath(c), retryAfter)
}
