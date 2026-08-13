package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// TestStatsPageRendersRichInsightsAndBBTChart guards the structural contracts
// the stats page exposes for accessibility, charting, and HTMX hooks. It
// intentionally avoids asserting human-readable copy: those fragments are
// (a) covered at the service layer by stats_service_test.go for the
// underlying computations, and (b) covered for rendered visible text by the
// Playwright spec e2e/stats-factor-context.spec.ts. Keeping copy assertions
// here as well only created copy-edit churn without catching new defects.
func TestStatsPageRendersRichInsightsAndBBTChart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-rich-insights@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	customSymptoms := []models.SymptomType{
		{UserID: user.ID, Name: "Headache", Icon: "H", Color: "#111111"},
		{UserID: user.ID, Name: "Cramps", Icon: "C", Color: "#222222"},
		{UserID: user.ID, Name: "Acne", Icon: "A", Color: "#333333"},
	}
	if err := database.Create(&customSymptoms).Error; err != nil {
		t.Fatalf("create custom symptoms: %v", err)
	}
	symptomByName := map[string]uint{
		"Headache": customSymptoms[0].ID,
		"Cramps":   customSymptoms[1].ID,
		"Acne":     customSymptoms[2].ID,
	}

	// Anchor all dates relative to time.Now() so the test stays within the
	// 90-day cycle-factor context window regardless of when CI runs it.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentCycleStart := today.AddDate(0, 0, -8)
	period3Start := currentCycleStart.AddDate(0, 0, -28)
	period2Start := currentCycleStart.AddDate(0, 0, -56)
	period1Start := currentCycleStart.AddDate(0, 0, -84)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": currentCycleStart,
		"track_bbt":         true,
		"irregular_cycle":   true,
		"usage_goal":        models.UsageGoalTrying,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	logs := []models.DailyLog{
		{UserID: user.ID, Date: period1Start, IsPeriod: true},
		{UserID: user.ID, Date: period1Start.AddDate(0, 0, 1), CycleFactorKeys: []string{models.CycleFactorStress}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: period1Start.AddDate(0, 0, 4), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: period2Start, IsPeriod: true},
		{UserID: user.ID, Date: period2Start.AddDate(0, 0, 1), CycleFactorKeys: []string{models.CycleFactorTravel}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: period2Start.AddDate(0, 0, 4), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: period3Start, IsPeriod: true},
		{UserID: user.ID, Date: period3Start.AddDate(0, 0, 1), SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: period3Start.AddDate(0, 0, 2), CycleFactorKeys: []string{models.CycleFactorStress}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: period3Start.AddDate(0, 0, 4), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: period3Start.AddDate(0, 0, 6), SymptomIDs: []uint{symptomByName["Acne"]}},
		{UserID: user.ID, Date: currentCycleStart, IsPeriod: true, BBT: new(36.40)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 1), BBT: new(36.45)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 2), BBT: new(36.50)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 3), BBT: new(36.42)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 4), BBT: new(36.43)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 5), BBT: new(36.70)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 6), BBT: new(36.72)},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 7), BBT: new(36.74)},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create stats logs: %v", err)
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

	rendered := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, rendered)

	// Charting accessibility contract — chart containers and their summary
	// nodes must remain wired up so screen readers can describe each chart.
	if htmlElementByID(document, "cycle-chart") == nil {
		t.Fatal("expected stats page to render cycle chart container")
	}
	if htmlElementByID(document, "bbt-chart") == nil {
		t.Fatal("expected stats page to render BBT chart container")
	}
	if htmlElementByID(document, "stats-cycle-trend-summary") == nil {
		t.Fatal("expected cycle chart summary node id=stats-cycle-trend-summary")
	}
	if htmlElementByID(document, "stats-bbt-summary") == nil {
		t.Fatal("expected bbt chart summary node id=stats-bbt-summary")
	}

	// data-* and ARIA hooks Playwright + assistive tech depend on.
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `role="img"`, message: "expected chart containers to expose image role"},
		bodyStringMatch{fragment: `aria-labelledby="stats-cycle-trend-title"`, message: "expected cycle chart accessible title"},
		bodyStringMatch{fragment: `aria-describedby="stats-cycle-trend-summary"`, message: "expected cycle chart summary reference"},
		bodyStringMatch{fragment: `aria-labelledby="stats-bbt-title"`, message: "expected bbt chart accessible title"},
		bodyStringMatch{fragment: `aria-describedby="stats-bbt-summary stats-bbt-caption"`, message: "expected bbt chart summary reference"},
		bodyStringMatch{fragment: `data-stats-prediction-explainer`, message: "expected stats prediction explainer hook"},
		bodyStringMatch{fragment: `data-stats-factor-context`, message: "expected stats factor context hook"},
	)

	assertStatsBBTTableTwin(t, document)
}

