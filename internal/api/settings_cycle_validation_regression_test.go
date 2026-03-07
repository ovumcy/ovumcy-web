package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSettingsCycleRejectsOutOfRangePeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-validation@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":  {"28"},
		"period_length": {"15"},
	}, "period_length=15")
}

func TestSettingsCycleRejectsIncompatibleCycleAndPeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-incompatible@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":  {"21"},
		"period_length": {"14"},
	}, "incompatible cycle/period values")
}

func TestSettingsCycleRejectsFutureLastPeriodStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-future-date@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"last_period_start": {"2999-01-01"},
	}, "future last_period_start")
}

func TestSettingsCycleRejectsTooOldLastPeriodStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-too-old-date@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"last_period_start": {"1969-12-31"},
	}, "too-old last_period_start")
}

func assertSettingsCycleStatusError(t *testing.T, app *fiber.App, authCookie string, form url.Values, scenario string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/settings/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, "status-error") {
		t.Fatalf("expected status-error markup for %s, got %q", scenario, body)
	}
}
