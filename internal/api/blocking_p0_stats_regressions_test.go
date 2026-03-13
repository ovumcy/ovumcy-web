package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
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
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer response.Body.Close()

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

	if !strings.Contains(rendered, "Enter the start of your next period to complete your first cycle and unlock insights.") {
		t.Fatalf("expected empty-state message when chart payload is skipped, got %q", rendered)
	}
}