// assertStatsBBTTableTwin pins the BBT chart's text equivalent. The canvas the
// chart draws into is aria-hidden and the crosshair readout is a pointer
// affordance, so the table is the only path to a per-day value that does not
// require sight and a mouse: it must exist, carry a caption, and hold exactly
// one row per plotted point — a shorter table is a value reachable by hovering
// only. Structural contracts only; the rendered numbers are the subject of
// e2e/stats-insights.spec.ts.
func assertStatsBBTTableTwin(t *testing.T, document *html.Node) {
	t.Helper()

	chart := htmlElementByID(document, "bbt-chart")
	if chart == nil {
		t.Fatal("expected stats page to render BBT chart container")
	}
	for _, attribute := range []string{"data-chart-hover", "data-hover-day-label", "data-hover-empty-text"} {
		if !htmlHasAttr(chart, attribute) {
			t.Fatalf("expected the BBT chart to declare %s for the crosshair readout", attribute)
		}
	}

	var payload struct {
		Labels     []string `json:"labels"`
		Dates      []string `json:"dates"`
		ValueTexts []string `json:"valueTexts"`
	}
	if err := json.Unmarshal([]byte(htmlAttr(chart, "data-chart")), &payload); err != nil {
		t.Fatalf("decode BBT chart payload: %v", err)
	}
	if len(payload.Labels) == 0 {
		t.Fatal("expected the BBT chart payload to carry labels")
	}
	if len(payload.Dates) != len(payload.Labels) {
		t.Fatalf("expected one crosshair date per chart point (%d), got %d", len(payload.Labels), len(payload.Dates))
	}
	if len(payload.ValueTexts) != len(payload.Labels) {
		t.Fatalf("expected one rendered reading per chart point (%d), got %d", len(payload.Labels), len(payload.ValueTexts))
	}

	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "details" && htmlHasAttr(node, "data-bbt-table-disclosure")
	}) == nil {
		t.Fatal("expected the BBT table twin to sit behind a data-bbt-table-disclosure disclosure")
	}

	table := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "table" && htmlHasAttr(node, "data-bbt-table")
	})
	if table == nil {
		t.Fatal("expected a data-bbt-table table under the BBT chart")
	}
	if htmlFindElement(table, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "caption"
	}) == nil {
		t.Fatal("expected the BBT table twin to carry a caption")
	}

	rows := htmlFindElements(table, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-bbt-table-row")
	})
	if len(rows) != len(payload.Labels) {
		t.Fatalf("expected one table row per chart point (%d), got %d", len(payload.Labels), len(rows))
	}
	for index, row := range rows {
		if got, want := htmlAttr(row, "data-cycle-day"), strconv.Itoa(index+1); got != want {
			t.Fatalf("row %d: expected data-cycle-day=%q, got %q", index, want, got)
		}
		if htmlFindElement(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasAttr(node, "data-bbt-table-value")
		}) == nil {
			t.Fatalf("row %d: expected a data-bbt-table-value cell", index)
		}
	}
}

