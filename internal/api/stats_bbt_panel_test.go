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

func statsBBTPanelPage(t *testing.T, bbtOffsets []int) string {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-bbt-panel@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	cycleStart := today.AddDate(0, 0, -11)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": cycleStart,
		"track_bbt":         true,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}
	// Three completed cycles take the page past its empty state and the
	// first-cycle floor that withholds the marker.
	values := []float64{36.30, 36.40, 36.50, 36.35, 36.45, 36.40, 36.70, 36.75, 36.80}
	logs := []models.DailyLog{
		{UserID: user.ID, Date: cycleStart.AddDate(0, 0, -84), IsPeriod: true, CycleStart: true},
		{UserID: user.ID, Date: cycleStart.AddDate(0, 0, -56), IsPeriod: true, CycleStart: true},
		{UserID: user.ID, Date: cycleStart.AddDate(0, 0, -28), IsPeriod: true, CycleStart: true},
		{UserID: user.ID, Date: cycleStart, IsPeriod: true, CycleStart: true, BBT: &values[0]},
	}
	for _, offset := range bbtOffsets[1:] {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: cycleStart.AddDate(0, 0, offset), BBT: &values[offset]})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create logs: %v", err)
	}

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
	return mustReadBodyString(t, response.Body)
}

func TestStatsPageHintsAtTheReadingsAFewerThanFiveCycleStillNeeds(t *testing.T) {
	rendered := statsBBTPanelPage(t, []int{0, 1, 2})
	document := mustParseHTMLDocument(t, rendered)

	if htmlElementByID(document, "bbt-chart") != nil {
		t.Fatal("expected no chart below five readings")
	}
	hint := htmlFindElement(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-stats-bbt-hint") })
	if hint == nil {
		t.Fatal("expected a data-stats-bbt-hint panel below five readings")
	}
	text := htmlNodeText(hint)
	if !strings.Contains(text, "5") || !strings.Contains(text, "3") {
		t.Fatalf("expected the hint to name the required and recorded counts, got %q", text)
	}
}

func TestStatsPageDatesTheMarkerAndFlagsItsTableRow(t *testing.T) {
	rendered := statsBBTPanelPage(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8})
	document := mustParseHTMLDocument(t, rendered)

	if htmlFindElement(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-stats-bbt-hint") }) != nil {
		t.Fatal("expected no hint once the chart is drawn")
	}
	rows := htmlFindElements(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-bbt-marker-row") })
	if len(rows) != 1 {
		t.Fatalf("expected exactly one marked table row, got %d", len(rows))
	}
	summary := htmlNodeText(htmlElementByID(document, "stats-bbt-summary"))
	if !strings.Contains(summary, "Coverline 36.5°C") || strings.Contains(summary, "36.50") {
		t.Fatalf("expected the coverline at the table's precision and spacing, got %q", summary)
	}
	if !strings.Contains(summary, "Probable ovulation, ") {
		t.Fatalf("expected the marker to carry its date, got %q", summary)
	}
	if !strings.Contains(rendered, `aria-describedby="stats-bbt-summary stats-bbt-caption stats-bbt-table-toggle"`) {
		t.Fatal("expected the chart to point at its table toggle")
	}
}
