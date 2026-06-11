package api

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

func newOnboardingTestApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{})
}

func newOnboardingTestAppWithCookieSecure(t *testing.T, cookieSecure bool) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{cookieSecure: cookieSecure})
}

func newOnboardingTestAppWithCSRF(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{enableCSRF: true})
}

func newOnboardingTestAppWithRegistrationMode(t *testing.T, registrationMode services.RegistrationMode) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{registrationMode: registrationMode})
}

type onboardingTestAppOptions struct {
	cookieSecure     bool
	enableCSRF       bool
	registrationMode services.RegistrationMode
	oidcService      OIDCWorkflowService
}

func newOnboardingTestAppWithOptions(t *testing.T, options onboardingTestAppOptions) (*fiber.App, *gorm.DB) {
	t.Helper()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file path")
	}

	apiDir := filepath.Dir(testFile)
	internalDir := filepath.Dir(apiDir)
	templatesDir := filepath.Join(internalDir, "templates")
	localesDir := filepath.Join(internalDir, "i18n", "locales")
	databasePath := filepath.Join(t.TempDir(), "ovumcy-onboarding-test.db")

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	i18nManager, err := i18n.NewManager("en", localesDir)
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler("test-secret-key", templatesDir, time.UTC, i18nManager, options.cookieSecure, newTestHandlerDependencies(database, i18nManager, options))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	if options.enableCSRF {
		app.Use(csrf.New(testCSRFMiddlewareConfig(options.cookieSecure)))
	}
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app, database
}

func newTestHandlerDependencies(database *gorm.DB, i18nManager *i18n.Manager, options ...onboardingTestAppOptions) Dependencies {
	var appOptions onboardingTestAppOptions
	if len(options) > 0 {
		appOptions = options[0]
	}

	registrationMode := services.RegistrationModeOpen
	if appOptions.registrationMode != "" {
		registrationMode = appOptions.registrationMode
	}

	// Delegate to the shared composition-root wiring (internal/bootstrap), the
	// same recipe the production binary uses, so the two cannot drift. Tests pass
	// the default attempt limits, an empty (disabled) OIDC config, and—unlike
	// production—leave LogoutAttempts unset to keep the auth-service default.
	return bootstrap.BuildDependencies(db.NewRepositories(database), []byte("test-secret-key"), i18nManager, bootstrap.Options{
		RegistrationMode:    registrationMode,
		OIDCConfig:          security.OIDCConfig{},
		OIDCServiceOverride: appOptions.oidcService,
		LoginAttempts:       bootstrap.AttemptLimit{Max: services.DefaultLoginAttemptsLimit, Window: services.DefaultLoginAttemptsWindow},
		RecoveryAttempts:    bootstrap.AttemptLimit{Max: services.DefaultRecoveryAttemptsLimit, Window: time.Hour},
	})
}

func testCSRFMiddlewareConfig(cookieSecure bool) csrf.Config {
	return csrf.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Path() == security.OIDCCallbackPath
		},
		KeyLookup:      "form:csrf_token",
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		ContextKey:     "csrf",
		Extractor:      CSRFTokenExtractor,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			LogSecurityEvent(c, "csrf", "denied", SecurityEventField{
				Key:   "reason",
				Value: testCSRFFailureReason(err),
			})
			return fiber.ErrForbidden
		},
	}
}

func testCSRFFailureReason(err error) string {
	switch {
	case errors.Is(err, csrf.ErrTokenInvalid):
		return "invalid token"
	case errors.Is(err, csrf.ErrTokenNotFound):
		return "missing token"
	case errors.Is(err, csrf.ErrNoReferer), errors.Is(err, csrf.ErrBadReferer):
		return "invalid referer"
	default:
		return "csrf rejected"
	}
}
