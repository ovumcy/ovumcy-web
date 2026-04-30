package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestCycleStartSavedWithoutTimezoneRendersOnLocalDayInNegativeOffset
// reproduces issue #48 end-to-end via HTTP.
//
// Scenario: a request that writes the cycle start arrives without a resolved
// client timezone (no X-Ovumcy-Timezone header, no ovumcy_tz cookie). The
// server falls back to handler.location (UTC in production Docker images) and
// persists the daily log Date / user.LastPeriodStart as UTC midnight of the
// chosen ISO date. A later page load (calendar / dashboard) arrives with a
// UTC-minus IANA timezone (America/Toronto). The current implementation pipes
// the stored UTC-midnight time.Time through DateAtLocation in the viewer's
// locale and the t.In(location) shift moves the calendar day one day earlier,
// which is what the user observed.
//
// Expected behavior: the actual logged entry must render on the same calendar
// day that was originally posted to the server, regardless of the viewer's
// locale.
func TestCycleStartSavedWithoutTimezoneRendersOnLocalDayInNegativeOffset(t *testing.T) {
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("zoneinfo for America/Toronto unavailable: %v", err)
	}

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "tz-negative-offset@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// The server falls back to handler.location (UTC) when no timezone is
	// supplied. Pick the date that the server treats as "today" in that
	// fallback so that the cycle-start policy accepts the request, and so
	// that this test does not depend on flaky day-boundary timing.
	serverToday := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	if serverToday.Day() == 1 {
		// Avoid the month boundary: if "today UTC" is the first of the
		// month, the prior calendar day belongs to the previous month and
		// requires a different month parameter. Step one day back to keep
		// the assertion within a single month grid.
		serverToday = serverToday.AddDate(0, 0, -1)
	}
	postedDayRaw := serverToday.Format("2006-01-02")
	priorDayRaw := serverToday.AddDate(0, 0, -1).Format("2006-01-02")
	monthValue := serverToday.Format("2006-01")

	cycleStartRequest := httptest.NewRequest(http.MethodPost, "/api/days/"+postedDayRaw+"/cycle-start", strings.NewReader(""))
	cycleStartRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cycleStartRequest.Header.Set("HX-Request", "true")
	cycleStartRequest.Header.Set("Accept-Language", "en")
	cycleStartRequest.Header.Set("Cookie", authCookie)
	// Intentionally omit X-Ovumcy-Timezone and ovumcy_tz cookie.

	cycleStartResponse := mustAppResponse(t, app, cycleStartRequest)
	if cycleStartResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("expected cycle-start status 204, got %d", cycleStartResponse.StatusCode)
	}

	tzCookieHeader := joinCookieHeader(authCookie, timezoneCookieName+"="+location.String())

	calendarRequest := httptest.NewRequest(http.MethodGet, "/calendar?month="+monthValue, nil)
	calendarRequest.Header.Set("Accept-Language", "en")
	calendarRequest.Header.Set("Cookie", tzCookieHeader)
	calendarRequest.Header.Set(timezoneHeaderName, location.String())

	calendarResponse := mustAppResponse(t, app, calendarRequest)
	assertStatusCode(t, calendarResponse, http.StatusOK)
	calendarBody := mustReadBodyString(t, calendarResponse.Body)

	postedState := extractCalendarCellState(t, calendarBody, postedDayRaw)
	priorState := extractCalendarCellState(t, calendarBody, priorDayRaw)
	t.Logf("posted day %s state=%q, prior day %s state=%q", postedDayRaw, postedState, priorDayRaw, priorState)

	if postedState != "period" {
		t.Fatalf("expected calendar cell %s to render as actual period (state=\"period\"), got %q", postedDayRaw, postedState)
	}
	if priorState == "period" {
		t.Fatalf("expected calendar cell %s NOT to render as actual period, got %q (entry shifted one day earlier in viewer locale)", priorDayRaw, priorState)
	}
}

func extractCalendarCellState(t *testing.T, body string, dayISO string) string {
	t.Helper()

	pattern := regexp.MustCompile(fmt.Sprintf(`data-day="%s"\s+data-calendar-state="([^"]+)"`, regexp.QuoteMeta(dayISO)))
	matches := pattern.FindStringSubmatch(body)
	if len(matches) < 2 {
		t.Fatalf("calendar cell for %s not found in markup", dayISO)
	}
	return matches[1]
}
