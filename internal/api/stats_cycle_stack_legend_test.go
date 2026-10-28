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

// The cycle stack draws the latest few completed cycles with decorative bands.
// These tests pin the words that stand in for the drawing: how many cycles are
// shown out of how many exist, what the colours mean, and a per-row summary a
// reader who cannot see the bands is given. Hooks, not sentences: copy edits
// belong to the catalogue.
func TestStatsCycleStackSaysHowManyCyclesItShowsAndWhatTheColoursMean(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-legend@example.com", 30, 6)
	document := mustParseHTMLDocument(t, body)

	count := htmlFindElement(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-count") })
	if count == nil {
		t.Fatal("expected a caption stating how many cycles the stack shows")
	}
	if text := htmlNodeText(count); !strings.Contains(text, "4") || !strings.Contains(text, "6") {
		t.Fatalf("expected the caption to name 4 shown of 6, got %q", text)
	}
	if !strings.Contains(body, "data-cycle-stack-hidden-note") {
		t.Fatal("expected a note that older cycles are left out when some are")
	}

	legend := htmlFindElement(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-legend") })
	if legend == nil {
		t.Fatal("expected a legend explaining the stack's colours")
	}
	items := htmlFindElements(legend, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-legend-item") })
	if len(items) < 2 {
		t.Fatalf("expected the legend to list at least the period and the logged mark, got %d items", len(items))
	}

	rows := htmlFindElements(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-row") })
	summaries := htmlFindElements(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-row-summary") })
	if len(rows) != 4 || len(summaries) != len(rows) {
		t.Fatalf("expected one text summary per row (%d rows), got %d summaries", len(rows), len(summaries))
	}
	for index, summary := range summaries {
		if strings.TrimSpace(htmlNodeText(summary)) == "" {
			t.Fatalf("row %d: expected a non-empty text summary", index)
		}
	}
}

// TestStatsCycleStackQualifiesInferredPhasesInItsLegend pins the medical-safety
// half: the phase and fertile-window swatches exist only with inferred phases
// on, and then the legend says they are estimates beside the shared disclaimer.
func TestStatsCycleStackQualifiesInferredPhasesInItsLegend(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-cycle-stack-phased@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	logs := make([]models.DailyLog, 0, 4)
	var latestStart time.Time
	for i := 3; i >= 0; i-- {
		latestStart = today.AddDate(0, 0, -8-28*i)
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: latestStart, IsPeriod: true, CycleStart: true})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start":      latestStart,
		"show_historical_phases": true,
	}).Error; err != nil {
		t.Fatalf("update user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)

	if !strings.Contains(body, `data-phase="ovulation"`) || !strings.Contains(body, `data-fertile="true"`) {
		t.Fatal("expected the legend to explain the phase and fertile-window colours when inferred phases are on")
	}
	if !strings.Contains(body, "method of contraception") || !strings.Contains(body, "estimated from cycle lengths") {
		t.Fatal("expected the estimate qualifier and the shared disclaimer beside the phase legend")
	}

	plain := statsBodyForCyclePattern(t, "stats-cycle-stack-unphased@example.com", 28, 3)
	if strings.Contains(plain, "estimated from cycle lengths") {
		t.Fatal("the estimate note belongs to inferred phases and must not render without them")
	}
}

func TestStatsCycleStackHasNoHiddenNoteWhenEveryCycleIsShown(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-all-shown@example.com", 30, 3)

	if !strings.Contains(body, "data-cycle-stack-count") {
		t.Fatal("expected the caption even when every cycle is shown")
	}
	if strings.Contains(body, "data-cycle-stack-hidden-note") {
		t.Fatal("the hidden-cycles note must not render when nothing is left out")
	}
}
