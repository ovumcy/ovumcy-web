package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func ppDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestResolvePregnancyPauseNoLogs(t *testing.T) {
	if _, paused := ResolvePregnancyPause(nil); paused {
		t.Fatal("expected no pause for empty logs")
	}
}

func TestResolvePregnancyPauseNoPositiveTest(t *testing.T) {
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 1), PregnancyTest: models.PregnancyTestNegative},
		{Date: ppDay(2026, time.March, 2), IsPeriod: true, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); paused {
		t.Fatal("expected no pause without a positive test")
	}
}

func TestResolvePregnancyPausePositiveWithoutLaterCycleStart(t *testing.T) {
	positive := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 1), IsPeriod: true, CycleStart: true},
		{Date: positive, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause when positive test has no later cycle start")
	}
	if !date.Equal(positive) {
		t.Fatalf("expected pause date %s, got %s", positive, date)
	}
}

func TestResolvePregnancyPauseLiftedByLaterCycleStart(t *testing.T) {
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 10), PregnancyTest: models.PregnancyTestPositive},
		{Date: ppDay(2026, time.April, 5), IsPeriod: true, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); paused {
		t.Fatal("expected no pause when a cycle start follows the positive test")
	}
}

func TestResolvePregnancyPausePositiveWinsSameDayTie(t *testing.T) {
	day := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: day, IsPeriod: true, CycleStart: true, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause when cycle start and positive test share a day")
	}
	if !date.Equal(day) {
		t.Fatalf("expected pause date %s, got %s", day, date)
	}
}

func TestResolvePregnancyPauseUsesLatestPositive(t *testing.T) {
	latest := ppDay(2026, time.March, 20)
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 5), PregnancyTest: models.PregnancyTestPositive},
		{Date: latest, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause")
	}
	if !date.Equal(latest) {
		t.Fatalf("expected latest positive date %s, got %s", latest, date)
	}
}

// TestResolvePregnancyPauseIsIndependentOfLogOrder pins that the two scans pick
// the LATEST matching day rather than the last entry they happen to see. Every
// other fixture in this file places the decisive event last, so replacing both
// date comparisons with plain last-entry-wins assignments changed nothing —
// while a repository that returns rows in a different order (or a caller that
// appends a backdated entry) would silently unpause a pregnancy, or pause on a
// stale positive test that a later cycle start has already lifted.
func TestResolvePregnancyPauseIsIndependentOfLogOrder(t *testing.T) {
	tests := []struct {
		name      string
		logs      []models.DailyLog
		wantPause bool
		wantDate  time.Time
	}{
		{
			name: "latest positive with no cycle start after it",
			logs: []models.DailyLog{
				{Date: ppDay(2026, time.March, 1), IsPeriod: true, CycleStart: true},
				{Date: ppDay(2026, time.March, 5), PregnancyTest: models.PregnancyTestPositive},
				{Date: ppDay(2026, time.March, 20), PregnancyTest: models.PregnancyTestPositive},
			},
			wantPause: true,
			wantDate:  ppDay(2026, time.March, 20),
		},
		{
			name: "a later cycle start lifts the pause",
			logs: []models.DailyLog{
				// One cycle start on each side of the positive test, so an
				// order-dependent scan can pick the earlier one and conclude
				// the pause is still on.
				{Date: ppDay(2026, time.March, 5), IsPeriod: true, CycleStart: true},
				{Date: ppDay(2026, time.March, 10), PregnancyTest: models.PregnancyTestPositive},
				{Date: ppDay(2026, time.April, 5), IsPeriod: true, CycleStart: true},
			},
			wantPause: false,
		},
	}

	orders := []struct {
		name    string
		reorder func([]models.DailyLog) []models.DailyLog
	}{
		{name: "ascending", reorder: func(logs []models.DailyLog) []models.DailyLog { return logs }},
		{name: "descending", reorder: reversedDailyLogs},
		// A fixed rotation stands in for "shuffled": it is deterministic, so a
		// failure here is reproducible, and it puts a middle entry last, which
		// is what a last-entry-wins scan reads as the decision.
		{name: "rotated", reorder: func(logs []models.DailyLog) []models.DailyLog {
			return append(append([]models.DailyLog{}, logs[len(logs)-1]), logs[:len(logs)-1]...)
		}},
	}

	for _, testCase := range tests {
		for _, order := range orders {
			t.Run(testCase.name+" "+order.name, func(t *testing.T) {
				date, paused := ResolvePregnancyPause(order.reorder(append([]models.DailyLog{}, testCase.logs...)))
				if paused != testCase.wantPause {
					t.Fatalf("ResolvePregnancyPause() paused = %v, want %v", paused, testCase.wantPause)
				}
				if !testCase.wantPause {
					return
				}
				if !date.Equal(testCase.wantDate) {
					t.Fatalf("ResolvePregnancyPause() date = %s, want %s", date, testCase.wantDate)
				}
			})
		}
	}
}

