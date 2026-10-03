package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testenv"
)

// The leap-day coverage in cycle_leap_day_test.go stops at PredictCycleWindow.
// These tests carry the same hand-counted calendar (February has 29 days in 2024,
// 28 in 2025 and 2100 — 2100 is divisible by 100 but not by 400) into the three
// places a cycle is measured: the cycle-length path, the calendar day difference
// every reader shares, and the BBT detection window. Every expected number is
// counted on the printed calendar, never derived from the implementation, and
// each case asserts both the span in calendar days and the cycle day it implies
// (one-based, so the day a cycle ends on is its length and the next start is
// length+1).
//
// The UTC rows hold the calendar and need no zone database. The rows in a
// non-UTC zone exist because a day difference taken as Sub(...).Hours()/24 is
// exact between two UTC midnights and wrong once an operand is a location
// midnight: a zone west of UTC against a UTC one (New York), two midnights of
// one zone across a DST jump (New York 2024-03-10, Berlin 2024-03-31), and a
// zone of +12h or more, where rounding instead of truncating also fails
// (Auckland, +13 in February and March 2024). A zone offset below 12h, such as
// Kolkata, is kept only as a control: it truncates to the right count.

func TestCycleLengthsAcrossFebruary29(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		starts      []string
		wantLengths []int
	}{
		{name: "leap year cycle spans Feb 29", starts: []string{"2024-02-15", "2024-03-14"}, wantLengths: []int{28}},
		{name: "same dates in a non-leap year are a day shorter", starts: []string{"2025-02-15", "2025-03-14"}, wantLengths: []int{27}},
		{name: "century year 2100 has no Feb 29", starts: []string{"2100-02-15", "2100-03-14"}, wantLengths: []int{27}},
		{name: "century year 2000 has Feb 29", starts: []string{"2000-02-15", "2000-03-14"}, wantLengths: []int{28}},
		{name: "cycle starting on the leap day", starts: []string{"2024-02-01", "2024-02-29", "2024-03-28"}, wantLengths: []int{28, 28}},
		{name: "cycle ending on the leap day", starts: []string{"2024-02-01", "2024-02-29"}, wantLengths: []int{28}},
		{name: "cycle starting on Feb 28 and ending on Mar 8", starts: []string{"2024-02-28", "2024-03-08"}, wantLengths: []int{9}},
		{name: "same Feb 28 start in a non-leap year", starts: []string{"2025-02-28", "2025-03-08"}, wantLengths: []int{8}},
		{name: "three cycles across the new year and the leap day", starts: []string{"2023-12-20", "2024-01-17", "2024-02-14", "2024-03-13"}, wantLengths: []int{28, 28, 28}},
		{name: "same run across 2100 loses a day at the end", starts: []string{"2099-12-20", "2100-01-17", "2100-02-14", "2100-03-13"}, wantLengths: []int{28, 28, 27}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			days := make([]time.Time, 0, len(testCase.starts))
			for _, raw := range testCase.starts {
				days = append(days, mustParseDay(t, raw))
			}

			lengths := CycleLengths(periodLogsOn(days...))
			if len(lengths) != len(testCase.wantLengths) {
				t.Fatalf("CycleLengths = %v, want %v", lengths, testCase.wantLengths)
			}
			for index, want := range testCase.wantLengths {
				if lengths[index] != want {
					t.Errorf("cycle %d (%s -> %s) = %d days, want %d", index, testCase.starts[index], testCase.starts[index+1], lengths[index], want)
				}
				start, next := days[index], days[index+1]
				if got := cycleDayAt(start, next.AddDate(0, 0, -1)); got != want {
					t.Errorf("cycle %d: the day before the next start is cycle day %d, want %d", index, got, want)
				}
				if got := cycleDayAt(start, next); got != want+1 {
					t.Errorf("cycle %d: the next start is cycle day %d of the old cycle, want %d", index, got, want+1)
				}
			}
		})
	}
}

