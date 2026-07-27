package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These tests pin the RENDER path of the short/long-cycle medical notes, not
// just the service predicate: a flag computed correctly in the service but
// dropped from the stats page-data map or the template would leave the
// predicate unit tests green while the note never reaches the user — exactly
// the failure mode that hid the show-historical-phases bug. They seed real
// cycle-start patterns, GET /stats, and assert the data-hook presence/absence.

// statsBodyForCyclePattern seeds cycleCount+1 period-start days gapDays apart,
// renders /stats, and returns the page body. The cycle detector then sees
// cycleCount completed cycles of length gapDays.
func statsBodyForCyclePattern(t *testing.T, email string, gapDays int, cycleCount int) string {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// cycleCount completed cycles need cycleCount+1 period starts.
	logs := make([]models.DailyLog, 0, cycleCount+1)
	var latestStart time.Time
	for i := cycleCount; i >= 0; i-- {
		start := today.AddDate(0, 0, -8-gapDays*i)
		if i == 0 {
			latestStart = start
		}
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: start, IsPeriod: true})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": latestStart,
	}).Error; err != nil {
		t.Fatalf("set last period start: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

// statsBodyForCycleAnchorAge seeds three period starts gapDays apart, the most
// recent of them startedDaysAgo days back, and renders /stats.
//
// Three rather than one because the note under test lives on the current-phase
// card, and the whole card grid is replaced by the empty state until the
// account has the two completed cycles that unlock insights. With both cycles
// the same length, the reference length is that length, so the caller controls
// staleness purely through the age of the latest anchor.
func statsBodyForCycleAnchorAge(t *testing.T, email string, startedDaysAgo int, gapDays int) string {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -startedDaysAgo)

	logs := []models.DailyLog{
		{UserID: user.ID, Date: start.AddDate(0, 0, -2*gapDays), IsPeriod: true},
		{UserID: user.ID, Date: start.AddDate(0, 0, -gapDays), IsPeriod: true},
		{UserID: user.ID, Date: start, IsPeriod: true},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": start,
	}).Error; err != nil {
		t.Fatalf("set last period start: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

// TestStatsPagePinsThePhaseEstimatedNoteByHook covers the last warning on the
// stats page that no test addressed. It is gated by CycleDataStale, so it has
// the same failure mode as the notices above: a flag computed correctly and
// then dropped from the page-data map or the template leaves every service test
// green while the reader never sees the note.
//
// Both directions are asserted in one test so the absent case has a positive
// anchor: a negative-only check would pass just as well if the hook were
// renamed or the block deleted outright.
func TestStatsPagePinsThePhaseEstimatedNoteByHook(t *testing.T) {
	staleBody := statsBodyForCycleAnchorAge(t, "stats-phase-estimated-stale@example.com", 60, 30)
	if !strings.Contains(staleBody, "data-stats-phase-estimated") {
		t.Fatal("expected the phase-estimated note on a stale cycle anchor")
	}
	if !strings.Contains(staleBody, `data-notice-key="dashboard.phase_estimated"`) {
		t.Fatal("expected the phase-estimated note to carry its catalogue key")
	}

	freshBody := statsBodyForCycleAnchorAge(t, "stats-phase-estimated-fresh@example.com", 3, 30)
	if strings.Contains(freshBody, "data-stats-phase-estimated") {
		t.Fatal("expected no phase-estimated note while the cycle anchor is current")
	}
}

func TestStatsPageRendersShortCycleNoticeForShortPattern(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-short-notice@example.com", 20, 3)
	if !strings.Contains(body, "data-stats-short-cycle-notice") {
		t.Fatal("expected the short-cycle notice to render for three 20-day cycles")
	}
	if strings.Contains(body, "data-stats-long-cycle-notice") {
		t.Fatal("did not expect the long-cycle notice for short cycles")
	}
}

func TestStatsPageRendersLongCycleNoticeForLongPattern(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-long-notice@example.com", 50, 3)
	if !strings.Contains(body, "data-stats-long-cycle-notice") {
		t.Fatal("expected the long-cycle notice to render for three 50-day cycles")
	}
	if strings.Contains(body, "data-stats-short-cycle-notice") {
		t.Fatal("did not expect the short-cycle notice for long cycles")
	}
}

func TestStatsPageOmitsCycleLengthNoticesForNormalPattern(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-normal-notice@example.com", 28, 3)
	if strings.Contains(body, "data-stats-short-cycle-notice") {
		t.Fatal("normal 28-day cycles must not render the short-cycle notice")
	}
	if strings.Contains(body, "data-stats-long-cycle-notice") {
		t.Fatal("normal 28-day cycles must not render the long-cycle notice")
	}
}
