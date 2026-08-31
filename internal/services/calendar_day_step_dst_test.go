package services

// calendar_day_step_dst_test.go — a calendar-day STEP must not be taken inside
// the request zone.
//
// `day.AddDate(0, 0, n)` re-enters time.Date in the receiver's own location. In
// the UTC-minus zones whose DST jump lands on midnight (America/Santiago
// 2026-09-06, America/Havana 2026-03-08) that local midnight does not exist, so
// the missing wall clock resolves BACKWARD and the step returns the PREVIOUS
// calendar day at 23:00. Everything downstream that reads the calendar
// components — a map key, a rendered bound, a projected date — is then handed
// the wrong day.
//
// AddCalendarDays is the single stepping point that fixes it: it steps over
// UTC-anchored days and resolves the result back into the caller's location
// through StartOfCalendarDay. This file pins the helper itself and the
// user-visible surfaces that were stepping by hand.
//
// The zone must be west of UTC — east-of-UTC zones normalize a missing midnight
// forward and keep the date, so the same fixture there is green about nothing
// (`.claude/rules/testing.md`). Every case carries a UTC control.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// calendarStepHavanaLocation is the second midnight-skipping zone. Named for
// this file rather than shared: santiagoTestLocation already exists in the
// package, and a second generic helper would collide with one added elsewhere.
func calendarStepHavanaLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatalf("load America/Havana: %v", err)
	}
	return location
}

// TestAddCalendarDaysCrossesASkippedMidnight pins the helper every converted
// site now routes through.
func TestAddCalendarDaysCrossesASkippedMidnight(t *testing.T) {
	santiago := santiagoTestLocation(t)
	havana := calendarStepHavanaLocation(t)

	testCases := []struct {
		name     string
		location *time.Location
		from     string
		days     int
		want     string
	}{
		{name: "santiago forward onto the skipped midnight", location: santiago, from: "2026-09-01", days: 5, want: "2026-09-06"},
		{name: "santiago forward across it", location: santiago, from: "2026-09-01", days: 8, want: "2026-09-09"},
		{name: "santiago backward onto it", location: santiago, from: "2026-09-09", days: -3, want: "2026-09-06"},
		{name: "havana forward onto the skipped midnight", location: havana, from: "2026-03-03", days: 5, want: "2026-03-08"},
		{name: "havana backward onto it", location: havana, from: "2026-03-11", days: -3, want: "2026-03-08"},
		{name: "control: UTC, same september arithmetic", location: time.UTC, from: "2026-09-01", days: 5, want: "2026-09-06"},
		{name: "control: santiago, no transition in range", location: santiago, from: "2026-08-01", days: 5, want: "2026-08-06"},
		{name: "control: zero step is the day itself", location: santiago, from: "2026-09-06", days: 0, want: "2026-09-06"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			from := CalendarDay(cyclesignalsCovDay(t, testCase.from), testCase.location)
			got := AddCalendarDays(from, testCase.days, testCase.location)
			if key := CalendarDayKey(got); key != testCase.want {
				t.Fatalf("AddCalendarDays(%s, %d) = %s, want %s", testCase.from, testCase.days, key, testCase.want)
			}
			// The result must be a real instant on the day it names, so a caller
			// ordering it against another location midnight is not comparing a
			// value that silently sits on the previous day.
			if year, month, day := got.Date(); CalendarDayKey(time.Date(year, month, day, 0, 0, 0, 0, time.UTC)) != testCase.want {
				t.Fatalf("AddCalendarDays returned %s, whose own calendar components are not %s", got.Format(time.RFC3339), testCase.want)
			}
		})
	}
}

// TestAddCalendarDaysKeepsAZeroValueZero guards the guard: a zero time must not
// become year 1 day 2, which would make an IsZero() check downstream stop
// firing.
func TestAddCalendarDaysKeepsAZeroValueZero(t *testing.T) {
	if got := AddCalendarDays(time.Time{}, 3, santiagoTestLocation(t)); !got.IsZero() {
		t.Fatalf("AddCalendarDays(zero) = %s, want the zero value", got.Format(time.RFC3339))
	}
}

