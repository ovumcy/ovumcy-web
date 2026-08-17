package services

// day_feedback_dst_walk_test.go — the backward period-streak walk must visit
// every calendar day exactly once, whatever the request zone does to the clock.
//
// currentPeriodStreakAtDay stepped its cursor with AddDate inside the request
// location. AddDate re-enters time.Date there, and in a UTC-minus zone whose
// DST jump lands on midnight (America/Santiago 2026-09-06) the missing wall
// clock normalizes BACKWARD into the previous calendar day: stepping back from
// 2026-09-07 00:00-03 lands on 2026-09-05 23:00-04, so the day key 2026-09-06
// was never queried at all while the log map built from CalendarDay carried it
// correctly.
//
// Two outcomes followed, and both are covered here:
//   - a continuous period spanning that day counted one day short, so a nine-day
//     run read as eight and the `streak > 8` gate suppressed the long-period
//     warning;
//   - a day that is NOT a period day no longer stopped the walk, so two separate
//     periods merged and the earlier cluster's start was reported — and then
//     written to long_period_warning_cycle_start by the acknowledgement path.
//
// The zone must be west of UTC: east-of-UTC zones normalize a missing midnight
// forward and keep the date, so the same fixture there is green about nothing.
// Controls: the identical fixtures in a transition-free month of the same zone,
// and in UTC, must be unchanged.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func santiagoTestLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatalf("load America/Santiago: %v", err)
	}
	return location
}

// seedPeriodDays writes IsPeriod logs for each YYYY-MM-DD in days, using the
// canonical UTC-midnight date-only shape the repository stores.
func seedPeriodDays(t *testing.T, logs *dayLogRepositoryStub, days ...string) {
	t.Helper()
	for _, day := range days {
		logs.entries[day] = models.DailyLog{
			UserID:   10,
			Date:     mustParseDayFeedbackDate(t, day),
			IsPeriod: true,
		}
	}
}

func continuousPeriodDays() []string {
	return []string{
		"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05",
		"2026-09-06", "2026-09-07", "2026-09-08", "2026-09-09",
	}
}

// TestCurrentPeriodStreakCountsTheDSTMidnightDay walks a nine-day continuous
// period whose middle day is America/Santiago's skipped midnight. Pre-fix the
// walk jumped straight from 09-07 to 09-05 and returned 8.
func TestCurrentPeriodStreakCountsTheDSTMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)
	logs := newDayLogRepositoryStub()
	seedPeriodDays(t, logs, continuousPeriodDays()...)

	entries, err := logs.ListByUser(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}

	day := time.Date(2026, time.September, 9, 12, 0, 0, 0, santiago)
	streak, cycleStart, ok := currentPeriodStreakAtDay(entries, day, santiago)
	if !ok {
		t.Fatal("expected the requested day to be recognized as a period day")
	}
	if streak != 9 {
		t.Fatalf("streak = %d, want 9: the walk must visit 2026-09-06 even though local midnight does not exist there", streak)
	}
	if got := CalendarDayKey(cycleStart); got != "2026-09-01" {
		t.Fatalf("cycle start = %s, want 2026-09-01", got)
	}
}

// TestCurrentPeriodStreakStopsAtTheDSTMidnightGap is the other half: 2026-09-06
// carries no period log, so the walk must stop there. Pre-fix it skipped the
// gap and merged the 08-28..09-05 period into the 09-07..09-09 one.
func TestCurrentPeriodStreakStopsAtTheDSTMidnightGap(t *testing.T) {
	santiago := santiagoTestLocation(t)
	logs := newDayLogRepositoryStub()
	seedPeriodDays(t, logs,
		"2026-08-28", "2026-08-29", "2026-08-30", "2026-08-31",
		"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05",
		"2026-09-07", "2026-09-08", "2026-09-09",
	)

	entries, err := logs.ListByUser(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}

	day := time.Date(2026, time.September, 9, 12, 0, 0, 0, santiago)
	streak, cycleStart, ok := currentPeriodStreakAtDay(entries, day, santiago)
	if !ok {
		t.Fatal("expected the requested day to be recognized as a period day")
	}
	if streak != 3 {
		t.Fatalf("streak = %d, want 3: the walk must stop at the gap on 2026-09-06 instead of merging the earlier period", streak)
	}
	if got := CalendarDayKey(cycleStart); got != "2026-09-07" {
		t.Fatalf("cycle start = %s, want 2026-09-07 (the current period), not the earlier cluster's start", got)
	}
}

// TestResolveDayFeedbackWarnsForALongPeriodSpanningTheDSTMidnightDay is the
// end-to-end consequence of the undercount: the nine-day run is the first that
// trips the `streak > 8` gate, so losing one day silences the warning the owner
// should see.
func TestResolveDayFeedbackWarnsForALongPeriodSpanningTheDSTMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	seedPeriodDays(t, logs, continuousPeriodDays()...)

	day := time.Date(2026, time.September, 9, 12, 0, 0, 0, santiago)
	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, santiago)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowLongPeriodWarning {
		t.Fatal("expected the long-period warning after nine consecutive period days spanning the skipped midnight")
	}
	if got := CalendarDayKey(state.LongPeriodCycleStart); got != "2026-09-01" {
		t.Fatalf("long-period cycle start = %s, want 2026-09-01", got)
	}
}

