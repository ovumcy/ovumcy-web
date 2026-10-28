package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
)

// TestLayoutMarksOnboardingPendingForTheTimezoneSync pins WEB-248: the
// onboarding gate refuses POST /api/v1/users/current/timezone until onboarding
// completes, so the layout tells the client sync to stay off those pages. Before
// the fix the sync fired from every pre-onboarding page and drew a 403 each time.
func TestLayoutMarksOnboardingPendingForTheTimezoneSync(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	pending := createOnboardingTestUser(t, database, "tz-onboarding-pending@example.com", "StrongPass1", false)
	done := createOnboardingTestUser(t, database, "tz-onboarding-done@example.com", "StrongPass1", true)

	if got := onboardingPendingBodies(t, app, "/onboarding", loginAndExtractAuthCookie(t, app, pending.Email, "StrongPass1")); got != 1 {
		t.Fatalf("pre-onboarding page: expected <body data-onboarding-pending>, found %d", got)
	}
	if got := onboardingPendingBodies(t, app, "/dashboard", loginAndExtractAuthCookie(t, app, done.Email, "StrongPass1")); got != 0 {
		t.Fatalf("onboarded owner's page must not carry data-onboarding-pending, found %d", got)
	}
}

func onboardingPendingBodies(t *testing.T, app *fiber.App, path, cookie string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document, err := html.Parse(strings.NewReader(mustReadBodyString(t, response.Body)))
	if err != nil {
		t.Fatalf("%s: parse page: %v", path, err)
	}
	count := 0
	walkElementsWithAttribute(document, "data-onboarding-pending", func(element *html.Node) {
		if element.Data == "body" {
			count++
		}
	})
	return count
}
