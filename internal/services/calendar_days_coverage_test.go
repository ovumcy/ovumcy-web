package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// calendarGridBounds – lines 50, 51, 52
// ---------------------------------------------------------------------------

// calendardaysCovGridBounds verifies the exact grid start and end dates for
// a given month, exercising lines 50 (monthEnd), 51 (gridStart), and 52
// (gridEnd).
//
// February 2026:
//   - monthEnd = 2026-02-28 (last day of February, line 50)
//   - monthStart.Weekday() = Sunday(0) → gridStart = 2026-02-01 (line 51)
//   - monthEnd.Weekday()   = Saturday(6) → gridEnd = 2026-02-28 + (6-6) = 2026-02-28 (line 52)
//
// March 2026:
//   - monthEnd = 2026-03-31
//   - monthStart.Weekday() = Sunday(0) → gridStart = 2026-03-01
//   - monthEnd.Weekday()   = Tuesday(2) → gridEnd = 2026-03-31 + (6-2) = 2026-04-04
//
// January 2026 gives a month that does NOT start on Sunday:
//   - monthStart.Weekday() = Thursday(4) → gridStart = 2025-12-28
func TestCalendarDaysGridBoundsFebruary2026(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartSunday)

	// February 2026 starts on Sunday → gridStart == monthStart
	if got := gridStart.Format("2006-01-02"); got != "2026-02-01" {
		t.Fatalf("gridStart: want 2026-02-01, got %s", got)
	}
	// February 2026 ends on a Saturday → gridEnd == 2026-02-28
	if got := gridEnd.Format("2006-01-02"); got != "2026-02-28" {
		t.Fatalf("gridEnd: want 2026-02-28, got %s", got)
	}
}

func TestCalendarDaysGridBoundsMarch2026(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartSunday)

	// March 2026 starts on Sunday → gridStart == monthStart
	if got := gridStart.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("gridStart: want 2026-03-01, got %s", got)
	}
	// March 2026 ends on Tuesday (2) → gridEnd = 2026-03-31 + (6-2) = 2026-04-04
	if got := gridEnd.Format("2006-01-02"); got != "2026-04-04" {
		t.Fatalf("gridEnd: want 2026-04-04, got %s", got)
	}
}

func TestCalendarDaysGridBoundsJanuary2026(t *testing.T) {
	// January 2026: monthStart = Thursday(4) → gridStart must retreat to Sunday.
	monthStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartSunday)

	// 2026-01-01 is a Thursday (weekday=4), subtract 4 days → 2025-12-28.
	if got := gridStart.Format("2006-01-02"); got != "2025-12-28" {
		t.Fatalf("gridStart: want 2025-12-28, got %s", got)
	}
	// January 2026 monthEnd = 2026-01-31, Weekday = Saturday(6) → gridEnd = 2026-01-31
	if got := gridEnd.Format("2006-01-02"); got != "2026-01-31" {
		t.Fatalf("gridEnd: want 2026-01-31, got %s", got)
	}
}

// TestCalendarDaysGridBoundsGridAlwaysStartsOnSunday ensures gridStart is
// always a Sunday (Weekday == 0), catching mutations to the sign or offset in
// line 51.
func TestCalendarDaysGridBoundsGridAlwaysStartsOnSunday(t *testing.T) {
	cases := []struct {
		year  int
		month time.Month
	}{
		{2026, time.January},
		{2026, time.February},
		{2026, time.March},
		{2026, time.April},
		{2026, time.May},
		{2026, time.June},
		{2026, time.July},
		{2026, time.August},
		{2026, time.September},
		{2026, time.October},
		{2026, time.November},
		{2026, time.December},
	}
	for _, c := range cases {
		monthStart := time.Date(c.year, c.month, 1, 0, 0, 0, 0, time.UTC)
		gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartSunday)
		if gridStart.Weekday() != time.Sunday {
			t.Errorf("%s: gridStart %s is not a Sunday (weekday=%d)",
				c.month, gridStart.Format("2006-01-02"), gridStart.Weekday())
		}
		// gridEnd must be a Saturday (line 52).
		if gridEnd.Weekday() != time.Saturday {
			t.Errorf("%s: gridEnd %s is not a Saturday (weekday=%d)",
				c.month, gridEnd.Format("2006-01-02"), gridEnd.Weekday())
		}
	}
}