// cycleLengths receives its starts as a parameter, so the operands may be
// location midnights: the same spans as above, measured between instants that
// are not UTC midnights.
func TestCycleLengthsAcrossFebruary29NonUTCStarts(t *testing.T) {
	t.Parallel()

	newYork := testenv.RequireTimeZone(t, "America/New_York")
	berlin := testenv.RequireTimeZone(t, "Europe/Berlin")
	auckland := testenv.RequireTimeZone(t, "Pacific/Auckland")

	cases := []struct {
		name       string
		from       time.Time
		to         time.Time
		wantLength int
	}{
		{name: "New York midnights across the leap day and the March DST jump", from: time.Date(2024, time.February, 29, 0, 0, 0, 0, newYork), to: time.Date(2024, time.March, 28, 0, 0, 0, 0, newYork), wantLength: 28},
		{name: "New York midnight to a UTC midnight", from: time.Date(2024, time.February, 29, 0, 0, 0, 0, newYork), to: time.Date(2024, time.March, 28, 0, 0, 0, 0, time.UTC), wantLength: 28},
		{name: "New York midnight to a UTC midnight, non-leap year", from: time.Date(2025, time.February, 28, 0, 0, 0, 0, newYork), to: time.Date(2025, time.March, 28, 0, 0, 0, 0, time.UTC), wantLength: 28},
		{name: "Berlin midnights across the leap day and the March DST jump", from: time.Date(2024, time.February, 29, 0, 0, 0, 0, berlin), to: time.Date(2024, time.April, 1, 0, 0, 0, 0, berlin), wantLength: 32},
		{name: "Berlin midnights across 2100 and its March DST jump", from: time.Date(2100, time.February, 28, 0, 0, 0, 0, berlin), to: time.Date(2100, time.April, 1, 0, 0, 0, 0, berlin), wantLength: 32},
		// +13 in February and March 2024: 28 days and 13 hours to the UTC
		// midnight, which rounding would call 29.
		{name: "Auckland midnight to a UTC midnight, +13 across the leap day", from: time.Date(2024, time.February, 29, 0, 0, 0, 0, auckland), to: time.Date(2024, time.March, 28, 0, 0, 0, 0, time.UTC), wantLength: 28},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lengths := cycleLengths([]time.Time{testCase.from, testCase.to})
			if len(lengths) != 1 || lengths[0] != testCase.wantLength {
				t.Fatalf("cycleLengths = %v, want [%d]", lengths, testCase.wantLength)
			}
			if got := cycleDayAt(testCase.from, testCase.to); got != testCase.wantLength+1 {
				t.Errorf("the next start is cycle day %d of the old cycle, want %d", got, testCase.wantLength+1)
			}
		})
	}
}

func TestCalendarDaysBetweenAcrossFebruary29(t *testing.T) {
	t.Parallel()

	at := func(location *time.Location, year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, location)
	}

	cases := []dayDiffCase{
		{name: "UTC Feb 28 to Mar 1, leap year", from: at(time.UTC, 2024, time.February, 28), to: at(time.UTC, 2024, time.March, 1), wantDays: 2},
		{name: "UTC Feb 28 to Mar 1, non-leap year", from: at(time.UTC, 2025, time.February, 28), to: at(time.UTC, 2025, time.March, 1), wantDays: 1},
		{name: "UTC Feb 28 to Mar 1, 2100", from: at(time.UTC, 2100, time.February, 28), to: at(time.UTC, 2100, time.March, 1), wantDays: 1},
		{name: "UTC Feb 29 to Mar 1", from: at(time.UTC, 2024, time.February, 29), to: at(time.UTC, 2024, time.March, 1), wantDays: 1},
		{name: "UTC Feb 1 to Mar 1, leap year", from: at(time.UTC, 2024, time.February, 1), to: at(time.UTC, 2024, time.March, 1), wantDays: 29},
		{name: "UTC Feb 1 to Mar 1, 2100", from: at(time.UTC, 2100, time.February, 1), to: at(time.UTC, 2100, time.March, 1), wantDays: 28},
		{name: "UTC Jan 1 to Jan 1 over 2024", from: at(time.UTC, 2024, time.January, 1), to: at(time.UTC, 2025, time.January, 1), wantDays: 366},
		{name: "UTC Jan 1 to Jan 1 over 2100", from: at(time.UTC, 2100, time.January, 1), to: at(time.UTC, 2101, time.January, 1), wantDays: 365},
		{name: "UTC Feb 29 2024 to Feb 28 2025", from: at(time.UTC, 2024, time.February, 29), to: at(time.UTC, 2025, time.February, 28), wantDays: 365},
		{name: "backward across the leap day", from: at(time.UTC, 2024, time.March, 1), to: at(time.UTC, 2024, time.February, 28), wantDays: -2},
		{name: "the leap day against itself is day 1 of the cycle", from: at(time.UTC, 2024, time.February, 29), to: at(time.UTC, 2024, time.February, 29), wantDays: 0},
	}

	runDayDiffCases(t, cases)
}

