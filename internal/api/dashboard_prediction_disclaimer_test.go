package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
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

// TestSettingsEgressSurfacesRenderPredictionDisclaimer covers the two settings
// sections that ship predicted dates off the instance: webhook reminders and
// the calendar feed. Both used to spell the qualifier out in their own
// catalogue entry; they now reference the single medical.disclaimer key, so
// this pins that the consolidation of the TEXT did not silently drop either
// SURFACE. The binding is made PER SURFACE: each hook's own subtree must carry
// the mandatory wording. A page-wide occurrence count cannot do that — an empty
// calendar-feed disclaimer beside a webhook disclaimer stating the sentence
// twice keeps the page total at two and passes while one egress surface ships
// predicted dates with no qualifier at all.
func TestSettingsEgressSurfacesRenderPredictionDisclaimer(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-disclaimer@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body := fetchPageBody(t, app, "/settings", authCookie)
	document := mustParseHTMLDocument(t, body)
	for _, hook := range []string{`data-webhook-disclaimer`, `data-calendar-feed-disclaimer`} {
		elements := htmlFindElements(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasAttr(node, hook)
		})
		if len(elements) == 0 {
			t.Fatalf("settings must render the disclaimer hook %q", hook)
		}
		for index, element := range elements {
			text := normalizeHTMLText(htmlNodeText(element))
			if !strings.Contains(text, "not medical advice or a method of contraception") {
				t.Fatalf("disclaimer hook %q (occurrence %d of %d) must carry the safety copy in its own subtree, got %q",
					hook, index+1, len(elements), text)
			}
		}
	}
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