// TestCalendarDaysGridBoundsMonthEndIsLastDayOfMonth verifies line 50:
// monthEnd must be the last calendar day of the given month, not the first day
// of the following month. We exercise this indirectly via the range that is
// actually included in the grid.
func TestCalendarDaysGridBoundsMonthEndIsLastDayOfMonth(t *testing.T) {
	// April 2026 ends on 2026-04-30 (Thursday). If AddDate(0,1,-1) were
	// AddDate(0,1,0) the monthEnd would be 2026-05-01 (Friday), shifting
	// gridEnd by one week.
	monthStart := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	_, gridEnd := calendarGridBounds(monthStart, models.WeekStartSunday)

	// April 2026 monthEnd = 2026-04-30 (Thursday, weekday=4)
	// gridEnd = 2026-04-30 + (6-4) = 2026-05-02
	if got := gridEnd.Format("2006-01-02"); got != "2026-05-02" {
		t.Fatalf("April 2026 gridEnd: want 2026-05-02, got %s", got)
	}
}

// TestCalendarDaysGridBoundsMondayFirst verifies the Monday-first week-start
// preference (issue #225): gridStart must retreat to the preceding Monday and
// gridEnd advance to the following Sunday, for every month of the year.
func TestCalendarDaysGridBoundsMondayFirst(t *testing.T) {
	for month := time.January; month <= time.December; month++ {
		monthStart := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
		gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartMonday)
		if gridStart.Weekday() != time.Monday {
			t.Errorf("%s: gridStart %s is not a Monday (weekday=%d)",
				month, gridStart.Format("2006-01-02"), gridStart.Weekday())
		}
		if gridEnd.Weekday() != time.Sunday {
			t.Errorf("%s: gridEnd %s is not a Sunday (weekday=%d)",
				month, gridEnd.Format("2006-01-02"), gridEnd.Weekday())
		}
	}
}

// TestCalendarDaysGridBoundsMondayFirstFebruary2026 pins concrete dates:
// February 2026 starts on a Sunday, so Monday-first must pull gridStart back a
// full week to 2026-01-26 (Monday) and pad gridEnd to 2026-03-01 (Sunday).
func TestCalendarDaysGridBoundsMondayFirstFebruary2026(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	gridStart, gridEnd := calendarGridBounds(monthStart, models.WeekStartMonday)
	if got := gridStart.Format("2006-01-02"); got != "2026-01-26" {
		t.Fatalf("gridStart: want 2026-01-26, got %s", got)
	}
	if got := gridEnd.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("gridEnd: want 2026-03-01, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// buildCalendarLogMaps – line 62 (first-log insertion / tie-break logic)
// ---------------------------------------------------------------------------

// TestCalendarDaysLogMapsFirstLogIsStoredEvenWithoutPrior ensures that when
// only a single log exists for a date it is stored (testing the !exists branch
// of line 62). A mutation that flips !exists → exists would store nothing,
// leaving IsPeriod=false even though the single log has IsPeriod=true.
func TestCalendarDaysLogMapsFirstLogIsStoredEvenWithoutPrior(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{
			ID:       1,
			Date:     time.Date(2026, time.February, 5, 9, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		},
	}

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)
	day := findCalendarDayStateByDateString(t, days, "2026-02-05")
	if !day.IsPeriod {
		t.Fatal("expected single log for 2026-02-05 to be stored; IsPeriod should be true")
	}
}

// TestCalendarDaysLogMapsLaterTimestampWinsOverEarlier verifies the
// logEntry.Date.After(existing.Date) branch at line 62. Two logs for the same
// calendar day: the later timestamp (12:00) has IsPeriod=false and must win
// over the earlier (08:00, IsPeriod=true). A mutation to Before would reverse
// the winner.
func TestCalendarDaysLogMapsLaterTimestampWinsOverEarlier(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// Slice order: early log first, late log second — both different wallclock
	// time on the same calendar day.
	logs := []models.DailyLog{
		{
			ID:       1,
			Date:     time.Date(2026, time.February, 5, 8, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		},
		{
			ID:       2,
			Date:     time.Date(2026, time.February, 5, 12, 0, 0, 0, time.UTC),
			IsPeriod: false,
			Flow:     models.FlowNone,
		},
	}

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)
	day := findCalendarDayStateByDateString(t, days, "2026-02-05")
	if day.IsPeriod {
		t.Fatal("expected later-timestamp log (IsPeriod=false) to win; got IsPeriod=true")
	}
}

// TestCalendarDaysLogMapsHasDataReflectsAllLogsForDay ensures that HasData
// aggregates across ALL logs for a calendar day (line 65), even when a
// later-timestamp log wins the tie-break but carries no data itself.
func TestCalendarDaysLogMapsHasDataReflectsAllLogsForDay(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{
			ID:       1,
			Date:     time.Date(2026, time.February, 5, 8, 0, 0, 0, time.UTC),
			IsPeriod: true, // carries data
			Flow:     models.FlowMedium,
		},
		{
			ID:   2,
			Date: time.Date(2026, time.February, 5, 12, 0, 0, 0, time.UTC),
			// No data: IsPeriod=false, Flow=none
		},
	}

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)
	day := findCalendarDayStateByDateString(t, days, "2026-02-05")
	if !day.HasData {
		t.Fatal("expected HasData=true because earlier log for the day has period data")
	}
	// OpenEditDirectly must be false because HasData=true.
	if day.OpenEditDirectly {
		t.Fatal("expected OpenEditDirectly=false when day has data")
	}
}

