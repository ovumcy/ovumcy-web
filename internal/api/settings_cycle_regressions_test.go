package api

import (
	"encoding/json"
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
	// The min attribute is the rolling floor SettingsCycleStartDateBounds
	// returns — any calendar day, not January 1st: the floor used to be the
	// start of the current year, which put a December cycle start out of reach
	// through the whole of January.
	lastPeriodInputAccessibilityPattern := regexp.MustCompile(`(?s)data-date-field-id="settings-last-period-start".*?id="settings-last-period-start".*?lang="en".*?min="\d{4}-\d{2}-\d{2}".*?aria-label="Day".*?aria-label="Month".*?aria-label="Year"`)
	if !lastPeriodInputAccessibilityPattern.MatchString(rendered) {
		t.Fatalf("expected settings last-period-start field to include localized segmented accessibility labels and range attributes")
	}
}

// TestSettingsUsageGoalChooserLeadsWithTheNeutralDefault pins the display order
// of the settings mode chooser against the same contract onboarding renders:
// the neutral default first, the two alternative modes after it.
func TestSettingsUsageGoalChooserLeadsWithTheNeutralDefault(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-goal-order@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, app, authCookie))

	assertUsageGoalOrder(t, htmlRadioValues(document, "usage_goal"))
}

// TestSettingsCycleGoalOnlyPatchWritesNothingButTheGoal covers the shape the
// dashboard quick switch sends: a body carrying usage_goal and nothing else.
// It rides the endpoint the settings form already uses, writes only that one
// column, and asks the browser to re-render — the goal reframes the whole page.
func TestSettingsCycleGoalOnlyPatchWritesNothingButTheGoal(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-goal-only@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":  31,
		"period_length": 6,
		"age_group":     models.AgeGroup40To45,
		"usage_goal":    models.UsageGoalHealth,
	}).Error; err != nil {
		t.Fatalf("seed cycle values: %v", err)
	}

	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	goalOnly := func(token string) *http.Request {
		form := url.Values{"usage_goal": {models.UsageGoalAvoid}}
		if strings.TrimSpace(token) != "" {
			form.Set("csrf_token", token)
		}
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))
		return request
	}

	assertStatusCode(t, mustAppResponse(t, app, goalOnly("")), http.StatusForbidden)

	response := mustAppResponse(t, app, goalOnly(csrfToken))
	assertStatusCode(t, response, http.StatusNoContent)
	if got := response.Header.Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected the quick switch to re-render the page, got HX-Refresh=%q", got)
	}

	persisted := models.User{}
	if err := database.Select("cycle_length", "period_length", "age_group", "usage_goal").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("expected persisted usage_goal=%q, got %q", models.UsageGoalAvoid, persisted.UsageGoal)
	}
	if persisted.CycleLength != 31 || persisted.PeriodLength != 6 {
		t.Fatalf("expected cycle length/period untouched (31/6), got %d/%d", persisted.CycleLength, persisted.PeriodLength)
	}
	if persisted.AgeGroup != models.AgeGroup40To45 {
		t.Fatalf("expected age_group untouched (%q), got %q", models.AgeGroup40To45, persisted.AgeGroup)
	}
}

// TestSettingsCycleGoalOnlyPatchAnswersEveryCaller covers the two non-HTMX
// shapes of the same goal-only save: a JSON body from an API client, and a
// plain browser form post with no negotiation headers at all. The CSRF posture
// is covered by the HTMX case above; this one isolates body parsing and
// content negotiation.
func TestSettingsCycleGoalOnlyPatchAnswersEveryCaller(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-goal-only-negotiation@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	jsonRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(`{"usage_goal":"trying_to_conceive"}`))
	jsonRequest.Header.Set("Content-Type", "application/json")
	jsonRequest.Header.Set("Accept", "application/json")
	jsonRequest.Header.Set("Cookie", authCookie)

	jsonResponse := mustAppResponse(t, app, jsonRequest)
	assertStatusCode(t, jsonResponse, http.StatusOK)
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(mustReadBodyString(t, jsonResponse.Body)), &payload); err != nil {
		t.Fatalf("decode goal-only JSON response: %v", err)
	}
	// The stored goal is deliberately NOT echoed: `OkResponse` declares
	// `additionalProperties: false`. That the save happened is asserted against
	// the database below, and the response shape against the schema in
	// TestUsageGoalOnlySaveAnswersTheDeclaredOkShape.
	if _, echoed := payload["usage_goal"]; echoed {
		t.Fatalf("the goal-only save echoed a member the OkResponse schema forbids: %v", payload)
	}
	afterJSON := models.User{}
	if err := database.Select("usage_goal").First(&afterJSON, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if afterJSON.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("expected the JSON caller's goal %q to be stored, got %q", models.UsageGoalTrying, afterJSON.UsageGoal)
	}

	formRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(url.Values{
		"usage_goal": {models.UsageGoalAvoid},
	}.Encode()))
	formRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formRequest.Header.Set("Cookie", authCookie)

	formResponse := mustAppResponse(t, app, formRequest)
	assertStatusCode(t, formResponse, http.StatusSeeOther)
	if location := formResponse.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected a browser caller to land back on the dashboard, got %q", location)
	}

	persisted := models.User{}
	if err := database.Select("usage_goal").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("expected the last save to win with %q, got %q", models.UsageGoalAvoid, persisted.UsageGoal)
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