// TestPredictCycleWindowLandsOnTheSkippedMidnightDay is the core: every
// projected date on every surface comes out of this function, and the calendar
// hands it a request-zone anchor. Pre-fix the step ran before dateOnly, so the
// projection named 2026-09-05 for an ovulation on the 6th.
func TestPredictCycleWindowLandsOnTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)

	testCases := []struct {
		name          string
		location      *time.Location
		periodStart   string
		wantOvulation string
	}{
		{
			name:          "santiago: projected ovulation on the skipped midnight",
			location:      santiago,
			periodStart:   "2026-08-24",
			wantOvulation: "2026-09-06",
		},
		{
			name:          "control: the same cycle anchored in UTC",
			location:      time.UTC,
			periodStart:   "2026-08-24",
			wantOvulation: "2026-09-06",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			periodStart := CalendarDay(cyclesignalsCovDay(t, testCase.periodStart), testCase.location)
			window := PredictCycleWindow(periodStart, 28, 14)
			if !window.Calculable {
				t.Fatal("expected a calculable window for a 28/14 cycle")
			}
			if got := CalendarDayKey(window.OvulationDate); got != testCase.wantOvulation {
				t.Fatalf("projected ovulation = %s, want %s", got, testCase.wantOvulation)
			}
		})
	}
}

// TestManualCycleStartMaxDateReachesTheSkippedMidnightDay covers the input rule
// an owner meets directly: a cycle start may be recorded up to
// manualCycleStartFutureDays ahead, and pre-fix the bound named the day before
// when that allowance landed on the skipped midnight — so the form refused a
// date its own rule permits.
func TestManualCycleStartMaxDateReachesTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)

	// Today is 2026-09-04 in Santiago; the two-day allowance reaches 09-06.
	now := time.Date(2026, time.September, 4, 15, 0, 0, 0, time.UTC)

	if got := CalendarDayKey(manualCycleStartMaxDate(now, santiago)); got != "2026-09-06" {
		t.Fatalf("manual cycle-start upper bound = %s, want 2026-09-06", got)
	}

	target := CalendarDay(cyclesignalsCovDay(t, "2026-09-06"), santiago)
	if !IsAllowedManualCycleStartDate(target, now, santiago) {
		t.Fatal("2026-09-06 is two days ahead and must be an allowed manual cycle start")
	}
}

// TestOnboardingDateBoundsReachTheSkippedMidnightDay covers the other bound an
// owner meets: the earliest date the onboarding picker offers. Pre-fix the
// window opened a day late whenever its far edge fell on the skipped midnight.
func TestOnboardingDateBoundsReachTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)

	// OnboardingStartDateWindowDays is 60; 2026-11-05 minus 60 days is 09-06.
	now := time.Date(2026, time.November, 5, 15, 0, 0, 0, time.UTC)

	earliest, latest := OnboardingDateBounds(now, santiago)
	if got := CalendarDayKey(earliest); got != "2026-09-06" {
		t.Fatalf("onboarding earliest date = %s, want 2026-09-06", got)
	}
	if got := CalendarDayKey(latest); got != "2026-11-05" {
		t.Fatalf("onboarding latest date = %s, want 2026-11-05", got)
	}
}

// TestBuildCalendarDayStatesProjectsACycleOntoTheSkippedMidnightDay follows the
// step all the way to the rendered grid. The assertion is on the EDGES of the
// painted run, not on the transition day itself: pre-fix the chain stepped
// 2026-08-09 + 28 onto the skipped midnight and landed on 09-05, so the whole
// five-day projected period shifted one day earlier — 09-06 stayed painted
// either way, and only the edges tell the two apart.
func TestBuildCalendarDayStatesProjectsACycleOntoTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)

	user := &models.User{WeekStartsOn: models.WeekStartMonday}
	monthStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)

	// The second projected cycle start is 2026-08-09 + 28 = 2026-09-06, so the
	// chain steps ONTO the skipped midnight rather than starting there.
	stats := CycleStats{
		MedianCycleLength:   28,
		AverageCycleLength:  28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		CurrentCycleDay:     25,
		LastPeriodStart:     time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:     time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(user, monthStart, nil, stats, now, santiago)

	predicted := make(map[string]bool, len(days))
	for _, day := range days {
		predicted[day.DateString] = day.IsPredicted
	}

	for _, day := range []string{"2026-09-06", "2026-09-10"} {
		if !predicted[day] {
			t.Errorf("%s must be painted as a projected period day: the cycle starting 2026-09-06 runs five days", day)
		}
	}
	if predicted["2026-09-05"] {
		t.Error("2026-09-05 must NOT be painted: the projected cycle starts on 09-06, and painting it here means the step normalized backward off the skipped midnight")
	}
}