// ---------------------------------------------------------------------------
// appendCurrentBaselinePreFertile – line 121
// ---------------------------------------------------------------------------

// TestCalendarDaysPreFertileEndIsOneDayBeforeFertilityStart verifies that
// the pre-fertile window ends exactly one day before the fertility window
// starts (line 121: preFertileEnd = fertilityStart - 1 day). If mutated to
// AddDate(0,0,0) the pre-fertile window would overlap with the fertility window.
func TestCalendarDaysPreFertileEndIsOneDayBeforeFertilityStart(t *testing.T) {
	// LastPeriodStart = 2026-03-01, AveragePeriodLength = 5 days,
	// FertilityWindowStart = 2026-03-16 (provided explicitly).
	// Pre-fertile window should run 2026-03-06 .. 2026-03-15.
	// 2026-03-16 must NOT be pre-fertile (it is the first fertility day).
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		AveragePeriodLength:  5,
		LastPeriodStart:      time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

	// 2026-03-15 must be pre-fertile (last day of pre-fertile window).
	d15 := findCalendarDayStateByDateString(t, days, "2026-03-15")
	if !d15.IsPreFertile {
		t.Fatal("expected 2026-03-15 to be pre-fertile (last day before fertility start)")
	}

	// 2026-03-16 must NOT be pre-fertile (it is the first fertility day).
	d16 := findCalendarDayStateByDateString(t, days, "2026-03-16")
	if d16.IsPreFertile {
		t.Fatal("expected 2026-03-16 to NOT be pre-fertile (it is the fertility window start)")
	}
}

// ---------------------------------------------------------------------------
// appendFertilityWindow – line 149 (peak threshold: offset <= 2)
// ---------------------------------------------------------------------------

