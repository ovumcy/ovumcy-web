package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestCalendarDayEditorBBTFieldCarriesTheLanguageSeparator keeps the edit field
// on the same number format as the read-only summary: a Russian page shows 98,6,
// not the 98.60 the owner never typed, and names the separator for the field
// script so a blur does not turn it back.
func TestCalendarDayEditorBBTFieldCarriesTheLanguageSeparator(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-bbt-field-ru@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitFahrenheit,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}
	if err := database.Create(&models.DailyLog{
		UserID: user.ID,
		Date:   time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
		BBT:    new(37.0),
		Notes:  "tracked",
	}).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	for _, tc := range []struct{ language, want, separator string }{
		{"ru", "98,6", ","},
		{"en", "98.6", "."},
	} {
		request := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17?mode=edit", nil)
		request.Header.Set("Accept-Language", tc.language)
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		rendered := mustReadBodyString(t, response.Body)

		field := regexp.MustCompile(`(?s)<input[^>]*id="calendar-bbt"[^>]*>`).FindString(rendered)
		if field == "" {
			t.Fatalf("%s: no calendar-bbt field in %q", tc.language, rendered)
		}
		if !regexp.MustCompile(`value="` + regexp.QuoteMeta(tc.want) + `"`).MatchString(field) {
			t.Fatalf("%s: expected the field value %q, got %q", tc.language, tc.want, field)
		}
		if !regexp.MustCompile(`data-temperature-separator="` + regexp.QuoteMeta(tc.separator) + `"`).MatchString(field) {
			t.Fatalf("%s: expected the separator %q on the field, got %q", tc.language, tc.separator, field)
		}
	}
}

// TestDashboardBBTFieldCarriesTheLanguageSeparator is the same pin for the
// dashboard journal's field; the dashboard prints no read-only temperature, so
// the field is its only temperature surface.
func TestDashboardBBTFieldCarriesTheLanguageSeparator(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-bbt-field-ru@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitFahrenheit,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	if err := database.Create(&models.DailyLog{UserID: user.ID, Date: today, BBT: new(37.0)}).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Cookie", authCookie)
	sendRequestTimezoneForTest(request, testRequestTimezone)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	rendered := mustReadBodyString(t, response.Body)

	field := regexp.MustCompile(`(?s)<input[^>]*id="dashboard-bbt"[^>]*>`).FindString(rendered)
	if field == "" {
		t.Fatalf("no dashboard-bbt field in the page")
	}
	if !regexp.MustCompile(`value="98,6"`).MatchString(field) {
		t.Fatalf("expected the ru dashboard field to read 98,6, got %q", field)
	}
}
