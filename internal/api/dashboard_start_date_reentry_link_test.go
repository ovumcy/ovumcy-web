package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDashboardStartDatePromptLinksToTheSettingsField pins the way back in for
// an account with no cycle anchor — the state a clear-data wipe leaves, where
// /onboarding is no longer reachable: the dashboard prompt links to the settings
// card holding the start-date field, and the settings page renders that card.
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

	dashboard := get("/dashboard")
	prompt := strings.Index(dashboard, "Enter the start date of your last cycle")
	if prompt < 0 {
		t.Fatal("expected the dashboard to show the start-date prompt for an account with no cycle anchor")
	}
	anchor := strings.LastIndex(dashboard[:prompt], "<a ")
	if anchor < 0 || strings.Contains(dashboard[anchor:prompt], "</a>") || !strings.Contains(dashboard[anchor:prompt], `href="/settings#settings-cycle"`) {
		t.Fatal("expected the start-date prompt to be a link to /settings#settings-cycle")
	}

	settings := get("/settings")
	card := strings.Index(settings, `id="settings-cycle"`)
	if card < 0 || !strings.Contains(settings[card:], `id="settings-last-period-start"`) {
		t.Fatal("expected the settings-cycle card to hold the start-date field")
	}
}
