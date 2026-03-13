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

func TestCalendarDayPanelReadonlySummaryShowsSavedBBT(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-bbt-summary@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitCelsius,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}

	logEntry := models.DailyLog{
		UserID: user.ID,
		Date:   time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
		BBT:    36.75,
		Notes:  "tracked",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, "BBT") {
		t.Fatalf("expected BBT label in calendar day summary, got %q", rendered)
	}
	if !strings.Contains(rendered, "36.75 °C") {
		t.Fatalf("expected saved BBT value in calendar day summary, got %q", rendered)
	}
}
