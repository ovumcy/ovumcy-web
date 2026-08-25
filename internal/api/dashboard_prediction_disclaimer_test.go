package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

// TestDashboardMarksAnInexactOvulationEstimateWithAddressableQualifiers pins the
// second half of the medical-safety labeling on the dashboard: beside the
// persistent disclaimer above, the header marks the individual estimates that
// are weaker than a plain date, and each such marker is addressable by a stable
// data-* hook rather than by its copy.
//
// Both markers are rendered from the same signal — the luteal phase did not fit
// the reference cycle length, so CalcOvulationDay clamped it and reported
// OvulationExact=false — and until now nothing outside internal/templates named
// either of them: neither the hooks nor the i18n keys behind them appeared in
// any Go, TypeScript or CSS source, so the qualifiers could be dropped from the
// header by a template edit with every suite still green.
//
// The fixture is deliberately narrow, since only a narrow band of inputs
// produces an inexact estimate that the header still shows at all:
//   - a 16-day reference cycle with the default 14-day luteal phase forces the
//     clamp (CalcOvulationDay caps the luteal phase at cycleLength-5), which is
//     what sets Approximate on both the cycle hero and the reminder banner;
//   - a 3-day period keeps ovulation day 5 clear of periodLength+1, without
//     which the cycle hero is not drawn and its qualifier never renders;
//   - one previous cycle clears the first-cycle fertility floor
//     (FertilityProjectionSuppressed), without which the ovulation slot and the
//     ovulation reminder are withheld entirely;
//   - cycle day 3 puts ovulation two days out, inside the default three-day
//     reminder window, and the next period fourteen days out, outside it — so
//     the banner summarizes ovulation rather than the period;
//   - the trying-to-conceive goal is what puts the ovulation estimate in the
//     status line at all (resolveDashboardTimingFrame).
//
// Anchors below assert the fixture actually reached that state before judging
// the hooks, so a fixture that drifts out of the band fails loudly instead of
// passing on an absent surface.
func TestDashboardMarksAnInexactOvulationEstimateWithAddressableQualifiers(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-inexact-ovulation@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	const cycleLength = 16
	const periodLength = 3
	lastPeriodStart := today.AddDate(0, 0, -2)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      cycleLength,
		"period_length":     periodLength,
		"last_period_start": lastPeriodStart,
		"usage_goal":        models.UsageGoalTrying,
	}).Error; err != nil {
		t.Fatalf("update inexact-ovulation cycle context: %v", err)
	}
	for _, cycleStart := range []time.Time{lastPeriodStart.AddDate(0, 0, -cycleLength), lastPeriodStart} {
		for offset := range periodLength {
			if err := database.Create(&models.DailyLog{
				UserID:   user.ID,
				Date:     cycleStart.AddDate(0, 0, offset),
				IsPeriod: true,
				Flow:     models.FlowMedium,
			}).Error; err != nil {
				t.Fatalf("create period log %s day %d: %v", cycleStart.Format("2006-01-02"), offset, err)
			}
		}
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header")
	}

	ovulation := dashboardElementByDataAttr(header, "data-dashboard-ovulation")
	if ovulation == nil {
		t.Fatal("fixture anchor: expected the ovulation slot in the status line")
	}
	banner := dashboardElementByDataAttr(header, "data-dashboard-reminder-banner")
	if banner == nil {
		t.Fatal("fixture anchor: expected the ovulation reminder banner")
	}

	if dashboardElementByDataAttr(header, "data-dashboard-estimate-qualifier") == nil {
		t.Fatal("expected the cycle-hero estimate qualifier to carry data-dashboard-estimate-qualifier")
	}
	if dashboardElementByDataAttr(ovulation, "data-dashboard-ovulation-approximate") == nil {
		t.Fatal("expected the ovulation estimate's approximate marker to carry data-dashboard-ovulation-approximate")
	}
	if dashboardElementByDataAttr(banner, "data-dashboard-ovulation-approximate") == nil {
		t.Fatal("expected the reminder banner's approximate marker to carry data-dashboard-ovulation-approximate")
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
