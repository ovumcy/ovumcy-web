package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDashboardRendersPredictionDisclaimer pins the medical-safety labeling
// from the prediction-accuracy pass: the dashboard must always render the
// "estimates, not medical advice or a method of contraception" disclaimer for
// the owner, so a future template refactor cannot silently drop it from a
// health-prediction surface.
func TestDashboardRendersPredictionDisclaimer(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "prediction-disclaimer@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	for _, fragment := range []string{
		`data-dashboard-prediction-disclaimer`,
		"not medical advice or a method of contraception",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("dashboard must render the prediction disclaimer fragment %q", fragment)
		}
	}
}

// TestStatsRendersPredictionDisclaimer extends the medical-safety labeling to
// the stats surface: it shows next-period/ovulation predictions, so the
// "estimates, not medical advice or a method of contraception" disclaimer must
// be present there too and cannot be dropped by a template refactor.
func TestStatsRendersPredictionDisclaimer(t *testing.T) {
	assertPredictionDisclaimerRendered(t, "/stats", `data-stats-prediction-disclaimer`)
}

// TestCalendarRendersPredictionDisclaimer extends the same medical-safety
// labeling to the calendar surface (predicted period / fertility / ovulation
// markers).
func TestCalendarRendersPredictionDisclaimer(t *testing.T) {
	assertPredictionDisclaimerRendered(t, "/calendar", `data-calendar-prediction-disclaimer`)
}

// assertPredictionDisclaimerRendered loads a predictive owner surface and pins
// both its stable data-hook and the exact safety copy, mirroring the dashboard
// check so every ovulation/next-period surface keeps the persistent disclaimer.
func assertPredictionDisclaimerRendered(t *testing.T, path, hook string) {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "prediction-disclaimer"+strings.ReplaceAll(path, "/", "-")+"@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	for _, fragment := range []string{
		hook,
		"not medical advice or a method of contraception",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("%s must render the prediction disclaimer fragment %q", path, fragment)
		}
	}
}
