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
