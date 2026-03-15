package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
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

	currentCycleStart := time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": currentCycleStart,
		"track_bbt":         true,
		"irregular_cycle":   true,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	logs := []models.DailyLog{
		{UserID: user.ID, Date: time.Date(2025, time.December, 4, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		{UserID: user.ID, Date: time.Date(2025, time.December, 5, 0, 0, 0, 0, time.UTC), CycleFactorKeys: []string{models.CycleFactorStress}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: time.Date(2025, time.December, 8, 0, 0, 0, 0, time.UTC), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		{UserID: user.ID, Date: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC), CycleFactorKeys: []string{models.CycleFactorTravel}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		{UserID: user.ID, Date: time.Date(2026, time.January, 30, 0, 0, 0, 0, time.UTC), SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC), CycleFactorKeys: []string{models.CycleFactorStress}, SymptomIDs: []uint{symptomByName["Headache"]}},
		{UserID: user.ID, Date: time.Date(2026, time.February, 2, 0, 0, 0, 0, time.UTC), SymptomIDs: []uint{symptomByName["Cramps"]}},
		{UserID: user.ID, Date: time.Date(2026, time.February, 4, 0, 0, 0, 0, time.UTC), SymptomIDs: []uint{symptomByName["Acne"]}},
		{UserID: user.ID, Date: time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC), IsPeriod: true, BBT: 36.40},
		{UserID: user.ID, Date: time.Date(2026, time.February, 27, 0, 0, 0, 0, time.UTC), BBT: 36.45},
		{UserID: user.ID, Date: time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC), BBT: 36.50},
		{UserID: user.ID, Date: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), BBT: 36.42},
		{UserID: user.ID, Date: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC), BBT: 36.43},
		{UserID: user.ID, Date: time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), BBT: 36.70},
		{UserID: user.ID, Date: time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC), BBT: 36.72},
		{UserID: user.ID, Date: time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC), BBT: 36.74},
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

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
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
}
