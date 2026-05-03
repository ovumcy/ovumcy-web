package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

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
		{UserID: user.ID, Date: currentCycleStart, IsPeriod: true, BBT: 36.40},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 1), BBT: 36.45},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 2), BBT: 36.50},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 3), BBT: 36.42},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 4), BBT: 36.43},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 5), BBT: 36.70},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 6), BBT: 36.72},
		{UserID: user.ID, Date: currentCycleStart.AddDate(0, 0, 7), BBT: 36.74},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create stats logs: %v", err)
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

	rendered := mustReadBodyString(t, response.Body)
	document := mustParseHTMLDocument(t, rendered)
	documentText := htmlDocumentText(document)

	for _, fragment := range []string{
		"Last cycle length",
		"Period length",
		"Current phase",
		"Prediction reliability",
		"Based on 3 completed cycles.",
		"Recent cycle factors",
		"Factors seen across variable cycles",
		"Recent cycle context",
		"Within a variable pattern",
		"Stress",
		"Top symptoms in your last cycle",
		"Symptom patterns",
	} {
		if !strings.Contains(documentText, fragment) {
			t.Fatalf("expected stats page to contain %q, got %q", fragment, documentText)
		}
	}
	if htmlElementByID(document, "bbt-chart") == nil {
		t.Fatalf("expected stats page to render BBT chart container")
	}
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-usage-goal-summary`, message: "expected stats usage-goal summary panel"},
		bodyStringMatch{fragment: "Current mode: Trying to conceive", message: "expected current usage-goal label on stats page"},
		bodyStringMatch{fragment: `id="cycle-chart"`, message: "expected cycle chart container"},
		bodyStringMatch{fragment: `role="img"`, message: "expected chart containers to expose image role"},
		bodyStringMatch{fragment: `aria-labelledby="stats-cycle-trend-title"`, message: "expected cycle chart accessible title"},
		bodyStringMatch{fragment: `aria-describedby="stats-cycle-trend-summary"`, message: "expected cycle chart summary reference"},
		bodyStringMatch{fragment: `id="stats-cycle-trend-summary"`, message: "expected cycle chart summary node"},
		bodyStringMatch{fragment: "3 completed cycles shown.", message: "expected cycle chart summary copy"},
		bodyStringMatch{fragment: `id="bbt-chart"`, message: "expected bbt chart container"},
		bodyStringMatch{fragment: `aria-labelledby="stats-bbt-title"`, message: "expected bbt chart accessible title"},
		bodyStringMatch{fragment: `aria-describedby="stats-bbt-summary stats-bbt-caption"`, message: "expected bbt chart summary reference"},
		bodyStringMatch{fragment: `id="stats-bbt-summary"`, message: "expected bbt chart summary node"},
		bodyStringMatch{fragment: "readings this cycle.", message: "expected bbt chart summary copy"},
		bodyStringMatch{fragment: "Calculations stay the same.", message: "expected stats usage-goal disclaimer"},
	)
}
