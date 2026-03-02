package api

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/i18n"
	"gorm.io/gorm"
)

func newOnboardingTestApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithCookieSecureAndCSRF(t, false, false)
}

func newOnboardingTestAppWithCookieSecure(t *testing.T, cookieSecure bool) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithCookieSecureAndCSRF(t, cookieSecure, false)
}

func newOnboardingTestAppWithCSRF(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	return newOnboardingTestAppWithCookieSecureAndCSRF(t, false, true)
}

func newOnboardingTestAppWithCookieSecureAndCSRF(t *testing.T, cookieSecure bool, enableCSRF bool) (*fiber.App, *gorm.DB) {
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

	handler, err := NewHandler(database, "test-secret-key", templatesDir, time.UTC, i18nManager, cookieSecure)
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	if enableCSRF {
		app.Use(csrf.New(testCSRFMiddlewareConfig(cookieSecure)))
	}
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app, database
}

func testCSRFMiddlewareConfig(cookieSecure bool) csrf.Config {
	return csrf.Config{
		KeyLookup:      "form:csrf_token",
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		ContextKey:     "csrf",
	}
}
