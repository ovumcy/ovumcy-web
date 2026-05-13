package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestSettingsCycleUsesRequestTimezoneForLastPeriodStartValidation(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "settings-cycle-tz@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location).Format("2006-01-02")

	form := url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"5"},
		"last_period_start": {localToday},
	}
	request := httptest.NewRequest(http.MethodPost, "/settings/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings cycle request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read settings cycle response body: %v", err)
	}
	if !strings.Contains(string(body), "status-ok") {
		t.Fatalf("expected success status markup for timezone-aware last_period_start, got %q", string(body))
	}

	updatedUser := models.User{}
	if err := database.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updatedUser.LastPeriodStart == nil {
		t.Fatal("expected persisted last_period_start")
	}

	// LastPeriodStart is a date-only value (UTC-midnight on disk per migration
	// 019). DateAtLocation/.In(location) would mis-shift it across DST and
	// negative-offset zones; read the calendar components directly instead —
	// see the docblock on services.DateAtLocation.
	savedLocalDay := services.CalendarDayKey(*updatedUser.LastPeriodStart)
	if savedLocalDay != localToday {
		t.Fatalf("expected saved last_period_start %q, got %q", localToday, savedLocalDay)
	}
}
