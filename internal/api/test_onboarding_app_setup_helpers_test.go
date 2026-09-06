package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
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
	auditLogEnabled  bool
	assetVersion     string
	// bodyLimit overrides fiber's DefaultBodyLimit for this app. Zero keeps the
	// default. Body-cap regressions set a small value so the compressed payload
	// under test stays a few hundred bytes on the wire while its decoded size
	// crosses the cap.
	bodyLimit int
	// dayService replaces the day service the composition root builds, for the
	// regressions that need one of its reads to fail. It is a factory rather
	// than a value because the database is created inside the app helper, and
	// a fault-injecting repository has to wrap the real one to stay a day
	// service in every other respect.
	dayService func(database *gorm.DB) *services.DayService
	// outboundDeliveryEnabled mirrors REMINDER_SCHEDULER_ENABLED. It ships off,
	// and an app left at the default renders every readable webhook row as
	// "this instance sends no reminders" -- correct, and the reason the egress
	// matrix has to be able to turn it on to reach the states behind it.
	outboundDeliveryEnabled bool
}

func newOnboardingTestAppWithOptions(t *testing.T, options onboardingTestAppOptions) (*fiber.App, *gorm.DB) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-onboarding-test.db")

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
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

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler(testAppSecretKey, time.UTC, i18nManager, options.cookieSecure, newTestHandlerDependencies(database, i18nManager, options))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}
	if options.assetVersion != "" {
		handler.SetAssetVersion(options.assetVersion)
	}

	app := fiber.New(fiber.Config{BodyLimit: options.bodyLimit})
	app.Use(handler.LanguageMiddleware)
	if options.enableCSRF {
		app.Use(csrf.New(testCSRFMiddlewareConfig(options.cookieSecure, handler)))
	}
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app, database
}

// testAppSecretKey is the application secret the shared test app is wired with.
// A test that mints something keyed by SECRET_KEY outside the app (a calendar-feed
// verifier MAC, say) must use this same value, or the app will refuse what the
// test just stored.
const testAppSecretKey = "test-secret-key"

// sealCookieForTestApp seals an arbitrary payload under the shared test app's
// secret, producing a cookie value the app will genuinely open. Use it to hand
// the app a well-sealed but adversarially SHAPED payload — one no production
// producer would mint — so a read-path guard can be exercised on its own.
func sealCookieForTestApp(t *testing.T, cookieName string, payload []byte) string {
	t.Helper()
	codec, err := newSecureCookieCodec([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	sealed, err := codec.seal(cookieName, payload)
	if err != nil {
		t.Fatalf("seal %s payload: %v", cookieName, err)
	}
	return sealed
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
	dependencies := bootstrap.BuildDependencies(db.NewRepositories(database), []byte(testAppSecretKey), i18nManager, bootstrap.Options{
		RegistrationMode:        registrationMode,
		OIDCConfig:              security.OIDCConfig{},
		OIDCServiceOverride:     appOptions.oidcService,
		LoginAttempts:           bootstrap.AttemptLimit{Max: services.DefaultLoginAttemptsLimit, Window: services.DefaultLoginAttemptsWindow},
		RecoveryAttempts:        bootstrap.AttemptLimit{Max: services.DefaultRecoveryAttemptsLimit, Window: time.Hour},
		AuditLogEnabled:         appOptions.auditLogEnabled,
		OutboundDeliveryEnabled: appOptions.outboundDeliveryEnabled,
	})
	if appOptions.dayService != nil {
		dependencies.DayService = appOptions.dayService(database)
	}
	return dependencies
}

// testCSRFMiddlewareConfig mirrors csrfMiddlewareConfig (cmd/ovumcy/server.go):
// the same two Next clauses, the OIDC callback exemption scoped to POST and
// the calendar-feed skip keyed on IsCalendarFeedRequest. Nothing type-checks
// that this copy stays in sync with its source, so each clause is pinned
// separately, on THIS copy, by TestTestAppCSRFExemptionIsExactlyProductionsShape
// (calendar_feed_regressions_test.go): a mutating method at the feed route
// still 403s (kills `Next: return true` and a feed clause widened past
// GET/HEAD), and a GET at the OIDC callback still mints ovumcy_csrf (kills the
// OIDC clause losing its POST-method guard). The PRODUCTION copy's own shape —
// the exemption list, and the feed's no-cookie contract across every outcome —
// is guarded independently, on the real app, by
// cmd/ovumcy/csrf_exemption_guard_test.go and
// cmd/ovumcy/calendar_feed_no_cookie_test.go.
func testCSRFMiddlewareConfig(cookieSecure bool, handler *Handler) csrf.Config {
	return csrf.Config{
		Next: func(c fiber.Ctx) bool {
			if c.Method() == fiber.MethodPost && c.Path() == security.OIDCCallbackPath {
				return true
			}
			return IsCalendarFeedRequest(c.Method(), c.Path())
		},
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		IdleTimeout:    time.Hour,
		Extractor:      CSRFTokenExtractor(),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			handler.LogSecurityEvent(c, "csrf", "denied", SecurityEventField{
				Key:   "reason",
				Value: CSRFFailureReason(err),
			})
			return fiber.ErrForbidden
		},
	}
}

// testConfigNoTimeout restores fiber v2's app.Test(req, -1) "no timeout"
// semantics: v3's default TestConfig times out after 1s, which bcrypt-heavy
// auth tests exceed under coverage instrumentation.
var testConfigNoTimeout = fiber.TestConfig{Timeout: 0, FailOnTimeout: false}