// TestCalendarDaysFertilityWindowPeakThreshold verifies the offset <= 2
// boundary at line 149. Days 0, 1, 2 before ovulation are peak; day 3 before
// ovulation is edge. A mutation to offset <= 1 would make day 2 an edge day.
//
// Fertility window: 2026-03-10 .. 2026-03-15, ovulation 2026-03-15.
//
//	offset from ovulation:
//	  2026-03-10 → 5 (edge)
//	  2026-03-12 → 3 (edge)
//	  2026-03-13 → 2 (peak ← boundary being tested)
//	  2026-03-14 → 1 (peak)
//	  2026-03-15 → 0 (ovulation, peak marker present)
func TestCalendarDaysFertilityWindowPeakThreshold(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		LastPeriodStart:      time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

	// offset == 2: 2026-03-13 must be IsFertilityPeak=true, IsFertilityEdge=false.
	d13 := findCalendarDayStateByDateString(t, days, "2026-03-13")
	if !d13.IsFertilityPeak {
		t.Fatalf("expected 2026-03-13 (offset 2) to be fertility peak, got %#v", d13)
	}
	if d13.IsFertilityEdge {
		t.Fatalf("expected 2026-03-13 (offset 2) to NOT be fertility edge, got %#v", d13)
	}

	// offset == 3: 2026-03-12 must be IsFertilityEdge=true, IsFertilityPeak=false.
	d12 := findCalendarDayStateByDateString(t, days, "2026-03-12")
	if d12.IsFertilityPeak {
		t.Fatalf("expected 2026-03-12 (offset 3) to NOT be fertility peak, got %#v", d12)
	}
	if !d12.IsFertilityEdge {
		t.Fatalf("expected 2026-03-12 (offset 3) to be fertility edge, got %#v", d12)
	}
}

// ---------------------------------------------------------------------------
// appendHistoricalCycles – line 199 (cycleLen from math.Round) and line 207
// (preFertileEnd = fertilityStart - 1)
// ---------------------------------------------------------------------------

// TestCalendarDaysHistoricalCycleLen28Days verifies that the cycle length
// at line 199 is correctly rounded via math.Round. Two cycle starts 27.5 days
// apart: math.Round gives 28, truncation gives 27. A 27-day cycle uses
// luteal=14 → ovulation on day 13, but a 28-day cycle puts ovulation on day 14.
// We verify the ovulation date matches a 28-day cycle (rounded).
//
// However, exact half-day offsets are hard to express in calendar dates.
// Instead, use two starts exactly N days apart and verify the derived ovulation
// date matches the expected cycle length.
func TestCalendarDaysHistoricalCycleLen28Days(t *testing.T) {
	// Cycle starts: 2026-01-01 → 2026-01-29 = 28 days.
	// With luteal=14: ovulationDay = 28 - 14 = 14 (1-based) → 2026-01-01 + 13 days = 2026-01-14.
	monthStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{Date: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), CycleStart: true, IsPeriod: true, Flow: models.FlowMedium},
		{Date: time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC), CycleStart: true, IsPeriod: true, Flow: models.FlowMedium},
	}
	stats := CycleStats{
		AverageCycleLength:  28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		LastPeriodStart:     time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC),
	}
	user := &models.User{ShowHistoricalPhases: true}

	days := BuildCalendarDayStates(user, monthStart, logs, stats, now, time.UTC)

	// Historical ovulation for the first cycle (starts 2026-01-01, 28 days):
	// ovulation = 2026-01-01 + (14-1) = 2026-01-14
	ovulationDay := findCalendarDayStateByDateString(t, days, "2026-01-14")
	if !ovulationDay.IsOvulation {
		t.Fatalf("expected historical ovulation on 2026-01-14 for 28-day cycle, got %#v", ovulationDay)
	}
}

