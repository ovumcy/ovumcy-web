package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
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

	request := httptest.NewRequest(http.MethodGet, "/api/stats/overview", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	payload := services.CycleStats{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode stats overview payload: %v", err)
	}

	gotLastPeriodStart := services.DateAtLocation(payload.LastPeriodStart.In(location), location).Format("2006-01-02")
	wantLastPeriodStart := localToday.Format("2006-01-02")
	if gotLastPeriodStart != wantLastPeriodStart {
		t.Fatalf("expected request-local last_period_start %q, got %q", wantLastPeriodStart, gotLastPeriodStart)
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

	data, err := handler.buildStatsPageData(&user, "en", map[string]string{}, nowUTC.In(location), location)
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