// The rows that need a zone database; the UTC rows above run without one.
func TestCalendarDaysBetweenAcrossFebruary29InZones(t *testing.T) {
	t.Parallel()

	newYork := testenv.RequireTimeZone(t, "America/New_York")
	berlin := testenv.RequireTimeZone(t, "Europe/Berlin")
	kolkata := testenv.RequireTimeZone(t, "Asia/Kolkata")
	auckland := testenv.RequireTimeZone(t, "Pacific/Auckland")

	at := func(location *time.Location, year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, location)
	}

	cases := []dayDiffCase{
		{name: "New York midnights across the leap day and the March DST jump", from: at(newYork, 2024, time.February, 29), to: at(newYork, 2024, time.March, 11), wantDays: 11},
		{name: "New York midnight to UTC midnight", from: at(newYork, 2024, time.February, 28), to: at(time.UTC, 2024, time.March, 1), wantDays: 2},
		{name: "UTC midnight to New York midnight", from: at(time.UTC, 2024, time.February, 28), to: at(newYork, 2024, time.March, 1), wantDays: 2},
		{name: "New York midnights across 2100 and its March DST jump", from: at(newYork, 2100, time.February, 28), to: at(newYork, 2100, time.March, 15), wantDays: 15},
		{name: "Berlin midnights across the leap day and the March DST jump", from: at(berlin, 2024, time.February, 29), to: at(berlin, 2024, time.April, 1), wantDays: 32},
		{name: "Berlin midnights across 2100 and its March DST jump", from: at(berlin, 2100, time.February, 28), to: at(berlin, 2100, time.April, 1), wantDays: 32},
		// The two Kolkata rows are controls: 29.5 and 18.5 hours truncate to the
		// right day count too, so they do not tell the hour arithmetic apart.
		{name: "control: Kolkata midnight to UTC midnight", from: at(kolkata, 2024, time.February, 29), to: at(time.UTC, 2024, time.March, 1), wantDays: 1},
		{name: "control: UTC midnight to Kolkata midnight", from: at(time.UTC, 2024, time.February, 29), to: at(kolkata, 2024, time.March, 1), wantDays: 1},
		// Auckland is +13 in February and March 2024: its midnight is 11:00 UTC
		// the day before. 37 hours to the UTC midnight is 1 day and a half-day
		// shortfall that rounding would turn into 2; 11 hours the other way
		// rounds to 0.
		{name: "Auckland midnight to UTC midnight, +13 across the leap day", from: at(auckland, 2024, time.February, 29), to: at(time.UTC, 2024, time.March, 1), wantDays: 1},
		{name: "UTC midnight to Auckland midnight, +13 across the leap day", from: at(time.UTC, 2024, time.February, 29), to: at(auckland, 2024, time.March, 1), wantDays: 1},
	}

	runDayDiffCases(t, cases)
}

type dayDiffCase struct {
	name     string
	from     time.Time
	to       time.Time
	wantDays int
}

func runDayDiffCases(t *testing.T, cases []dayDiffCase) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := CalendarDaysBetween(testCase.from, testCase.to); got != testCase.wantDays {
				t.Fatalf("CalendarDaysBetween(%s, %s) = %d, want %d", testCase.from.Format(time.RFC3339), testCase.to.Format(time.RFC3339), got, testCase.wantDays)
			}
			wantCycleDay := testCase.wantDays + 1
			if testCase.wantDays < 0 {
				wantCycleDay = 0
			}
			if got := cycleDayAt(testCase.from, testCase.to); got != wantCycleDay {
				t.Errorf("cycleDayAt(%s, %s) = %d, want %d", testCase.from.Format(time.RFC3339), testCase.to.Format(time.RFC3339), got, wantCycleDay)
			}
		})
	}
}

