package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestMarkCycleStartRequiresAuthJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-19/cycle-start", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("unauthenticated cycle-start request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %q", got)
	}
}

func TestMarkCycleStartRejectsUnsupportedLegacyRoleJSON(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-legacy@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-19/cycle-start", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("unsupported legacy role cycle-start request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
		t.Fatalf("expected unsupported-role sign-in error, got %q", got)
	}
}

func TestMarkCycleStartHTMXWithCSRFRefreshesAndPersists(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadManualCycleStartCSRFContext(t, app, authCookie)

	targetDay := "2026-02-19"
	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     mustParseManualCycleStartDay(t, targetDay),
		IsPeriod: false,
		Flow:     models.FlowMedium,
		Notes:    "keep me",
	}).Error; err != nil {
		t.Fatalf("create log: %v", err)
	}

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/days/"+targetDay+"/cycle-start?source=calendar", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}
	if got := response.Header.Get("HX-Trigger"); got != "calendar-day-updated" {
		t.Fatalf("expected HX-Trigger calendar-day-updated, got %q", got)
	}
	if got := response.Header.Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected HX-Refresh=true, got %q", got)
	}

	day := mustParseManualCycleStartDay(t, targetDay)
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load updated log: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatalf("expected selected day to persist as period day")
	}
	if !entry.CycleStart {
		t.Fatalf("expected selected day to persist as the explicit cycle start")
	}
	if entry.Notes != "keep me" {
		t.Fatalf("expected existing notes to be preserved, got %q", entry.Notes)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if persisted.LastPeriodStart != nil {
		t.Fatalf("expected manual cycle start to leave settings last_period_start unchanged, got %v", persisted.LastPeriodStart)
	}
}

func TestMarkCycleStartMissingCSRFRejectedByMiddleware(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-csrf@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-19/cycle-start", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}
}

func loadManualCycleStartCSRFContext(t *testing.T, app *fiber.App, authCookie string) (*http.Cookie, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard status 200 while preparing csrf context, got %d", response.StatusCode)
	}

	body := mustReadBodyString(t, response.Body)
	csrfToken := extractCSRFTokenFromHTML(t, body)
	csrfCookie := responseCookie(response.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatalf("expected csrf cookie in dashboard response")
	}

	return csrfCookie, csrfToken
}

func mustParseManualCycleStartDay(t *testing.T, raw string) time.Time {
	t.Helper()

	day, err := services.ParseDayDate(raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return day
}