func reversedDailyLogs(logs []models.DailyLog) []models.DailyLog {
	reversed := make([]models.DailyLog, 0, len(logs))
	for index := len(logs) - 1; index >= 0; index-- {
		reversed = append(reversed, logs[index])
	}
	return reversed
}

func TestResolvePregnancyPauseIgnoresCycleStartWithoutPeriod(t *testing.T) {
	positive := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: positive, PregnancyTest: models.PregnancyTestPositive},
		{Date: ppDay(2026, time.April, 1), IsPeriod: false, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); !paused {
		t.Fatal("expected pause: a cycle-start flag without a period day must not lift it")
	}
}

// pauseParitySurfaceLogs names the log set each surface used to hand the shared
// derivation. ResolvePregnancyPause itself has no today — it compares two dates
// out of whatever set it is given — so before the bound moved into
// BuildCycleStatsFromLogs the verdict was a property of the CALLER's fetch, not
// of the owner's data. The dashboard and the .ics feed were correct by accident
// of pre-bounding; the webhook notify pass passes the whole stored history.
func pauseParitySurfaceLogs(history []models.DailyLog, today time.Time, location *time.Location) map[string][]models.DailyLog {
	return map[string][]models.DailyLog{
		"whole stored history (webhook notify pass)": history,
		"bounded at today (dashboard, .ics feed)":    FilterLogsByDateRange(history, today.AddDate(-1, 0, 0), today, location),
	}
}

// TestBuildCycleStatsFromLogsResolvesThePauseOnOneTodayBoundedTimeline is the
// parity case: for one owner at one instant, the pause verdict must not depend
// on which surface's fetch bound produced the log set, nor on the owner's zone.
// A cycle start recorded for TOMORROW is permitted by manual entry
// (manualCycleStartFutureDays) and belongs to no timeline that has happened, so
// it may not lift a pause anywhere. The zones straddle the date line in both
// directions, so a bound taken in UTC rather than in the owner's zone lands on
// the wrong calendar day and shows up here.
func TestBuildCycleStatsFromLogsResolvesThePauseOnOneTodayBoundedTimeline(t *testing.T) {
	zones := []struct {
		name string
		hour int
	}{
		{"UTC", 12},
		{"Pacific/Kiritimati", 11}, // UTC+14: the owner is already on the next date
		{"Pacific/Niue", 2},        // UTC-11: the owner is still on the previous one
		{"Asia/Tokyo", 23},         // local tomorrow, UTC today
		{"America/Los_Angeles", 1}, // local yesterday, UTC today
	}

	for _, zone := range zones {
		location, err := time.LoadLocation(zone.name)
		if err != nil {
			t.Fatalf("load %s: %v", zone.name, err)
		}

		now := time.Date(2026, time.March, 12, zone.hour, 0, 0, 0, time.UTC)
		// Stored dates are canonical UTC-midnight, so the fixture names the
		// owner's local calendar day in the shape the database holds.
		today := DateAtLocation(now, location)
		positive := ppDay(today.Year(), today.Month(), today.Day())
		user := &models.User{}

		paused := []models.DailyLog{
			{Date: positive.AddDate(0, 0, -26), IsPeriod: true, CycleStart: true},
			{Date: positive, PregnancyTest: models.PregnancyTestPositive},
			{Date: positive.AddDate(0, 0, 1), IsPeriod: true, CycleStart: true},
		}
		for surface, logs := range pauseParitySurfaceLogs(paused, today, location) {
			stats := BuildCycleStatsFromLogs(user, logs, now, location)
			if !stats.PregnancyPaused {
				t.Errorf("%s / %s: a cycle start logged for tomorrow lifted the pause", zone.name, surface)
			}
			if !PredictionsSuppressed(user, stats) {
				t.Errorf("%s / %s: the pause must reach the shared suppression predicate", zone.name, surface)
			}
		}

		// Control: a start already in the past is a real resumption and must lift
		// the pause on every surface. Without it the case above would be green
		// against a derivation that had simply started pausing unconditionally.
		resumed := []models.DailyLog{
			{Date: positive.AddDate(0, 0, -26), IsPeriod: true, CycleStart: true},
			{Date: positive.AddDate(0, 0, -2), PregnancyTest: models.PregnancyTestPositive},
			{Date: positive.AddDate(0, 0, -1), IsPeriod: true, CycleStart: true},
		}
		for surface, logs := range pauseParitySurfaceLogs(resumed, today, location) {
			stats := BuildCycleStatsFromLogs(user, logs, now, location)
			if stats.PregnancyPaused {
				t.Errorf("%s / %s: a cycle start already in the past must lift the pause", zone.name, surface)
			}
		}
	}
}