// TestCalendarDaysHistoricalPreFertileEndIsOneDayBeforeFertilityStart checks
// line 207: preFertileEnd = fertilityStart - 1 day. The day immediately before
// fertilityStart must be pre-fertile, and fertilityStart itself must NOT be
// pre-fertile (it is the fertile window start).
//
// Cycle: 2026-01-01 .. 2026-01-29 (28 days), period=5, luteal=14.
// PredictCycleWindow: ovulation = day 14 = 2026-01-14; fertilityStart = 2026-01-09;
// preFertileStart = 2026-01-06 (after 5-day period); preFertileEnd = 2026-01-08.
// So 2026-01-08 must be pre-fertile and 2026-01-09 must NOT be pre-fertile.
func TestCalendarDaysHistoricalPreFertileEndIsOneDayBeforeFertilityStart(t *testing.T) {
	monthStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{Date: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), CycleStart: true, IsPeriod: true, Flow: models.FlowMedium},
		{Date: time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC), CycleStart: true, IsPeriod: true, Flow: models.FlowMedium},
	}
	stats := CycleStats{
		AverageCycleLength:  28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		LastPeriodStart:     time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC),
	}
	user := &models.User{ShowHistoricalPhases: true}

	days := BuildCalendarDayStates(user, monthStart, logs, stats, now, time.UTC)

	// fertilityStart = 2026-01-14 - 5 = 2026-01-09
	// preFertileEnd  = 2026-01-09 - 1 = 2026-01-08
	d08 := findCalendarDayStateByDateString(t, days, "2026-01-08")
	if !d08.IsPreFertile {
		t.Fatalf("expected 2026-01-08 to be pre-fertile (last day before fertility start), got %#v", d08)
	}

	d09 := findCalendarDayStateByDateString(t, days, "2026-01-09")
	if d09.IsPreFertile {
		t.Fatalf("expected 2026-01-09 to NOT be pre-fertile (it is the fertility window start), got %#v", d09)
	}
}

// ---------------------------------------------------------------------------
// appendPredictedWindow – line 228 (preFertileEnd = fertilityStart - 1)
// ---------------------------------------------------------------------------

// TestCalendarDaysPredictedWindowPreFertileEndBoundary verifies line 228:
// preFertileEnd must be one day before fertilityStart, so the last pre-fertile
// day and the first fertility day are distinct. Uses a future predicted cycle.
//
// NextPeriodStart = 2026-04-07, predictedCycleLength = 28, periodLength = 5,
// luteal default (0 → ResolveLutealPhase default).
// ovulationDay = 28 - 14 = 14 → 2026-04-07 + 13 = 2026-04-20.
// fertilityStart = 2026-04-20 - 5 = 2026-04-15.
// preFertileStart = 2026-04-07 + 5 = 2026-04-12.
// preFertileEnd   = 2026-04-15 - 1 = 2026-04-14.
func TestCalendarDaysPredictedWindowPreFertileEndBoundary(t *testing.T) {
	monthStart := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		MedianCycleLength:   28,
		AveragePeriodLength: 5,
		LutealPhase:         14,
		LastPeriodStart:     time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:     time.Date(2026, time.April, 7, 0, 0, 0, 0, time.UTC),
		OvulationDate:       time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

	// preFertileEnd = 2026-04-14 must be pre-fertile.
	d14 := findCalendarDayStateByDateString(t, days, "2026-04-14")
	if !d14.IsPreFertile {
		t.Fatalf("expected 2026-04-14 to be pre-fertile (last day before predicted fertility start), got %#v", d14)
	}

	// fertilityStart = 2026-04-15 must NOT be pre-fertile.
	d15 := findCalendarDayStateByDateString(t, days, "2026-04-15")
	if d15.IsPreFertile {
		t.Fatalf("expected 2026-04-15 to NOT be pre-fertile (it is the predicted fertility window start), got %#v", d15)
	}
}

// ---------------------------------------------------------------------------
// buildCalendarDayState – line 268 (InMonth) and line 269 (IsToday)
// ---------------------------------------------------------------------------

// TestCalendarDaysInMonthDistinguishesGridPaddingDays verifies line 268:
// days whose month differs from monthStart must have InMonth=false. Grid
// padding days (before/after the target month) must be marked as outside the
// current month. A mutation that replaces == with != would invert InMonth for
// every day.
func TestCalendarDaysInMonthDistinguishesGridPaddingDays(t *testing.T) {
	// January 2026 starts on Thursday → gridStart = 2025-12-28.
	// Days 2025-12-28 through 2025-12-31 must be InMonth=false.
	monthStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	days := BuildCalendarDayStates(nil, monthStart, nil, CycleStats{}, now, time.UTC)

	// Padding days before January must be outside the month.
	for _, ds := range []string{"2025-12-28", "2025-12-29", "2025-12-30", "2025-12-31"} {
		d := findCalendarDayStateByDateString(t, days, ds)
		if d.InMonth {
			t.Errorf("expected %s (December) to have InMonth=false for January grid, got InMonth=true", ds)
		}
	}

	// January days must be inside the month.
	for _, ds := range []string{"2026-01-01", "2026-01-15", "2026-01-31"} {
		d := findCalendarDayStateByDateString(t, days, ds)
		if !d.InMonth {
			t.Errorf("expected %s (January) to have InMonth=true for January grid, got InMonth=false", ds)
		}
	}
}

