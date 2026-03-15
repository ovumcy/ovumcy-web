package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestDashboardEnglishRendersLocalizedPredictionDates(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-date-localization@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	lastPeriodStart := services.DateAtLocation(nowUTC.In(location), location).AddDate(0, 0, -8)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie+"; "+timezoneCookieName+"="+timezoneName)
	request.Header.Set(timezoneHeaderName, timezoneName)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)

	nextPeriodPattern := regexp.MustCompile(`(?s)<span data-dashboard-next-period>\s*[A-Z][a-z]{2} \d{1,2}, \d{4}\s*—\s*[A-Z][a-z]{2} \d{1,2}, \d{4}\s*</span>`)
	if !nextPeriodPattern.MatchString(rendered) {
		t.Fatalf("expected English-localized next period range in dashboard status line")
	}

	ovulationPattern := regexp.MustCompile(`Ovulation:\s*[A-Z][a-z]{2} \d{1,2}, \d{4}`)
	if !ovulationPattern.MatchString(rendered) {
		t.Fatalf("expected English-localized ovulation display date in dashboard status line")
	}
}

func TestDashboardEnglishRendersOvulationRangeForIrregularMode(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-irregular-ovulation@example.com", "StrongPass1", true)
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
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, Flow: models.FlowMedium}).Error; err != nil {
			t.Fatalf("create irregular cycle start %s: %v", day.Format("2006-01-02"), err)
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

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	ovulationRangePattern := regexp.MustCompile(`Ovulation:\s*[A-Z][a-z]{2} \d{1,2}, \d{4}\s*—\s*[A-Z][a-z]{2} \d{1,2}, \d{4}`)
	if !ovulationRangePattern.MatchString(rendered) {
		t.Fatalf("expected English-localized ovulation range in dashboard status line")
	}
	if !strings.Contains(rendered, `data-dashboard-prediction-explainer`) || !strings.Contains(rendered, "Irregular cycle mode uses ranges instead of exact prediction dates.") {
		t.Fatalf("expected shared irregular-range explanation note in dashboard, got %q", rendered)
	}
}

func TestDashboardEnglishRendersSharedSparsePredictionExplanationForIrregularMode(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-irregular-sparse@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	today := services.DateAtLocation(nowUTC, time.UTC)
	cycleStarts := []time.Time{
		today.AddDate(0, 0, -84),
		today.AddDate(0, 0, -56),
		today.AddDate(0, 0, -28),
	}
	for _, day := range cycleStarts {
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, Flow: models.FlowMedium}).Error; err != nil {
			t.Fatalf("create sparse irregular cycle start %s: %v", day.Format("2006-01-02"), err)
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"irregular_cycle":   true,
		"cycle_length":      29,
		"period_length":     5,
		"last_period_start": cycleStarts[len(cycleStarts)-1],
	}).Error; err != nil {
		t.Fatalf("update sparse irregular cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, "3 cycles are needed for a reliable range") {
		t.Fatalf("expected sparse irregular dashboard status copy, got %q", rendered)
	}
	if !strings.Contains(rendered, `data-dashboard-prediction-explainer`) || !strings.Contains(rendered, "Irregular cycle mode needs at least 3 completed cycles before Ovumcy can show steadier ranges.") {
		t.Fatalf("expected shared sparse irregular explanation note in dashboard, got %q", rendered)
	}
}
