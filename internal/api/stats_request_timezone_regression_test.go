package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestStatsOverviewUsesRequestTimezoneForCycleContext(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "stats-overview-timezone@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": localToday,
	}).Error; err != nil {
		t.Fatalf("update timezone stats baseline: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	payload := StatsOverviewResponse{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode stats overview payload: %v", err)
	}

	if payload.LastPeriodStart == nil {
		t.Fatal("expected a recorded last_period_start, got null")
	}
	// The wire value is a calendar day, so the request timezone shows in the
	// published date itself rather than in the reader's conversion of an instant.
	wantLastPeriodStart := localToday.Format("2006-01-02")
	if *payload.LastPeriodStart != wantLastPeriodStart {
		t.Fatalf("expected request-local last_period_start %q, got %q", wantLastPeriodStart, *payload.LastPeriodStart)
	}
}

func TestBuildStatsPageDataUsesRequestTimezoneForCycleContext(t *testing.T) {
	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "stats-page-timezone@example.com")

	nowUTC := time.Now().UTC()
	_, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": localToday,
	}).Error; err != nil {
		t.Fatalf("update timezone stats page baseline: %v", err)
	}

	if err := database.First(&user, user.ID).Error; err != nil {
		t.Fatalf("reload stats page user: %v", err)
	}

	data, err := handler.buildStatsPageData(context.Background(), &user, "en", map[string]string{}, nowUTC.In(location), location)
	if err != nil {
		t.Fatalf("build stats page data: %v", err)
	}

	stats, ok := data["Stats"].(services.CycleStats)
	if !ok {
		t.Fatalf("expected stats payload in stats page data")
	}

	gotLastPeriodStart := services.DateAtLocation(stats.LastPeriodStart.In(location), location).Format("2006-01-02")
	wantLastPeriodStart := localToday.Format("2006-01-02")
	if gotLastPeriodStart != wantLastPeriodStart {
		t.Fatalf("expected stats page request-local last_period_start %q, got %q", wantLastPeriodStart, gotLastPeriodStart)
	}
}