// TestStatsPageRendersHistoryStatements pins the structural contract of the
// statements section: the hook that marks it, the heading key, and one row per
// statement carrying its kind, its chosen catalogue key and the numbers the
// sentence prints. Copy stays out of here — the phrasing is the catalogue's and
// the Playwright spec's subject; what must not drift is the pair of attributes
// a spec addresses a statement by.
func TestStatsPageRendersHistoryStatements(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-history-statements@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{UserID: user.ID, Name: "Headache", Icon: "H", Color: "#111111"}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	// Anchor relative to today so the completed-cycle count, which is measured
	// against the live clock, holds whenever CI runs this.
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	currentCycleStart := today.AddDate(0, 0, -8)
	// Cycle lengths 31, 31, 27, 27 — the earlier half of the window runs four
	// days longer than the recent half, so the trend statement is a "shorter"
	// one and carries its window detail line rather than the steady wording.
	starts := []time.Time{
		currentCycleStart.AddDate(0, 0, -116),
		currentCycleStart.AddDate(0, 0, -85),
		currentCycleStart.AddDate(0, 0, -54),
		currentCycleStart.AddDate(0, 0, -27),
		currentCycleStart,
	}

	logs := make([]models.DailyLog, 0, len(starts)*2)
	for index, start := range starts {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: start, IsPeriod: true, Flow: models.FlowMedium})
		if index == len(starts)-1 {
			continue
		}
		// Cycle day 22 sits past the ovulation day of every seeded cycle, so
		// the symptom lands in the luteal phase of all four closed cycles.
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: start.AddDate(0, 0, 21), SymptomIDs: []uint{symptom.ID}})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create statement logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": currentCycleStart,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
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

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	section := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-statements")
	})
	if section == nil {
		t.Fatal("expected the stats page to render the data-stats-statements section")
	}

	heading := htmlFindElement(section, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-statements-heading")
	})
	if heading == nil {
		t.Fatal("expected the statements section to carry its heading hook")
	}
	if key := htmlAttr(heading, "data-heading-key"); key != "stats.statements_title" {
		t.Errorf("expected the heading to name its catalogue key, got %q", key)
	}

	rows := htmlFindElements(section, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-statement")
	})
	if len(rows) < 2 {
		t.Fatalf("expected the trend and recurrence statements to render, got %d rows", len(rows))
	}

	if kind := htmlAttr(rows[0], "data-stats-statement"); kind != "cycle_length_trend" {
		t.Fatalf("expected the cycle-length trend to lead the section, got %q", kind)
	}
	if key := htmlAttr(rows[0], "data-statement-key"); key != "stats.statement_cycle_trend_shorter" {
		t.Errorf("expected the shorter-trend key on the leading statement, got %q", key)
	}
	if direction := htmlAttr(rows[0], "data-statement-direction"); direction != "shorter" {
		t.Errorf("expected the leading statement to name its direction, got %q", direction)
	}

	phases := map[string]struct{}{"menstrual": {}, "follicular": {}, "ovulation": {}, "luteal": {}}
	sawRecurrence := false
	for _, row := range rows[1:] {
		if htmlAttr(row, "data-stats-statement") != "symptom_phase_recurrence" {
			t.Fatalf("expected recurrence statements after the trend, got %q", htmlAttr(row, "data-stats-statement"))
		}
		sawRecurrence = true
		if key := htmlAttr(row, "data-statement-key"); key != "stats.statement_symptom_recurrence" {
			t.Errorf("expected the recurrence key, got %q", key)
		}
		// Fertility is a status, never a phase: a recurrence row may only ever
		// name one of the four cycle phases.
		if _, ok := phases[htmlAttr(row, "data-statement-phase")]; !ok {
			t.Errorf("expected a cycle phase on the recurrence row, got %q", htmlAttr(row, "data-statement-phase"))
		}
		if htmlAttr(row, "data-statement-count") == "" || htmlAttr(row, "data-statement-total") == "" {
			t.Errorf("expected the recurrence row to carry both of its numbers, got %q of %q",
				htmlAttr(row, "data-statement-count"), htmlAttr(row, "data-statement-total"))
		}
	}
	if !sawRecurrence {
		t.Error("expected at least one symptom-by-phase recurrence statement")
	}
}

// renderStatsInsightsPage drives a stats page render for an owner with four
// recorded period starts (three completed cycles), so the insights branch of
// stats.html is exercised. When unpredictableCycle is set, predictions are off
// and the reliability signal is gated away, which is the negative half of
// TestStatsPageAttachesPredictionReliabilityToPredictionContext.
func renderStatsInsightsPage(t *testing.T, email string, unpredictableCycle bool) (string, *html.Node) {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	currentCycleStart := today.AddDate(0, 0, -8)
	logs := []models.DailyLog{
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, -84), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, -56), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, -28), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: currentCycleStart, IsPeriod: true, Flow: models.FlowMedium},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create period logs: %v", err)
	}
	updates := map[string]any{"last_period_start": currentCycleStart}
	if unpredictableCycle {
		updates["unpredictable_cycle"] = true
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
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

	rendered := mustReadBodyString(t, response.Body)
	return rendered, mustParseHTMLDocument(t, rendered)
}

