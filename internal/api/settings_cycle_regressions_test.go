package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestSettingsCycleUpdatePersistsWithHTMXAndCSRF(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-persist@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":     15,
		"period_length":    5,
		"auto_period_fill": false,
		"irregular_cycle":  false,
	}).Error; err != nil {
		t.Fatalf("set initial cycle values: %v", err)
	}

	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	form := url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"auto_period_fill":  {"true"},
		"irregular_cycle":   {"true"},
		"last_period_start": {"2026-02-10"},
	}
	updateBody := submitSettingsCycleUpdate(t, app, authCookie, csrfCookie, csrfToken, form)
	assertSettingsCycleHTMXSuccess(t, updateBody)

	persisted := models.User{}
	if err := database.Select("cycle_length", "period_length", "auto_period_fill", "irregular_cycle", "last_period_start").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user cycle values: %v", err)
	}
	if persisted.CycleLength != 28 {
		t.Fatalf("expected persisted cycle_length=28, got %d", persisted.CycleLength)
	}
	if persisted.PeriodLength != 6 {
		t.Fatalf("expected persisted period_length=6, got %d", persisted.PeriodLength)
	}
	if !persisted.AutoPeriodFill {
		t.Fatalf("expected persisted auto_period_fill=true")
	}
	if !persisted.IrregularCycle {
		t.Fatalf("expected persisted irregular_cycle=true")
	}
	if persisted.LastPeriodStart == nil || persisted.LastPeriodStart.Format("2006-01-02") != "2026-02-10" {
		t.Fatalf("expected persisted last_period_start=2026-02-10, got %v", persisted.LastPeriodStart)
	}
}

func TestSettingsCycleUsesRequestTimezoneForLastPeriodStartValidation(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "settings-cycle-tz@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location).Format("2006-01-02")

	form := url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"5"},
		"last_period_start": {localToday},
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings cycle request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read settings cycle response body: %v", err)
	}
	if !strings.Contains(string(body), "status-ok") {
		t.Fatalf("expected success status markup for timezone-aware last_period_start, got %q", string(body))
	}

	updatedUser := models.User{}
	if err := database.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updatedUser.LastPeriodStart == nil {
		t.Fatal("expected persisted last_period_start")
	}

	// LastPeriodStart is a date-only value (UTC-midnight on disk per migration
	// 019). DateAtLocation/.In(location) would mis-shift it across DST and
	// negative-offset zones; read the calendar components directly instead —
	// see the docblock on services.DateAtLocation.
	savedLocalDay := services.CalendarDayKey(*updatedUser.LastPeriodStart)
	if savedLocalDay != localToday {
		t.Fatalf("expected saved last_period_start %q, got %q", localToday, savedLocalDay)
	}
}

func TestSettingsPageRendersPersistedCycleValues(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-values@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":     29,
		"period_length":    6,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update cycle values: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := renderSettingsPageForTest(t, app, authCookie)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="settings-period-length"`, message: "expected settings period slider max=14"},
		bodyStringMatch{fragment: `max="14"`, message: "expected settings period slider max=14"},
		bodyStringMatch{fragment: `id="settings-last-period-start"`, message: "expected settings cycle form to include editable last-period-start field"},
		bodyStringMatch{fragment: `name="auto_period_fill" value="true"`, message: "expected settings cycle form to include auto-period-fill toggle"},
		bodyStringMatch{fragment: `id="export-from"`, message: "expected export date range inputs to be rendered"},
		bodyStringMatch{fragment: `id="export-to"`, message: "expected export date range inputs to be rendered"},
	)

	cycleInputPattern := regexp.MustCompile(`(?s)name="cycle_length".*?value="29"`)
	if !cycleInputPattern.MatchString(rendered) {
		t.Fatalf("expected cycle slider value attribute to be rendered from DB")
	}
	periodInputPattern := regexp.MustCompile(`(?s)name="period_length".*?value="6"`)
	if !periodInputPattern.MatchString(rendered) {
		t.Fatalf("expected period slider value attribute to be rendered from DB")
	}
	autoPeriodFillPattern := regexp.MustCompile(`(?s)name="auto_period_fill".*?checked`)
	if !autoPeriodFillPattern.MatchString(rendered) {
		t.Fatalf("expected auto_period_fill checkbox to reflect persisted enabled state")
	}
	exportInputPattern := regexp.MustCompile(`(?s)data-export-from-field.*?data-date-field-id="export-from".*?data-date-field-open.*?data-export-to-field.*?data-date-field-id="export-to".*?data-date-field-open`)
	if !exportInputPattern.MatchString(rendered) {
		t.Fatalf("expected export date fields to render segmented controls with explicit calendar buttons")
	}
	lastPeriodInputAccessibilityPattern := regexp.MustCompile(`(?s)data-date-field-id="settings-last-period-start".*?id="settings-last-period-start".*?lang="en".*?min="\d{4}-01-01".*?aria-label="Day".*?aria-label="Month".*?aria-label="Year"`)
	if !lastPeriodInputAccessibilityPattern.MatchString(rendered) {
		t.Fatalf("expected settings last-period-start field to include localized segmented accessibility labels and range attributes")
	}
}

func renderSettingsPageForTest(t *testing.T, app *fiber.App, authCookie string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}
