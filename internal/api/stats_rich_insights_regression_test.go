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
		bodyStringMatch{fragment: `data-usage-goal-summary`, message: "expected stats usage-goal summary panel hook"},
		bodyStringMatch{fragment: `data-stats-prediction-explainer`, message: "expected stats prediction explainer hook"},
		bodyStringMatch{fragment: `data-stats-factor-context`, message: "expected stats factor context hook"},
	)
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

func statsKPICards(document *html.Node) []*html.Node {
	return htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "article" && htmlHasClass(node, "stat-card")
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
			if node.Type == html.ElementNode && htmlHasClass(node, "stat-card") {
				t.Fatal("reliability context line must not sit inside a KPI stat card")
			}
		}

		linePosition := strings.Index(rendered, "data-prediction-reliability")
		cardPosition := strings.Index(rendered, "stat-card")
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