// bbtLogsOn builds one valid reading per supplied calendar day at UTC midnight,
// the shape DailyLog.Date is stored in.
func bbtLogsOn(days []time.Time, values []float64) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(days))
	for index, day := range days {
		value := values[index]
		logs = append(logs, models.DailyLog{Date: day, BBT: &value})
	}
	return logs
}

// TestCollectCycleBBTPointsNumbersDaysAcrossFebruary29 pins the cycle day each
// reading gets inside the detection window [cycleStart, seriesEnd). Cycle day 1
// is the start itself, so 2024-02-25 is day 1, the leap day is day 5 and
// 2024-03-01 day 6; the same start in 2025 and 2100 reaches Mar 1 on day 5.
//
// Every row also holds a reading on the day BEFORE the cycle start, which the
// window must drop.
func TestCollectCycleBBTPointsNumbersDaysAcrossFebruary29(t *testing.T) {
	t.Parallel()

	cases := []collectCase{
		{name: "UTC leap year", location: time.UTC, cycleStart: "2024-02-25", readings: []string{"2024-02-25", "2024-02-28", "2024-02-29", "2024-03-01", "2024-03-11"}, wantCycleDay: []int{1, 4, 5, 6, 16}},
		{name: "UTC non-leap year", location: time.UTC, cycleStart: "2025-02-25", readings: []string{"2025-02-25", "2025-02-28", "2025-03-01", "2025-03-02", "2025-03-11"}, wantCycleDay: []int{1, 4, 5, 6, 15}},
		{name: "UTC 2100", location: time.UTC, cycleStart: "2100-02-25", readings: []string{"2100-02-25", "2100-02-28", "2100-03-01", "2100-03-02", "2100-03-11"}, wantCycleDay: []int{1, 4, 5, 6, 15}},
	}

	runCollectCases(t, cases)
}

type collectCase struct {
	name         string
	location     *time.Location
	cycleStart   string
	readings     []string
	wantCycleDay []int
}

// The rows that need a zone database; the UTC rows above run without one.
func TestCollectCycleBBTPointsNumbersDaysAcrossFebruary29InZones(t *testing.T) {
	t.Parallel()

	newYork := testenv.RequireTimeZone(t, "America/New_York")
	berlin := testenv.RequireTimeZone(t, "Europe/Berlin")

	cases := []collectCase{
		{name: "New York leap year across the March DST jump", location: newYork, cycleStart: "2024-02-25", readings: []string{"2024-02-25", "2024-02-28", "2024-02-29", "2024-03-01", "2024-03-11"}, wantCycleDay: []int{1, 4, 5, 6, 16}},
		{name: "New York 2100 across the March DST jump", location: newYork, cycleStart: "2100-02-25", readings: []string{"2100-02-25", "2100-02-28", "2100-03-01", "2100-03-02", "2100-03-15"}, wantCycleDay: []int{1, 4, 5, 6, 19}},
		{name: "Berlin leap year across the late March DST jump", location: berlin, cycleStart: "2024-02-25", readings: []string{"2024-02-25", "2024-02-29", "2024-03-01", "2024-04-01"}, wantCycleDay: []int{1, 5, 6, 37}},
	}

	runCollectCases(t, cases)
}

