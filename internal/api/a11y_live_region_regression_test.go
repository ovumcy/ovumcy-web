package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
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
	document := mustParseHTMLDocument(t, body)
	for _, id := range ids {
		targetID := id
		node := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "id") == targetID
		})
		if node == nil {
			t.Fatalf("expected an element with id %q", targetID)
		}
		// The a11y contract is only id + aria-live=polite; the element's class
		// list and attribute ordering are incidental styling, not the contract.
		if got := htmlAttr(node, "aria-live"); got != "polite" {
			t.Fatalf("expected %q to be an aria-live=polite region, got aria-live=%q", targetID, got)
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

// TestBaseLayoutRendersSkipLink pins the skip-to-content link from audit
// task #26: every full page rendered through the base layout must offer
// keyboard users a way past the always-visible header and navigation
// (Tailwind's content scan reads these comments too, so utility-named
// bare words are avoided here on purpose), and the link's
// target must be focusable (tabindex=-1 on <main>) so the jump actually
// moves focus.
func TestBaseLayoutRendersSkipLink(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "a11y-skip-link@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, path := range []string{"/dashboard", "/settings"} {
		body := fetchPageBody(t, app, path, authCookie)
		// Structural hook only — the visible copy is i18n (a11y.skip_to_content)
		// and the actual skip behavior is owned by visual-a11y.spec.ts; do not
		// pin the English phrase here.
		if !strings.Contains(body, `<a href="#main-content" class="skip-link">`) {
			t.Fatalf("expected %s to render the skip-to-content link", path)
		}
		if !strings.Contains(body, `<main id="main-content" tabindex="-1"`) {
			t.Fatalf("expected %s to render a focusable #main-content target", path)
		}
	}
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