// TestCalendarDaysIsTodayMatchesExactDate verifies line 269: IsToday must
// be true for exactly the current date and false for all adjacent days. A
// mutation that changes == to != would invert IsToday for every day.
func TestCalendarDaysIsTodayMatchesExactDate(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 15, 9, 30, 0, 0, time.UTC) // mid-day

	days := BuildCalendarDayStates(nil, monthStart, nil, CycleStats{}, now, time.UTC)

	today := findCalendarDayStateByDateString(t, days, "2026-03-15")
	if !today.IsToday {
		t.Fatal("expected 2026-03-15 to be IsToday=true, got false")
	}

	yesterday := findCalendarDayStateByDateString(t, days, "2026-03-14")
	if yesterday.IsToday {
		t.Fatal("expected 2026-03-14 to be IsToday=false, got true")
	}

	tomorrow := findCalendarDayStateByDateString(t, days, "2026-03-16")
	if tomorrow.IsToday {
		t.Fatal("expected 2026-03-16 to be IsToday=false, got true")
	}
}

// ---------------------------------------------------------------------------
// buildCalendarDayState – line 280 (HasSex)
// ---------------------------------------------------------------------------

// TestCalendarDaysHasSexIsTrueWhenLogHasSexActivity verifies line 280:
// HasSex must be true when the log carries a non-none sex activity value. A
// mutation to == models.SexActivityNone would invert HasSex.
func TestCalendarDaysHasSexIsTrueWhenLogHasSexActivity(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{
			ID:          1,
			Date:        time.Date(2026, time.March, 5, 9, 0, 0, 0, time.UTC),
			SexActivity: models.SexActivityProtected,
		},
		{
			ID:   2,
			Date: time.Date(2026, time.March, 6, 9, 0, 0, 0, time.UTC),
			// No sex activity
		},
	}

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)

	d5 := findCalendarDayStateByDateString(t, days, "2026-03-05")
	if !d5.HasSex {
		t.Fatal("expected HasSex=true for day with protected sex activity")
	}

	d6 := findCalendarDayStateByDateString(t, days, "2026-03-06")
	if d6.HasSex {
		t.Fatal("expected HasSex=false for day with no sex activity")
	}
}

// TestCalendarDaysHasSexIsTrueForUnprotectedActivity verifies that
// unprotected sex activity also sets HasSex=true (NormalizeDaySexActivity
// must return non-none for "unprotected").
func TestCalendarDaysHasSexIsTrueForUnprotectedActivity(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{
			ID:          1,
			Date:        time.Date(2026, time.March, 5, 9, 0, 0, 0, time.UTC),
			SexActivity: models.SexActivityUnprotected,
		},
	}

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)

	d5 := findCalendarDayStateByDateString(t, days, "2026-03-05")
	if !d5.HasSex {
		t.Fatal("expected HasSex=true for day with unprotected sex activity")
	}
}

// TestCalendarDaysHasSexFalseWhenNoLog verifies that a day with no log
// entry has HasSex=false (the hasEntry guard in line 280 prevents a nil-log
// false positive).
func TestCalendarDaysHasSexFalseWhenNoLog(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	days := BuildCalendarDayStates(nil, monthStart, nil, CycleStats{}, now, time.UTC)

	d5 := findCalendarDayStateByDateString(t, days, "2026-03-05")
	if d5.HasSex {
		t.Fatal("expected HasSex=false for day with no log entry")
	}
}