// TestStatsPageStacksCompletedCyclesOnOneAxis pins the cycle stack's structural
// contract: one row per completed cycle, every row carrying the SAME number of
// day cells, and the cells past a row's own length marked as outside it. The
// shared cell count is the whole mechanism — it is what makes a longer cycle
// draw longer without a single computed width — so a row that stopped emitting
// its surplus cells would silently turn the comparison into four equal bars.
func TestStatsPageStacksCompletedCyclesOnOneAxis(t *testing.T) {
	_, document := renderStatsInsightsPage(t, "stats-cycle-stack@example.com", false)

	stack := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-stats-cycle-stack")
	})
	if stack == nil {
		t.Fatal("expected the cycle stack section for an owner with completed cycles")
	}

	rows := htmlFindElements(stack, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-cycle-stack-row")
	})
	if len(rows) != 3 {
		t.Fatalf("expected one row per completed cycle (3), got %d", len(rows))
	}

	axisDays := 0
	for index, row := range rows {
		cells := htmlFindElements(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasAttr(node, "data-cycle-stack-day")
		})
		if len(cells) == 0 {
			t.Fatalf("row %d rendered no day cells", index)
		}
		if index == 0 {
			axisDays = len(cells)
		} else if len(cells) != axisDays {
			t.Fatalf("row %d carries %d cells against the axis's %d — the rows share one axis", index, len(cells), axisDays)
		}
		if got := htmlAttr(cells[0], "data-cycle-stack-day"); got != "1" {
			t.Fatalf("row %d starts at day %q, expected the ribbon to run from day 1", index, got)
		}

		inCycle := 0
		for _, cell := range cells {
			if htmlAttr(cell, "data-in-cycle") == "true" {
				inCycle++
			}
		}
		if inCycle == 0 {
			t.Fatalf("row %d marks no day as inside its own cycle", index)
		}
		if inCycle > axisDays {
			t.Fatalf("row %d marks %d days inside a %d-day axis", index, inCycle, axisDays)
		}
	}
}

func statsKPICards(document *html.Node) []*html.Node {
	return htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "article" && htmlHasClass(node, "card-dense")
	})
}

func statsPredictionReliabilityLine(document *html.Node) *html.Node {
	return htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-prediction-reliability")
	})
}

// TestStatsPageAttachesPredictionReliabilityToPredictionContext pins where the
// prediction-reliability signal lives: it is context attached to the
// prediction, not a fourth metric standing beside the three KPI numbers. The
// KPI row therefore stays at three stat cards whether or not the signal is
// shown, and the signal renders as a context line carrying the chosen
// reliability label key on data-prediction-reliability. Its gate is unchanged
// — the line appears exactly when the service sets ShowPredictionReliability,
// so the predictions-off owner sees the same three cards and no line.
func TestStatsPageAttachesPredictionReliabilityToPredictionContext(t *testing.T) {
	reliabilityLabelKeys := map[string]struct{}{
		"stats.reliability.early":    {},
		"stats.reliability.building": {},
		"stats.reliability.stable":   {},
		"stats.reliability.variable": {},
	}

	t.Run("signal shown", func(t *testing.T) {
		rendered, document := renderStatsInsightsPage(t, "stats-reliability-context@example.com", false)

		if cards := statsKPICards(document); len(cards) != 3 {
			t.Errorf("expected the KPI row to hold 3 stat cards, got %d", len(cards))
		}

		reliability := statsPredictionReliabilityLine(document)
		if reliability == nil {
			t.Fatal("expected stats page to render the data-prediction-reliability context line")
		}
		labelKey := htmlAttr(reliability, "data-prediction-reliability")
		if _, ok := reliabilityLabelKeys[labelKey]; !ok {
			t.Fatalf("expected a reliability label key on the context line, got %q", labelKey)
		}

		for node := reliability.Parent; node != nil; node = node.Parent {
			if node.Type == html.ElementNode && htmlHasClass(node, "card-dense") {
				t.Fatal("reliability context line must not sit inside a KPI stat card")
			}
		}

		linePosition := strings.Index(rendered, "data-prediction-reliability")
		cardPosition := strings.Index(rendered, "card-dense")
		if linePosition < 0 || cardPosition < 0 || linePosition > cardPosition {
			t.Fatalf("expected the reliability context line to precede the KPI row (line at %d, first card at %d)", linePosition, cardPosition)
		}
	})

	t.Run("signal gated off", func(t *testing.T) {
		_, document := renderStatsInsightsPage(t, "stats-reliability-gated@example.com", true)

		if cards := statsKPICards(document); len(cards) != 3 {
			t.Fatalf("expected the KPI row to hold 3 stat cards with predictions off, got %d", len(cards))
		}
		if statsPredictionReliabilityLine(document) != nil {
			t.Fatal("did not expect a reliability context line while predictions are disabled")
		}
	})
}
