package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// TestStatsPageShowsUnknownPhaseWhenCycleDataIsStale reads the phase the page
// actually renders. The account has two completed cycles, so the insights grid
// (and with it the current-phase card) is on screen and the only reason left to
// withhold a phase is the stale anchor — the condition under test. Seeding no
// cycles at all would replace the whole grid with the empty state, and the card
// this test is named for would never be reached.
func TestStatsPageShowsUnknownPhaseWhenCycleDataIsStale(t *testing.T) {
	document := renderStatsPageWithStaleCycleData(t)

	phaseValue := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-current-phase")
	})
	if phaseValue == nil {
		t.Fatalf("expected the current-phase card to render with a data-stats-current-phase hook")
	}
	if got := htmlAttr(phaseValue, "data-stats-current-phase"); got != "unknown" {
		t.Fatalf("expected the rendered phase to be unknown on a 60-day-stale cycle, got %q", got)
	}
	if got := htmlAttr(phaseValue, "data-fertility-status"); got != "unknown" {
		t.Fatalf("expected the rendered fertility status to be unknown on a 60-day-stale cycle, got %q", got)
	}

	staleNote := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-phase-estimated")
	})
	if staleNote == nil {
		t.Fatalf("expected the phase-estimated note beside the withheld phase")
	}
}

// TestStatsPageRendersTheDetectedPhaseWhileTheAnchorIsCurrent is the positive
// anchor for the case above: same fixture shape, a current anchor, so a change
// that hard-coded the unknown phase would be caught here rather than read as a
// pass.
func TestStatsPageRendersTheDetectedPhaseWhileTheAnchorIsCurrent(t *testing.T) {
	document := renderStatsPageForCycleAnchorAge(t, "stats-fresh-ui@example.com", 3)

	phaseValue := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-current-phase")
	})
	if phaseValue == nil {
		t.Fatalf("expected the current-phase card to render with a data-stats-current-phase hook")
	}
	if got := htmlAttr(phaseValue, "data-stats-current-phase"); got == "unknown" {
		t.Fatalf("expected a detected phase while the cycle anchor is current, got %q", got)
	}
}

func TestStatsPageEmptyStateUsesDedicatedProgressMeterWithoutInlineStyle(t *testing.T) {
	document := renderStatsPageWithoutCompletedCycles(t)
	emptyState := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-empty-state")
	})
	if emptyState == nil {
		t.Fatalf("expected stats empty-state element with data-stats-empty-state attribute")
	}
	progressMeter := htmlElementByTagAndClass(document, "progress", "stats-progress-meter")
	if progressMeter == nil {
		t.Fatalf("expected stats empty state to render a dedicated progress meter")
	}
	if htmlAttr(progressMeter, "style") != "" {
		t.Fatalf("expected progress meter tag to avoid inline style attributes under strict CSP, got %q", htmlAttr(progressMeter, "style"))
	}
}

func renderStatsPageWithStaleCycleData(t *testing.T) *html.Node {
	t.Helper()
	return renderStatsPageForCycleAnchorAge(t, "stats-stale-ui@example.com", 60)
}

// renderStatsPageForCycleAnchorAge seeds three period starts 30 days apart, the
// most recent of them startedDaysAgo back, and renders /stats. Three starts are
// two completed cycles, which is what unlocks the insights grid; with both
// cycles the same length the reference length is that length, so the caller
// controls staleness purely through the age of the latest anchor.
func renderStatsPageForCycleAnchorAge(t *testing.T, email string, startedDaysAgo int) *html.Node {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	const cycleLength = 30
	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -startedDaysAgo)
	logs := []models.DailyLog{
		{UserID: user.ID, Date: lastPeriodStart.AddDate(0, 0, -2*cycleLength), IsPeriod: true},
		{UserID: user.ID, Date: lastPeriodStart.AddDate(0, 0, -cycleLength), IsPeriod: true},
		{UserID: user.ID, Date: lastPeriodStart, IsPeriod: true},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	return renderStatsPageDocument(t, app, authCookie)
}

// renderStatsPageWithoutCompletedCycles keeps the empty-state fixture the
// progress-meter case needs: an onboarded account with a cycle anchor and no
// logged cycles at all.
func renderStatsPageWithoutCompletedCycles(t *testing.T) *html.Node {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-empty-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	return renderStatsPageDocument(t, app, authCookie)
}

func renderStatsPageDocument(t *testing.T, app *fiber.App, authCookie string) *html.Node {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}
