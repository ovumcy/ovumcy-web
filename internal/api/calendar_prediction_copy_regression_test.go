package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestCalendarEnglishRendersSharedPredictionExplanation(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "calendar-prediction-copy@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	today := services.DateAtLocation(nowUTC, time.UTC)

	cycleStarts := []time.Time{
		today.AddDate(0, 0, -84),
		today.AddDate(0, 0, -58),
		today.AddDate(0, 0, -26),
		today.AddDate(0, 0, -5),
	}
	for _, day := range cycleStarts {
		if err := database.Create(&models.DailyLog{
			UserID:   user.ID,
			Date:     day,
			IsPeriod: true,
			Flow:     models.FlowMedium,
		}).Error; err != nil {
			t.Fatalf("create cycle start %s: %v", day.Format("2006-01-02"), err)
		}
	}
	factorLogs := map[time.Time][]string{
		cycleStarts[0].AddDate(0, 0, 2): {models.CycleFactorStress},
		cycleStarts[1].AddDate(0, 0, 2): {models.CycleFactorTravel},
		cycleStarts[2].AddDate(0, 0, 2): {models.CycleFactorStress},
	}
	for day, keys := range factorLogs {
		if err := database.Create(&models.DailyLog{
			UserID:          user.ID,
			Date:            day,
			CycleFactorKeys: keys,
		}).Error; err != nil {
			t.Fatalf("create factor log %s: %v", day.Format("2006-01-02"), err)
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"irregular_cycle":   true,
		"cycle_length":      29,
		"period_length":     5,
		"last_period_start": cycleStarts[len(cycleStarts)-1],
	}).Error; err != nil {
		t.Fatalf("update user irregular cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar?month="+today.Format("2006-01")+"&day="+today.Format("2006-01-02"), nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, `data-calendar-prediction-explainer`) {
		t.Fatalf("expected calendar prediction explainer block, got %q", rendered)
	}
	if !strings.Contains(rendered, "Irregular cycle mode uses ranges instead of exact prediction dates.") {
		t.Fatalf("expected shared irregular range note, got %q", rendered)
	}
	if !strings.Contains(rendered, "Recent tags can add context when timing feels less steady.") {
		t.Fatalf("expected shared factor context note, got %q", rendered)
	}
}
