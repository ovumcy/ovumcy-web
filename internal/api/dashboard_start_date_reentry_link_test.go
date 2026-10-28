package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDashboardStartDatePromptLinksToTheSettingsField pins the way back in for
// an account with no cycle anchor — the state a clear-data wipe leaves, where
// /onboarding is no longer reachable: the dashboard prompt is a link, and its
// fragment names an element the settings page actually renders.
func TestDashboardStartDatePromptLinksToTheSettingsField(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "start-date-reentry@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	get := func(path string) string {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, http.StatusOK)
		return mustReadBodyString(t, response.Body)
	}

	const link = `<a href="/settings#settings-last-period-start-label" class="inline-link" data-dashboard-next-period-prompt>Enter the start date of your last cycle</a>`
	if dashboard := get("/dashboard"); !strings.Contains(dashboard, link) {
		t.Fatalf("expected the dashboard start-date prompt to link to the settings field")
	}
	if settings := get("/settings"); !strings.Contains(settings, `id="settings-last-period-start-label"`) {
		t.Fatalf("settings page no longer renders the start-date prompt's link target")
	}
}
