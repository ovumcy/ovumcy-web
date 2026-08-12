package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// TestDashboardStatusHeaderCarriesTheSegmentedRingForStableCycleContext pins the
// header the wave-2 design pass leaves on the dashboard: one status header,
// always rendered, whose ring is segmented once the cycle context is stable
// enough to draw the phases.
func TestDashboardStatusHeaderCarriesTheSegmentedRingForStableCycleContext(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-cycle-hero@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	lastPeriodStart := today.AddDate(0, 0, -2)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update stable cycle context: %v", err)
	}

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
	ring := dashboardElementByDataAttr(header, "data-dashboard-cycle-ring")
	if ring == nil {
		t.Fatal("expected the compact cycle ring inside the status header")
	}
	if got := htmlAttr(ring, "data-cycle-ring-segmented"); got != "true" {
		t.Fatalf("expected a segmented ring on a stable cycle context, got %q", got)
	}
	if dashboardElementByDataAttr(ring, "data-dashboard-cycle-day") == nil {
		t.Fatal("expected the cycle day inside the ring")
	}
	if dashboardElementByDataAttr(header, "data-dashboard-status-line") == nil {
		t.Fatal("expected the single status line inside the status header")
	}
	if htmlFindElement(header, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-next-period")
	}) == nil {
		t.Fatal("expected the next-period estimate in the status line")
	}
}

// TestDashboardStatusHeaderDropsTheNextPeriodSlotForAnOverdueCycle pins the
// structural half of the withheld window: once the running cycle is past the
// reference length by more than a week, the status line carries no next-period
// estimate at all — not a softened one — while the late-cycle notice below it,
// which is what the state actually justifies saying, still renders.
func TestDashboardStatusHeaderDropsTheNextPeriodSlotForAnOverdueCycle(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-overdue-window@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	// Cycle day 37 against the 28-day reference: past 28 + 7.
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": today.AddDate(0, 0, -36),
	}).Error; err != nil {
		t.Fatalf("update overdue cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header for an overdue cycle")
	}
	if htmlFindElement(header, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-next-period")
	}) != nil {
		t.Fatal("did not expect a next-period estimate for a cycle past the late threshold")
	}
	if dashboardElementByDataAttr(header, "data-dashboard-next-period-paused") == nil {
		t.Fatal("expected the withheld-estimate line in place of the next-period slot")
	}
	if dashboardElementByDataAttr(header, "data-dashboard-reminder-banner") != nil {
		t.Fatal("did not expect a reminder banner summarizing a withheld estimate")
	}
	warnings := dashboardElementByDataAttr(header, "data-dashboard-cycle-warnings")
	if warnings == nil {
		t.Fatal("expected the cycle warnings block for an overdue cycle")
	}
	if dashboardElementByDataAttr(warnings, "data-dashboard-cycle-day-warning") == nil {
		t.Fatal("expected the late-cycle notice to keep carrying the state")
	}
	if dashboardElementByDataAttr(header, "data-dashboard-cycle-day") == nil {
		t.Fatal("expected the cycle day to stay visible for an overdue cycle")
	}
}

// TestDashboardStatusHeaderDropsRingSegmentsWhenPredictionsAreDisabled is the
// other half: the header still renders in unpredictable mode, but nothing draws
// phase segments the account's data cannot support.
func TestDashboardStatusHeaderDropsRingSegmentsWhenPredictionsAreDisabled(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-cycle-hero-disabled@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":        28,
		"period_length":       5,
		"last_period_start":   today.AddDate(0, 0, -4),
		"unpredictable_cycle": true,
	}).Error; err != nil {
		t.Fatalf("update unpredictable cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header in unpredictable mode")
	}
	ring := dashboardElementByDataAttr(header, "data-dashboard-cycle-ring")
	if ring == nil {
		t.Fatal("expected the compact cycle ring in unpredictable mode")
	}
	if got := htmlAttr(ring, "data-cycle-ring-segmented"); got != "false" {
		t.Fatalf("did not expect a segmented ring in unpredictable mode, got %q", got)
	}
	if htmlFindElement(ring, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-cycle-hero-segment")
	}) != nil {
		t.Fatal("did not expect phase segments in unpredictable mode")
	}
}
