package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// staleBoundaryCycleDay picks the cycle day the request zone sees so that the
// request zone and UTC fall on opposite sides of the out-of-date boundary. A
// 28-day history is out of date from cycle day 29 (the reference length + 1). In
// a zone ahead of UTC the zone's today is the later day, so the zone sees day 29
// (stale) where UTC sees 28; in a zone behind UTC it is the other way round. The
// cycle start is the same calendar day for both readers, so only the reading of
// "today" decides the verdict.
func staleBoundaryCycleDay(localToday, utcToday time.Time) (localDay int, localStale bool) {
	if localToday.Format("2006-01-02") > utcToday.Format("2006-01-02") {
		return 29, true
	}
	return 28, false
}

func TestStatsOverviewUsesRequestTimezoneForCycleContext(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "stats-overview-timezone@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)
	localDay, localStale := staleBoundaryCycleDay(localToday, services.DateAtLocation(nowUTC, time.UTC))

	zoneDate := services.CalendarDay(localToday, time.UTC)
	lastPeriodStart := services.AddCalendarDays(zoneDate, -(localDay - 1), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	})
	seedStatsOverviewCycleHistory(t, database, user, zoneDate, localDay-1, localDay-1+28, localDay-1+56, localDay-1+84)

	fetch := func(t *testing.T, zoneName string) StatsOverviewResponse {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+zoneName))
		request.Header.Set(timezoneHeaderName, zoneName)

		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		payload := StatsOverviewResponse{}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatalf("decode stats overview payload: %v", err)
		}
		return payload
	}

	payload := fetch(t, timezoneName)
	if payload.LastPeriodStart == nil {
		t.Fatal("expected a recorded last_period_start, got null")
	}
	// The wire value is a calendar day, so the request timezone shows in the
	// published date itself rather than in the reader's conversion of an instant.
	if want := lastPeriodStart.Format("2006-01-02"); *payload.LastPeriodStart != want {
		t.Fatalf("expected last_period_start %q, got %q", want, *payload.LastPeriodStart)
	}
	// The zone's today decides the cycle day and the out-of-date verdict derived
	// from it; the same stored history read as UTC lands on the other side of the
	// boundary (the control below).
	if payload.CurrentCycleDay != localDay {
		t.Fatalf("current_cycle_day = %d in %s, want %d (the zone's today, not UTC's)", payload.CurrentCycleDay, timezoneName, localDay)
	}
	stale := func(response StatsOverviewResponse) bool {
		return slices.Contains(response.Suppression.Reasons, string(services.SuppressionReasonCycleDataStale))
	}
	if stale(payload) != localStale || payload.Suppression.Fertility != localStale {
		t.Fatalf("in %s suppression = %+v, want the out-of-date fertility suppression = %t", timezoneName, payload.Suppression, localStale)
	}

	control := fetch(t, "UTC")
	if control.CurrentCycleDay == localDay || stale(control) == localStale {
		t.Fatalf("fixture: the same history read as UTC gives cycle day %d with reasons %v, which does not differ from the zone's verdict", control.CurrentCycleDay, control.Suppression.Reasons)
	}
}

func TestBuildStatsPageDataUsesRequestTimezoneForCycleContext(t *testing.T) {
	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "stats-page-timezone@example.com")

	nowUTC := time.Now().UTC()
	_, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)
	localDay, localStale := staleBoundaryCycleDay(localToday, services.DateAtLocation(nowUTC, time.UTC))

	zoneDate := services.CalendarDay(localToday, time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": services.AddCalendarDays(zoneDate, -(localDay - 1), time.UTC),
	})
	seedStatsOverviewCycleHistory(t, database, user, zoneDate, localDay-1, localDay-1+28, localDay-1+56, localDay-1+84)

	if err := database.First(&user, user.ID).Error; err != nil {
		t.Fatalf("reload stats page user: %v", err)
	}

	build := func(t *testing.T, at time.Time, in *time.Location) (services.CycleStats, bool) {
		t.Helper()
		data, err := handler.buildStatsPageData(context.Background(), &user, "en", map[string]string{}, at, in)
		if err != nil {
			t.Fatalf("build stats page data: %v", err)
		}
		stats, ok := data["Stats"].(services.CycleStats)
		if !ok {
			t.Fatalf("expected stats payload in stats page data")
		}
		stale, ok := data["CycleDataStale"].(bool)
		if !ok {
			t.Fatalf("expected a boolean CycleDataStale in stats page data, got %T", data["CycleDataStale"])
		}
		return stats, stale
	}

	stats, stale := build(t, nowUTC.In(location), location)
	if stats.CurrentCycleDay != localDay {
		t.Fatalf("stats page current cycle day = %d, want %d (the request zone's today)", stats.CurrentCycleDay, localDay)
	}
	if stale != localStale {
		t.Fatalf("stats page CycleDataStale = %t in the request zone, want %t", stale, localStale)
	}

	utcStats, utcStale := build(t, nowUTC, time.UTC)
	if utcStats.CurrentCycleDay == localDay || utcStale == localStale {
		t.Fatalf("fixture: the same history read as UTC gives cycle day %d, stale %t — it does not differ from the zone's verdict", utcStats.CurrentCycleDay, utcStale)
	}
}
