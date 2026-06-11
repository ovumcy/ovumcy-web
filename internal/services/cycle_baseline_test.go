package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestApplyUserCycleBaselineUsesSettingsFallbackWhenNoExplicitCycleStartExists(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-02-07")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     29,
		PeriodLength:    6,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-02-07"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-02-16"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-02-17")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if stats.AverageCycleLength != 9 {
		t.Fatalf("expected average cycle length 9, got %.2f", stats.AverageCycleLength)
	}
	if stats.MedianCycleLength != 9 {
		t.Fatalf("expected median cycle length 9, got %d", stats.MedianCycleLength)
	}
	if stats.AveragePeriodLength != 1 {
		t.Fatalf("expected average period length 1, got %.2f", stats.AveragePeriodLength)
	}
	if stats.LastPeriodStart.Format("2006-01-02") != "2026-02-07" {
		t.Fatalf("expected settings fallback last period start 2026-02-07, got %s", stats.LastPeriodStart.Format("2006-01-02"))
	}
	if stats.NextPeriodStart.Format("2006-01-02") != "2026-02-16" {
		t.Fatalf("expected next period start 2026-02-16, got %s", stats.NextPeriodStart.Format("2006-01-02"))
	}
	if stats.CurrentCycleDay != 11 {
		t.Fatalf("expected current cycle day 11, got %d", stats.CurrentCycleDay)
	}
	if stats.CurrentPhase != "unknown" {
		t.Fatalf("expected unknown phase for incompatible projected cycle, got %s", stats.CurrentPhase)
	}
}

func TestApplyUserCycleBaselinePrefersExplicitCycleStartOverSettingsFallback(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2025-03-27")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     29,
		PeriodLength:    6,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-02"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-03"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-30"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-31"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-26"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-27"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-28"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2025-03-05")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if stats.AverageCycleLength != 28 {
		t.Fatalf("expected reliable average cycle length 28, got %.2f", stats.AverageCycleLength)
	}
	if stats.MedianCycleLength != 28 {
		t.Fatalf("expected reliable median cycle length 28, got %d", stats.MedianCycleLength)
	}
	if stats.LastPeriodStart.Format("2006-01-02") != "2025-02-26" {
		t.Fatalf("expected explicit last period start 2025-02-26, got %s", stats.LastPeriodStart.Format("2006-01-02"))
	}
}

func TestApplyUserCycleBaselinePrefersMoreRecentSettingsBaselineOverOlderExplicitHistory(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-03-13")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-01-25"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-03-10"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-03-16")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if got := stats.LastPeriodStart.Format("2006-01-02"); got != "2026-03-13" {
		t.Fatalf("expected more recent user baseline 2026-03-13, got %s", got)
	}
	if stats.CurrentCycleDay != 4 {
		t.Fatalf("expected current cycle day 4 from recent baseline, got %d", stats.CurrentCycleDay)
	}
}

func TestApplyUserCycleBaselineClampsShortCyclePredictionsAwayFromDayOne(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-02-10")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     15,
		PeriodLength:    10,
		LutealPhase:     15,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-02-10"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-02-12")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if got := stats.OvulationDate.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected clamped ovulation date 2026-02-14, got %s", got)
	}
	if got := stats.FertilityWindowStart.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected fertility window to start on 2026-02-10, got %s", got)
	}
	if got := stats.FertilityWindowEnd.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected fertility window to end on 2026-02-14, got %s", got)
	}
	if stats.OvulationExact {
		t.Fatalf("expected exact=false for clamped short-cycle values")
	}
	if stats.OvulationImpossible {
		t.Fatalf("expected impossible=false for clamped short-cycle values")
	}
}

