package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestClearDataRemovesTrackedCalendarEntriesAndResetsCycleSettings(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/settings/clear-data", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected clear data status 200, got %d", response.StatusCode)
	}

	assertClearDataPostconditions(t, scenario.database, scenario.user)
}
