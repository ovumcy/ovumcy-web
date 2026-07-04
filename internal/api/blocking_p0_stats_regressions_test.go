package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

func TestStatsChartExcludesCycleEndingToday(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-trend@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	previousStart := today.AddDate(0, 0, -10)

	logs := []models.DailyLog{
		{
			UserID:     user.ID,
			Date:       previousStart,
			IsPeriod:   true,
			Flow:       models.FlowMedium,
			SymptomIDs: []uint{},
		},
		{
			UserID:     user.ID,
			Date:       today,
			IsPeriod:   true,
			Flow:       models.FlowMedium,
			SymptomIDs: []uint{},
		},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create period logs: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("Accept-Language", "en")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected stats status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stats body: %v", err)
	}

	rendered := string(body)
	chartPayload, err := extractStatsChartPayload(rendered)
	if err == nil {
		if len(chartPayload.Values) != 0 {
			t.Fatalf("expected no completed cycle points when latest cycle starts today, got %v", chartPayload.Values)
		}
		return
	}

	document := mustParseHTMLDocument(t, rendered)
	emptyState := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-empty-state")
	})
	if emptyState == nil {
		t.Fatal("expected stats empty-state container when chart payload is skipped")
	}
	if got := htmlAttr(emptyState, "data-stats-completed-cycles"); got != "0" {
		t.Fatalf("expected stats empty-state completed-cycles attribute %q, got %q", "0", got)
	}
}

func TestStatsPageKeepsMetricGridHiddenAfterOneCompletedCycle(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-one-cycle@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	logs := []models.DailyLog{
		{UserID: user.ID, Date: today.AddDate(0, 0, -56), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: today.AddDate(0, 0, -28), IsPeriod: true, Flow: models.FlowMedium},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create period logs: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("Accept-Language", "en")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected stats status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	emptyState := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-empty-state")
	})
	if emptyState == nil {
		t.Fatal("expected stats empty-state container after a single completed cycle")
	}
	if got := htmlAttr(emptyState, "data-stats-completed-cycles"); got != "1" {
		t.Fatalf("expected stats empty-state completed-cycles attribute %q, got %q", "1", got)
	}
	if htmlElementByID(document, "cycle-chart") != nil {
		t.Fatalf("did not expect cycle chart before two completed cycles")
	}
	if htmlElementByTagAndClass(document, "article", "stat-card") != nil {
		t.Fatalf("did not expect metric cards before two completed cycles")
	}
}