// TestResolveDayFeedbackDoesNotMergeTwoPeriodsAcrossTheDSTMidnightGap follows
// the merged-period outcome all the way to the persisted column: pre-fix the
// three-day current period read as a twelve-day one, the warning fired, and
// AcknowledgeLongPeriodWarning wrote the PREVIOUS period's start (2026-08-28)
// into long_period_warning_cycle_start.
func TestResolveDayFeedbackDoesNotMergeTwoPeriodsAcrossTheDSTMidnightGap(t *testing.T) {
	santiago := santiagoTestLocation(t)
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	seedPeriodDays(t, logs,
		"2026-08-28", "2026-08-29", "2026-08-30", "2026-08-31",
		"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05",
		"2026-09-07", "2026-09-08", "2026-09-09",
	)

	day := time.Date(2026, time.September, 9, 12, 0, 0, 0, santiago)
	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, santiago)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.ShowLongPeriodWarning {
		t.Fatalf("expected no long-period warning for a three-day period, got one anchored at %s", CalendarDayKey(state.LongPeriodCycleStart))
	}

	// The acknowledgement path is what persists the value, so drive it exactly
	// as the day-write handler does and assert nothing was written.
	if state.ShowLongPeriodWarning && !state.LongPeriodCycleStart.IsZero() {
		if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, state.LongPeriodCycleStart, santiago); err != nil {
			t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
		}
	}
	if stored := users.settings.LongPeriodWarnedAt; stored != nil {
		t.Fatalf("long_period_warning_cycle_start = %s, want no write at all", stored.Format("2006-01-02"))
	}
}

// TestCurrentPeriodStreakIsUnchangedWithoutATransition is the control pair: the
// same shapes in a transition-free month of the same west-of-UTC zone, and in
// UTC, must produce exactly the same answers as before the fix.
func TestCurrentPeriodStreakIsUnchangedWithoutATransition(t *testing.T) {
	santiago := santiagoTestLocation(t)

	continuousAugust := []string{
		"2026-08-01", "2026-08-02", "2026-08-03", "2026-08-04", "2026-08-05",
		"2026-08-06", "2026-08-07", "2026-08-08", "2026-08-09",
	}
	gapAugust := []string{
		"2026-07-24", "2026-07-25", "2026-07-26", "2026-07-27", "2026-07-28",
		"2026-07-29", "2026-07-30", "2026-07-31", "2026-08-01",
		"2026-08-03", "2026-08-04", "2026-08-05",
	}

	cases := []struct {
		name           string
		location       *time.Location
		days           []string
		requested      time.Time
		wantStreak     int
		wantCycleStart string
	}{
		{
			name:           "continuous run, Santiago, no transition in August",
			location:       santiago,
			days:           continuousAugust,
			requested:      time.Date(2026, time.August, 9, 12, 0, 0, 0, santiago),
			wantStreak:     9,
			wantCycleStart: "2026-08-01",
		},
		{
			name:           "gap, Santiago, no transition in August",
			location:       santiago,
			days:           gapAugust,
			requested:      time.Date(2026, time.August, 5, 12, 0, 0, 0, santiago),
			wantStreak:     3,
			wantCycleStart: "2026-08-03",
		},
		{
			name:           "continuous run across the same dates, UTC",
			location:       time.UTC,
			days:           continuousPeriodDays(),
			requested:      time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
			wantStreak:     9,
			wantCycleStart: "2026-09-01",
		},
		{
			name:     "gap across the same dates, UTC",
			location: time.UTC,
			days: []string{
				"2026-08-28", "2026-08-29", "2026-08-30", "2026-08-31",
				"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05",
				"2026-09-07", "2026-09-08", "2026-09-09",
			},
			requested:      time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
			wantStreak:     3,
			wantCycleStart: "2026-09-07",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			seedPeriodDays(t, logs, testCase.days...)
			entries, err := logs.ListByUser(context.Background(), 10)
			if err != nil {
				t.Fatalf("ListByUser() unexpected error: %v", err)
			}

			streak, cycleStart, ok := currentPeriodStreakAtDay(entries, testCase.requested, testCase.location)
			if !ok {
				t.Fatal("expected the requested day to be recognized as a period day")
			}
			if streak != testCase.wantStreak {
				t.Fatalf("streak = %d, want %d", streak, testCase.wantStreak)
			}
			if got := CalendarDayKey(cycleStart); got != testCase.wantCycleStart {
				t.Fatalf("cycle start = %s, want %s", got, testCase.wantCycleStart)
			}
		})
	}
}
