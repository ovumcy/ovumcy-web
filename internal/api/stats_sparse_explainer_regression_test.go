package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestStatsPageExplainsTheIrregularSparseTierBelowTheInsightsThreshold pins
// the explainer for an irregular owner with two completed cycles. Irregular
// mode stays sparse below three completed cycles, and so does the insights
// tier, so the page holds its empty state for this owner; the explainer that
// says why must still render beside it rather than only inside the insights
// branch, which this owner never reaches.
func TestStatsPageExplainsTheIrregularSparseTierBelowTheInsightsThreshold(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-irregular-sparse@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentCycleStart := today.AddDate(0, 0, -8)
	previousStart := currentCycleStart.AddDate(0, 0, -28)
	firstStart := currentCycleStart.AddDate(0, 0, -56)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": currentCycleStart,
		"irregular_cycle":   true,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	logs := []models.DailyLog{
		{UserID: user.ID, Date: firstStart, IsPeriod: true},
		{UserID: user.ID, Date: previousStart, IsPeriod: true},
		{UserID: user.ID, Date: currentCycleStart, IsPeriod: true},
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
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-stats-empty-state data-stats-completed-cycles="2"`, message: "expected the empty state for two completed cycles, below the insights threshold"},
		bodyStringMatch{fragment: `data-stats-prediction-explainer data-explainer-key="prediction.explainer.irregular_sparse"`, message: "expected the irregular sparse explainer beside the empty state"},
	)
}
