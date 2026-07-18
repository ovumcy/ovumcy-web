package main

import (
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/security"
	staticassets "github.com/ovumcy/ovumcy-web/web"
)

const (
	headerXContentTypeOptions     = "X-Content-Type-Options"
	headerReferrerPolicy          = "Referrer-Policy"
	headerPermissionsPolicy       = "Permissions-Policy"
	headerCrossOriginOpenerPolicy = "Cross-Origin-Opener-Policy"
	headerXFrameOptions           = "X-Frame-Options"
	headerContentSecurityPolicy   = "Content-Security-Policy"
	headerStrictTransportSecurity = "Strict-Transport-Security"

	xContentTypeOptionsNoSniff     = "nosniff"
	referrerPolicyStrictOrigin     = "strict-origin-when-cross-origin"
	permissionsPolicyDefault       = "geolocation=(), camera=(), microphone=(), accelerometer=(), gyroscope=(), payment=(), usb=(), interest-cohort=(), ambient-light-sensor=()"
	crossOriginOpenerPolicyDefault = "same-origin"
	xFrameOptionsDeny              = "DENY"
	contentSecurityPolicyDefault   = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; worker-src 'none'"
	strictTransportSecurityDefault = "max-age=31536000; includeSubDomains"

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
		log.Printf("register .webmanifest MIME type: %v", err) // codecov:ignore -- defensive: a valid extension/type pair never errors.
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
