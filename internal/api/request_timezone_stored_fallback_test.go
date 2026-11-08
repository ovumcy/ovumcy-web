package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

var storedTimezoneDashboardDatePattern = regexp.MustCompile(`data-dashboard-date="([0-9-]+)"`)

// The injected clock sits at 23:30 UTC on 2026-11-07: Pacific/Kiritimati
// (UTC+14) is already on 2026-11-08 and Pacific/Pago_Pago (UTC-11) is still on
// 2026-11-07 midday, so the instance zone (UTC) and each stored zone disagree
// on the calendar day without touching the wall clock.
var storedTimezoneTestNow = time.Date(2026, time.November, 7, 23, 30, 0, 0, time.UTC)

func renderedDashboardDate(t *testing.T, response *http.Response) string {
	t.Helper()
	assertStatusCode(t, response, http.StatusOK)
	match := storedTimezoneDashboardDatePattern.FindStringSubmatch(mustReadBodyString(t, response.Body))
	if match == nil {
		t.Fatal("no data-dashboard-date in the dashboard")
	}
	return match[1]
}

func TestDashboardTodayFallsBackToStoredOwnerTimezone(t *testing.T) {
	cases := []struct {
		name     string
		stored   string
		header   string
		cookie   string
		wantZone string
	}{
		{name: "auth cookie only uses the stored zone", stored: "Pacific/Kiritimati", wantZone: "Pacific/Kiritimati"},
		{name: "header outranks the stored zone", stored: "Pacific/Kiritimati", header: "Pacific/Pago_Pago", wantZone: "Pacific/Pago_Pago"},
		{name: "cookie outranks the stored zone", stored: "Pacific/Kiritimati", cookie: "Pacific/Pago_Pago", wantZone: "Pacific/Pago_Pago"},
		{name: "invalid header falls through to the stored zone", stored: "Pacific/Kiritimati", header: "Not/AZone", wantZone: "Pacific/Kiritimati"},
		{name: "empty stored zone falls through to the instance zone", stored: "", wantZone: "UTC"},
		{name: "unknown stored zone falls through to the instance zone", stored: "Not/AZone", wantZone: "UTC"},
		{name: "Local token stored falls through to the instance zone", stored: "Local", wantZone: "UTC"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return storedTimezoneTestNow }})
			user := createOnboardingTestUser(t, database, "stored-tz-dashboard@example.com", "StrongPass1", true)
			if err := database.Model(&user).Update("timezone", tc.stored).Error; err != nil {
				t.Fatalf("store timezone: %v", err)
			}
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			location, err := time.LoadLocation(tc.wantZone)
			if err != nil {
				t.Skipf("timezone data unavailable for %s: %v", tc.wantZone, err)
			}
			want := services.DateAtLocation(storedTimezoneTestNow.In(location), location).Format("2006-01-02")

			request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			cookieHeader := authCookie
			if tc.cookie != "" {
				cookieHeader = joinCookieHeader(authCookie, timezoneCookieName+"="+tc.cookie)
			}
			request.Header.Set("Cookie", cookieHeader)
			if tc.header != "" {
				request.Header.Set(timezoneHeaderName, tc.header)
			}

			got := renderedDashboardDate(t, mustAppResponse(t, app, request))
			if got != want {
				t.Fatalf("dashboard rendered %s, want %s (the day in %s at the injected clock)", got, want, tc.wantZone)
			}
		})
	}
}

func TestStatsOverviewJSONFallsBackToStoredOwnerTimezone(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "stored-tz-stats-json@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)
	localDay, _ := staleBoundaryCycleDay(localToday, services.DateAtLocation(nowUTC, time.UTC))

	zoneDate := services.CalendarDay(localToday, time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": services.AddCalendarDays(zoneDate, -(localDay - 1), time.UTC),
		"timezone":          timezoneName,
	})
	seedStatsOverviewCycleHistory(t, database, user, zoneDate, localDay-1, localDay-1+28, localDay-1+56, localDay-1+84)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	payload := StatsOverviewResponse{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode stats overview payload: %v", err)
	}
	if payload.CurrentCycleDay != localDay {
		t.Fatalf("current_cycle_day = %d with no header or cookie, want %d (today in the stored %s, not UTC)", payload.CurrentCycleDay, localDay, timezoneName)
	}
}

func TestResolveRequestLocationStoredZoneRanksBetweenCookieAndFallback(t *testing.T) {
	fallback := time.FixedZone("UTC+4", 4*60*60)

	location, cookieValue := resolveRequestLocation("", "", "Europe/Moscow", fallback)
	if location.String() != "Europe/Moscow" || cookieValue != "" {
		t.Fatalf("stored zone: got %q cookie %q", location.String(), cookieValue)
	}
	location, _ = resolveRequestLocation("", "UTC", "Europe/Moscow", fallback)
	if location.String() != "UTC" {
		t.Fatalf("cookie must outrank the stored zone, got %q", location.String())
	}
	location, _ = resolveRequestLocation("", "", "bad zone", fallback)
	if location.String() != fallback.String() {
		t.Fatalf("an unparsable stored zone must fall through, got %q", location.String())
	}
}