// TestApplyUserCycleBaselineCurrentCycleDayMatchesLocalCalendarAcrossTimezonesAndDST
// pins the displayed baseline "current cycle day" to the viewer's local
// calendar-day count, independent of UTC offset and DST transitions. The
// owner baseline path derives `today` from the request location (issue #48),
// so both operands of the day count carry a non-UTC wall clock; counting
// elapsed days by instant subtraction would lose a day whenever a DST
// spring-forward falls between the anchor and today. The expected values are
// pure calendar-day differences (anchor is cycle day 1).
func TestApplyUserCycleBaselineCurrentCycleDayMatchesLocalCalendarAcrossTimezonesAndDST(t *testing.T) {
	cases := []struct {
		name       string
		locationID string
		anchor     string
		now        time.Time
		wantDay    int
	}{
		{
			// UTC-5, no DST transition in the window: regression guard that a
			// negative-offset zone still counts the plain calendar difference.
			name:       "America/New_York without DST transition",
			locationID: "America/New_York",
			anchor:     "2026-02-07",
			now:        time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC),
			wantDay:    11, // 2026-02-07 -> 2026-02-17 local = 10 elapsed days
		},
		{
			// UTC+9, no DST ever: cross-timezone regression guard.
			name:       "Asia/Tokyo cross-timezone UTC+9",
			locationID: "Asia/Tokyo",
			anchor:     "2026-02-07",
			now:        time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC),
			wantDay:    11,
		},
		{
			// Spring-forward is 2026-03-29 02:00->03:00 in Berlin; the cycle
			// spans it, so instant subtraction would yield 695h (-> day 29).
			name:       "Europe/Berlin across spring-forward",
			locationID: "Europe/Berlin",
			anchor:     "2026-03-01",
			now:        time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
			wantDay:    30, // 2026-03-01 -> 2026-03-30 local = 29 elapsed days
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			location, err := time.LoadLocation(tc.locationID)
			if err != nil {
				t.Fatalf("load location %q: %v", tc.locationID, err)
			}

			anchor := mustParseBaselineDay(t, tc.anchor)
			user := &models.User{
				Role:            models.RoleOwner,
				CycleLength:     28,
				PeriodLength:    5,
				LastPeriodStart: &anchor,
			}
			logs := []models.DailyLog{
				{Date: anchor, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
			}

			stats := BuildCycleStats(logs, tc.now)
			stats = ApplyUserCycleBaseline(user, logs, stats, tc.now, location)

			if got := stats.LastPeriodStart.Format("2006-01-02"); got != tc.anchor {
				t.Fatalf("baseline LastPeriodStart = %s, want anchor %s (test setup drifted)", got, tc.anchor)
			}
			if stats.CurrentCycleDay != tc.wantDay {
				t.Fatalf("CurrentCycleDay = %d, want %d (local calendar day in %s)", stats.CurrentCycleDay, tc.wantDay, tc.locationID)
			}
		})
	}
}

// TestBaselineCurrentCycleDayGuardsAgainstFutureAnchor pins the defensive
// today-before-anchor branch of baselineCurrentCycleDay. ApplyUserCycleBaseline
// gates the resolved anchor to <= today, so this guard is unreachable through
// the public path and is exercised directly: a today strictly before the anchor
// (or a zero anchor) must yield 0, never a negative or wrapped cycle day, and
// the anchor day itself is cycle day 1.
func TestBaselineCurrentCycleDayGuardsAgainstFutureAnchor(t *testing.T) {
	anchor := mustParseBaselineDay(t, "2026-03-10")

	if got := baselineCurrentCycleDay(anchor, mustParseBaselineDay(t, "2026-03-09")); got != 0 {
		t.Fatalf("today before anchor must be 0, got %d", got)
	}
	if got := baselineCurrentCycleDay(time.Time{}, mustParseBaselineDay(t, "2026-03-09")); got != 0 {
		t.Fatalf("zero anchor must be 0, got %d", got)
	}
	if got := baselineCurrentCycleDay(anchor, anchor); got != 1 {
		t.Fatalf("anchor day itself must be cycle day 1, got %d", got)
	}
}

func mustParseBaselineDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
