package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/i18n"
	"github.com/terraincognita07/ovumcy/internal/services"
	"gorm.io/gorm"
)

func TestDashboardTodaySavePersistsAndRendersWithNonUTCTimezone(t *testing.T) {
	app, database, location := newOnboardingTestAppWithLocation(t, time.FixedZone("UTC+3", 3*60*60))
	user := createOnboardingTestUser(t, database, "dashboard-today-tz@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(location), location).Format("2006-01-02")
	note := "timezone save note"

	form := url.Values{
		"is_period": {"true"},
		"flow":      {"none"},
		"notes":     {note},
	}

	saveRequest := httptest.NewRequest(http.MethodPost, "/api/days/"+today, strings.NewReader(form.Encode()))
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRequest.Header.Set("HX-Request", "true")
	saveRequest.Header.Set("Accept-Language", "en")
	saveRequest.Header.Set("Cookie", authCookie)

	saveResponse, err := app.Test(saveRequest, -1)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	defer saveResponse.Body.Close()

	if saveResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", saveResponse.StatusCode)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)

	dashboardResponse, err := app.Test(dashboardRequest, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer dashboardResponse.Body.Close()

	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", dashboardResponse.StatusCode)
	}

	body, err := io.ReadAll(dashboardResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)

	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatal("expected period toggle to remain checked for saved day in non-UTC timezone")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected notes to be restored in dashboard textarea, got body without %q", note)
	}
}

func newOnboardingTestAppWithLocation(t *testing.T, location *time.Location) (*fiber.App, *gorm.DB, *time.Location) {
	t.Helper()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file path")
	}

	apiDir := filepath.Dir(testFile)
	internalDir := filepath.Dir(apiDir)
	templatesDir := filepath.Join(internalDir, "templates")
	localesDir := filepath.Join(internalDir, "i18n", "locales")
	databasePath := filepath.Join(t.TempDir(), "ovumcy-onboarding-test-tz.db")

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

	handler, err := NewHandler("test-secret-key", templatesDir, location, i18nManager, false, newTestHandlerDependencies(database))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	RegisterRoutes(app, handler)
	return app, database, location
}
