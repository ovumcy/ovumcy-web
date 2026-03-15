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

	if !strings.Contains(rendered, "Complete 2 cycles to unlock insights. Start by entering the first day of your next period.") {
		t.Fatalf("expected empty-state message when chart payload is skipped, got %q", rendered)
	}
}

func TestStatsPageKeepsMetricGridHiddenAfterOneCompletedCycle(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-one-cycle@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	logs := []models.DailyLog{
		{UserID: user.ID, Date: today.AddDate(0, 0, -56), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: today.AddDate(0, 0, -28), IsPeriod: true, Flow: models.FlowMedium},
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

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, "You have 1 completed cycle. Complete one more cycle to unlock insights.") {
		t.Fatalf("expected one-cycle empty-state guidance, got %q", rendered)
	}
	if strings.Contains(rendered, `id="cycle-chart"`) {
		t.Fatalf("did not expect cycle chart before two completed cycles")
	}
	if strings.Contains(rendered, "Last cycle length") {
		t.Fatalf("did not expect metric cards before two completed cycles")
	}
}
