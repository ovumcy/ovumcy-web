package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestSettingsCycleRejectsOutOfRangePeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-validation@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":  {"28"},
		"period_length": {"15"},
	}, "onboarding.error.period_length_range")
}

func TestSettingsCycleRejectsIncompatibleCycleAndPeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-incompatible@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":  {"21"},
		"period_length": {"14"},
	}, "settings.cycle.error_incompatible")
}

func TestSettingsCycleRejectsFutureLastPeriodStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-future-date@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"last_period_start": {"2999-01-01"},
	}, "settings.error.invalid_last_period_start")
}

func TestSettingsCycleRejectsTooOldLastPeriodStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-too-old-date@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	assertSettingsCycleStatusError(t, app, authCookie, url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"last_period_start": {"1969-12-31"},
	}, "settings.error.invalid_last_period_start")
}

func assertSettingsCycleStatusError(t *testing.T, app *fiber.App, authCookie string, form url.Values, flashKey string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFlashByKey(document, flashKey) == nil {
		t.Fatalf("expected flash key %q in error response", flashKey)
	}
}
