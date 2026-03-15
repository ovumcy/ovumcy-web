package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
	"golang.org/x/net/html"
)

func TestStatsPageShowsUnknownPhaseWhenCycleDataIsStale(t *testing.T) {
	documentText := htmlDocumentText(renderStatsPageWithStaleCycleData(t))
	if !strings.Contains(documentText, "Complete 2 cycles to unlock insights. Start by entering the first day of your next period.") {
		t.Fatalf("expected updated empty-state guidance before the first completed cycle")
	}
}

func TestStatsPageEmptyStateUsesDedicatedProgressMeterWithoutInlineStyle(t *testing.T) {
	document := renderStatsPageWithStaleCycleData(t)
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

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-stale-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}
