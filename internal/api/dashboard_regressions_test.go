package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

func TestDashboardAndCalendarExposeAccessibleBBTInputs(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "bbt-accessibility@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitCelsius,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	dashboardBody := mustReadBodyString(t, dashboardResponse.Body)
	for _, fragment := range []string{
		`id="dashboard-bbt"`,
		`aria-labelledby="dashboard-bbt-legend"`,
		`aria-describedby="dashboard-bbt-hint"`,
	} {
		if !strings.Contains(dashboardBody, fragment) {
			t.Fatalf("expected dashboard BBT field markup %q", fragment)
		}
	}

	dayActionPrefix := `hx-post="/api/v1/days/`
	startIndex := strings.Index(dashboardBody, dayActionPrefix)
	if startIndex < 0 {
		t.Fatal("expected dashboard day form action")
	}
	dayStart := startIndex + len(dayActionPrefix)
	dayEnd := dayStart + len("2006-01-02")
	if len(dashboardBody) < dayEnd {
		t.Fatal("expected dashboard day form date to be present")
	}
	dayRaw := dashboardBody[dayStart:dayEnd]

	panelRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/"+dayRaw+"?mode=edit", nil)
	panelRequest.Header.Set("Accept-Language", "en")
	panelRequest.Header.Set("Cookie", authCookie)
	panelResponse := mustAppResponse(t, app, panelRequest)
	assertStatusCode(t, panelResponse, http.StatusOK)

	panelBody := mustReadBodyString(t, panelResponse.Body)
	for _, fragment := range []string{
		`id="calendar-bbt"`,
		`aria-labelledby="calendar-bbt-legend"`,
		`aria-describedby="calendar-bbt-hint"`,
	} {
		if !strings.Contains(panelBody, fragment) {
			t.Fatalf("expected calendar BBT field markup %q", fragment)
		}
	}
}

func TestDashboardStaleCycleWarningIncludesSettingsCTAAndEstimatedPhase(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-stale-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	warnings := dashboardElementByDataAttr(document, "data-dashboard-cycle-warnings")
	if warnings == nil {
		t.Fatal("expected dashboard cycle warning container when baseline is stale")
	}
	if dashboardElementByDataAttr(warnings, "data-dashboard-stale-warning") == nil {
		t.Fatal("expected stale cycle warning element inside the warning container")
	}
	settingsCTA := htmlFindElement(warnings, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && htmlAttr(node, "href") == "/settings#settings-cycle"
	})
	if settingsCTA == nil {
		t.Fatal("expected stale cycle warning to include direct settings CTA")
	}

	statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
	if statusLine == nil {
		t.Fatal("expected dashboard status line when stale baseline suppresses the hero")
	}
	if got := htmlAttr(statusLine, "data-dashboard-phase"); got != "unknown" {
		t.Fatalf("expected dashboard status line phase %q while cycle data is stale, got %q", "unknown", got)
	}
}

func TestDashboardAndStatsUseSameStalePhasePresentation(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-stats-stale-phase@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update stale baseline for user: %v", err)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse, err := app.Test(dashboardRequest, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer dashboardResponse.Body.Close()

	dashboardDocument := mustParseHTMLDocument(t, mustReadBodyString(t, dashboardResponse.Body))
	dashboardStatusLine := dashboardElementByDataAttr(dashboardDocument, "data-dashboard-status-line")
	if dashboardStatusLine == nil {
		t.Fatal("expected dashboard status line while cycle data is stale")
	}
	if got := htmlAttr(dashboardStatusLine, "data-dashboard-phase"); got != "unknown" {
		t.Fatalf("expected dashboard status line phase %q while cycle data is stale, got %q", "unknown", got)
	}

	statsRequest := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRequest.Header.Set("Accept-Language", "en")
	statsRequest.Header.Set("Cookie", authCookie)
	statsResponse, err := app.Test(statsRequest, -1)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer statsResponse.Body.Close()

	statsDocument := mustParseHTMLDocument(t, mustReadBodyString(t, statsResponse.Body))
	if dashboardElementByDataAttr(statsDocument, "data-stats-empty-state") == nil {
		t.Fatal("expected stats page to show gated empty state before enough completed cycles")
	}
}

func TestDashboardTodaySavePersistsPeriodToggleAndNotes(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-today-save@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	todayRaw := today.Format("2006-01-02")
	note := "Remember hydration and rest"

	form := url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowNone},
		"notes":     {note},
	}
	saveResponse := mustAppResponse(t, app, dashboardSaveRequest(todayRaw, form, authCookie))
	assertStatusCode(t, saveResponse, http.StatusOK)

	saveBody := mustReadBodyString(t, saveResponse.Body)
	if !strings.Contains(saveBody, "status-ok") {
		t.Fatalf("expected save status success markup")
	}

	parsedDay, err := services.ParseDayDate(todayRaw, time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, parsedDay, time.UTC)
	if err != nil {
		t.Fatalf("load stored day after dashboard save: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("expected period toggle to persist after dashboard save")
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected flow to remain %q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.Notes != note {
		t.Fatalf("expected notes %q, got %q", note, entry.Notes)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	rendered := mustReadBodyString(t, dashboardResponse.Body)
	periodCheckedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !periodCheckedPattern.MatchString(rendered) {
		t.Fatalf("expected dashboard period toggle to remain checked after reload")
	}
	if !strings.Contains(rendered, note) {
		t.Fatalf("expected dashboard notes field to include saved note %q", note)
	}
}

func dashboardSaveRequest(todayRaw string, form url.Values, authCookie string) *http.Request {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayRaw, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	return request
}

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

	saveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+today, strings.NewReader(form.Encode()))
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

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler("test-secret-key", location, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	RegisterRoutes(app, handler)
	return app, database, location
}
