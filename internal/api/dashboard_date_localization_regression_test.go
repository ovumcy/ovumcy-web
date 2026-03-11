package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
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

	nextPeriodPattern := regexp.MustCompile(`(?s)<span data-dashboard-next-period>\s*([A-Z][a-z]{2} \d{1,2})\s*</span>`)
	if !nextPeriodPattern.MatchString(rendered) {
		t.Fatalf("expected English-localized next period short date in dashboard status line")
	}

	ovulationPattern := regexp.MustCompile(`Ovulation:\s*[A-Z][a-z]{2} \d{1,2}`)
	if !ovulationPattern.MatchString(rendered) {
		t.Fatalf("expected English-localized ovulation short date in dashboard status line")
	}
}
