package api

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/i18n"
	"github.com/terraincognita07/ovumcy/internal/services"
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

	handler, err := NewHandler("test-secret-key", templatesDir, time.UTC, i18nManager, cookieSecure, newTestHandlerDependencies(database, i18nManager))
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

func newTestHandlerDependencies(database *gorm.DB, i18nManager *i18n.Manager) Dependencies {
	repositories := db.NewRepositories(database)
	authService := services.NewAuthService(repositories.Users)
	attemptLimiter := services.NewAttemptLimiter()
	passwordResetService := services.NewPasswordResetService(authService, attemptLimiter)
	loginService := services.NewLoginService(authService, passwordResetService)
	dayService := services.NewDayService(repositories.DailyLogs, repositories.Users)
	reservedBuiltinNames := make([]string, 0)
	if i18nManager != nil {
		reservedBuiltinNames = services.BuiltinSymptomReservedNames(i18nManager)
	}
	symptomService := services.NewSymptomService(repositories.Symptoms, reservedBuiltinNames...)
	registrationService := services.NewRegistrationService(authService, repositories.Users)
	viewerService := services.NewViewerService(dayService, symptomService)
	statsService := services.NewStatsService(dayService, symptomService)
	calendarViewService := services.NewCalendarViewService(dayService, statsService)
	dashboardViewService := services.NewDashboardViewService(statsService, viewerService, dayService)
	exportService := services.NewExportService(dayService, symptomService)
	settingsService := services.NewSettingsService(repositories.Users)
	notificationService := services.NewNotificationService()
	settingsViewService := services.NewSettingsViewService(settingsService, notificationService, exportService, symptomService)
	onboardingService := services.NewOnboardingService(repositories.Users)
	setupService := services.NewSetupService(repositories.Users)

	return Dependencies{
		AuthService:          authService,
		RegistrationService:  registrationService,
		PasswordResetService: passwordResetService,
		LoginService:         loginService,
		DayService:           dayService,
		SymptomService:       symptomService,
		ViewerService:        viewerService,
		StatsService:         statsService,
		CalendarViewService:  calendarViewService,
		DashboardViewService: dashboardViewService,
		ExportService:        exportService,
		SettingsService:      settingsService,
		SettingsViewService:  settingsViewService,
		OnboardingService:    onboardingService,
		SetupService:         setupService,
	}
}

func testCSRFMiddlewareConfig(cookieSecure bool) csrf.Config {
	return csrf.Config{
		KeyLookup:      "form:csrf_token",
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		ContextKey:     "csrf",
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
