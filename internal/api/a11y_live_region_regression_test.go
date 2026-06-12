package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// These tests pin the a11y contract from audit task #26-A2: every HTMX
// save-status container that receives swapped success/error feedback must be
// an aria-live="polite" region, so screen-reader users hear the outcome of
// day saves, onboarding steps, and settings submissions instead of the page
// staying silent. The auth pages already carried aria-live; this locks the
// same treatment onto the dashboard, calendar editor, onboarding, and
// settings surfaces.

func assertLiveStatusContainers(t *testing.T, body string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		fragment := `<div id="` + id + `" class="save-status text-sm" aria-live="polite">`
		if !strings.Contains(body, fragment) {
			t.Fatalf("expected %q to render as an aria-live polite save-status container", id)
		}
	}
}

func fetchPageBody(t *testing.T, app *fiber.App, path string, authCookie string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

func TestDashboardSaveStatusIsLiveRegion(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-dashboard-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/dashboard", authCookie)
	assertLiveStatusContainers(t, body, "save-status")
}

func TestCalendarDayEditorSaveStatusIsLiveRegion(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-calendar-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/calendar/day/2026-02-17?mode=edit", authCookie)
	assertLiveStatusContainers(t, body, "calendar-save-status")
}

func TestOnboardingStepStatusesAreLiveRegions(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-onboarding-live@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/onboarding", authCookie)
	assertLiveStatusContainers(t, body, "onboarding-step1-status", "onboarding-step2-status")
}

func TestSettingsStatusContainersAreLiveRegions(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-settings-live@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/settings", authCookie)
	assertLiveStatusContainers(t, body,
		"settings-cycle-status",
		"settings-tracking-status",
		"settings-clear-data-status",
		"delete-account-feedback",
	)
}
