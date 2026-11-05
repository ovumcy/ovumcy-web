package api

import (
	"fmt"
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
	wantCaption := fmt.Sprintf(messageForKey(t, "stats.cycle_stack_count"), 4, 6)
	if got := strings.Join(strings.Fields(htmlNodeText(count)), " "); got != wantCaption {
		t.Fatalf("expected the caption to name 4 shown of 6 as %q, got %q", wantCaption, got)
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

	// The rows draw the same phase cells, so the swatches are looked up inside
	// the legend node, never in the whole page.
	legend := htmlFindElement(mustParseHTMLDocument(t, body), func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-legend") })
	if legend == nil {
		t.Fatal("expected a legend explaining the stack's colours")
	}
	for _, swatch := range []struct{ attribute, value string }{{"data-phase", "ovulation"}, {"data-fertile", "true"}} {
		found := htmlFindElement(legend, func(node *html.Node) bool { return htmlAttr(node, swatch.attribute) == swatch.value })
		if found == nil {
			t.Fatalf("expected the legend to carry a %s=%q swatch when inferred phases are on", swatch.attribute, swatch.value)
		}
	}

	wantNote := messageForKey(t, "stats.cycle_stack_legend_estimate") + " " + messageForKey(t, "medical.disclaimer")
	note := legendFollowingNote(legend)
	if note == nil {
		t.Fatal("expected the estimate note to follow the legend")
	}
	if got := strings.Join(strings.Fields(htmlNodeText(note)), " "); got != wantNote {
		t.Fatalf("legend note = %q, want the estimate qualifier and the shared disclaimer %q", got, wantNote)
	}

	plain := mustParseHTMLDocument(t, statsBodyForCyclePattern(t, "stats-cycle-stack-unphased@example.com", 28, 3))
	plainLegend := htmlFindElement(plain, func(node *html.Node) bool { return htmlHasAttr(node, "data-cycle-stack-legend") })
	if plainLegend == nil {
		t.Fatal("expected a legend without inferred phases too")
	}
	if note := legendFollowingNote(plainLegend); note != nil {
		t.Fatalf("the estimate note belongs to inferred phases and must not render without them, got %q", htmlNodeText(note))
	}
}

// legendFollowingNote is the paragraph element directly after the legend list,
// or nil when the next element is not a paragraph.
func legendFollowingNote(legend *html.Node) *html.Node {
	for sibling := legend.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type != html.ElementNode {
			continue
		}
		if sibling.Data == "p" {
			return sibling
		}
		return nil
	}
	return nil
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

// TestStatsIrregularAccountStaysBehindTheThreeCycleInsightsFloor pins the
// shared three-completed-cycle floor on /stats for an irregular-cycle account,
// not the irregular verdict itself: with fewer than three completed cycles the
// page is the empty state, so the current-phase card's fertile status and
// window line and the cycle stack's fertile cells are not rendered at all. Both
// accounts are on cycle day 12 of a 28-day pattern with phases switched on; the
// three-cycle twin is the positive control and must render the dynamic fertile
// status and window hooks the thin account must not. The legend swatch for the
// ovulation phase is static markup present whenever the stack is, so it is not
// a claim hook.
func TestStatsIrregularAccountStaysBehindTheThreeCycleInsightsFloor(t *testing.T) {
	claimHooks := []string{
		`data-fertile-window`,
		`data-fertility-status="fertile"`,
		`data-fertile="true"`,
	}
	controlHooks := []string{
		`data-fertile-window`,
		`data-fertility-status="fertile"`,
	}
	render := func(t *testing.T, email string, completedCycles int) string {
		t.Helper()

		app, database := newOnboardingTestApp(t)
		user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
		authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

		today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
		var logs []models.DailyLog
		var latestStart time.Time
		for i := completedCycles; i >= 0; i-- {
			latestStart = today.AddDate(0, 0, -11-28*i)
			logs = append(logs, models.DailyLog{UserID: user.ID, Date: latestStart, IsPeriod: true, CycleStart: true})
		}
		if err := database.Create(&logs).Error; err != nil {
			t.Fatalf("seed period logs: %v", err)
		}
		if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"last_period_start":      latestStart,
			"irregular_cycle":        true,
			"show_historical_phases": true,
		}).Error; err != nil {
			t.Fatalf("update user: %v", err)
		}

		request := httptest.NewRequest(http.MethodGet, "/stats", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		return mustReadBodyString(t, response.Body)
	}

	control := render(t, "stats-irregular-three-cycles@example.com", 3)
	for _, hook := range controlHooks {
		if !strings.Contains(control, hook) {
			t.Fatalf("control: an irregular account with three completed cycles did not render %s", hook)
		}
	}

	thin := render(t, "stats-irregular-two-cycles@example.com", 2)
	if !strings.Contains(thin, `data-stats-empty-state data-stats-completed-cycles="2"`) {
		t.Fatal("expected the empty state for an irregular account with two completed cycles")
	}
	for _, hook := range claimHooks {
		if strings.Contains(thin, hook) {
			t.Errorf("/stats for an irregular account with two completed cycles rendered %s", hook)
		}
	}
}
