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