func runCollectCases(t *testing.T, cases []collectCase) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			days := make([]time.Time, 0, len(testCase.readings))
			values := make([]float64, 0, len(testCase.readings))
			for _, raw := range testCase.readings {
				days = append(days, mustParseDay(t, raw))
				values = append(values, 36.4)
			}
			cycleStart := CalendarDay(mustParseDay(t, testCase.cycleStart), testCase.location)
			seriesEnd := AddCalendarDays(CalendarDay(days[len(days)-1], testCase.location), 1, testCase.location)

			beforeStart := 36.4
			logs := append(bbtLogsOn(days, values), models.DailyLog{Date: mustParseDay(t, testCase.cycleStart).AddDate(0, 0, -1), BBT: &beforeStart})

			points := collectCycleBBTPoints(logs, cycleStart, seriesEnd, testCase.location)
			if len(points) != len(testCase.wantCycleDay) {
				t.Fatalf("collected %d point(s), want %d", len(points), len(testCase.wantCycleDay))
			}
			for index, point := range points {
				if point.CycleDay != testCase.wantCycleDay[index] {
					t.Errorf("reading on %s is cycle day %d, want %d", testCase.readings[index], point.CycleDay, testCase.wantCycleDay[index])
				}
				if got := point.Date.Format("2006-01-02"); got != testCase.readings[index] {
					t.Errorf("point %d is on %s, want %s", index, got, testCase.readings[index])
				}
			}

			// seriesEnd is exclusive: a window ending on the last reading's own
			// day leaves that reading out.
			shorter := collectCycleBBTPoints(logs, cycleStart, CalendarDay(days[len(days)-1], testCase.location), testCase.location)
			if len(shorter) != len(points)-1 {
				t.Errorf("a window ending on the last reading's day holds %d point(s), want %d", len(shorter), len(points)-1)
			}
		})
	}
}

// TestInferBBTOvulationDateAcrossFebruary29 runs the detector on a cycle that
// starts on Feb 25: six low readings, then three elevated ones from cycle day 7.
// The estimate is the day before the first elevated day, cycle day 6, which is
// 2024-03-01 in the leap year (Feb 25 is day 1, Feb 29 day 5) and 2025-03-02
// and 2100-03-02 where February ends on the 28th.
func TestInferBBTOvulationDateAcrossFebruary29(t *testing.T) {
	t.Parallel()

	runInferCases(t, []inferCase{
		{name: "UTC leap year", location: time.UTC, cycleStart: "2024-02-25", wantDate: "2024-03-01"},
		{name: "UTC non-leap year", location: time.UTC, cycleStart: "2025-02-25", wantDate: "2025-03-02"},
		{name: "UTC 2100", location: time.UTC, cycleStart: "2100-02-25", wantDate: "2100-03-02"},
	})
}

type inferCase struct {
	name       string
	location   *time.Location
	cycleStart string
	wantDate   string
}

// The rows that need a zone database. The Havana row is not a leap-day row: its
// estimate lands on 2026-03-08, a date whose local midnight Havana skips, which
// is the one place stepping from the zone anchor instead of a UTC one names the
// day before.
func TestInferBBTOvulationDateAcrossFebruary29InZones(t *testing.T) {
	t.Parallel()

	newYork := testenv.RequireTimeZone(t, "America/New_York")
	havana := testenv.RequireTimeZone(t, "America/Havana")

	runInferCases(t, []inferCase{
		{name: "New York leap year", location: newYork, cycleStart: "2024-02-25", wantDate: "2024-03-01"},
		{name: "New York 2100", location: newYork, cycleStart: "2100-02-25", wantDate: "2100-03-02"},
		{name: "Havana estimate on a skipped local midnight", location: havana, cycleStart: "2026-03-03", wantDate: "2026-03-08"},
	})
}

func runInferCases(t *testing.T, cases []inferCase) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			start := mustParseDay(t, testCase.cycleStart)
			values := []float64{36.30, 36.30, 36.30, 36.30, 36.30, 36.30, 36.60, 36.60, 36.80}
			days := make([]time.Time, 0, len(values))
			for offset := range values {
				days = append(days, start.AddDate(0, 0, offset))
			}
			cycleStart := CalendarDay(start, testCase.location)
			seriesEnd := AddCalendarDays(CalendarDay(days[len(days)-1], testCase.location), 1, testCase.location)

			got := inferBBTOvulationDate(bbtLogsOn(days, values), cycleStart, seriesEnd, testCase.location)
			if got.IsZero() {
				t.Fatal("no ovulation inferred from a clear three-over-six shift")
			}
			if key := got.Format("2006-01-02"); key != testCase.wantDate {
				t.Errorf("inferred ovulation %s, want %s", key, testCase.wantDate)
			}
		})
	}
}
