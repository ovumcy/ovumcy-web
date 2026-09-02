package services

// cycle_signals_dst_test.go — both ovulation inferrers step a calendar day
// from the cycle window, and that step must not be taken inside the request
// zone.
//
// inferBBTOvulationDate returned cycleStart.AddDate(0, 0, ovulationCycleDay-1)
// and inferEggWhiteOvulationDate returned lastEggWhite.AddDate(0, 0, 1), both
// anchored at location midnight. AddDate re-enters time.Date in that location,
// and in a UTC-minus zone whose DST jump lands on midnight (America/Santiago
// 2026-09-06, America/Havana 2026-03-08) no instant exists at 00:00 local on
// that date: the missing wall clock normalizes BACKWARD into the previous
// calendar day. Measured on tzdata 2025c —
// 2026-09-01T00:00-04 plus five days is 2026-09-05T23:00-04, whose .Date() is
// September 5. Every reader of the returned value takes its calendar
// components, so the inference NAMED the day before the one it found.
//
// Two surfaces read it, and both were wrong by a day on those dates: the
// observed-luteal sample (InferUserLutealPhase), and — since the calendar's
// solid ovulation marker moved onto the detector's own date — the calendar
// grid. The fix anchors the step in UTC, the way calendarGridBounds already
// does; the egg-white clamp moves to CalendarDaysBetween in the same step,
// because a UTC-midnight day compared as an INSTANT against a UTC-minus zone's
// midnight reads as earlier and would let the estimate land on the next cycle
// start itself.
//
// The zone must be west of UTC: east-of-UTC zones normalize a missing midnight
// forward and keep the date, so the same fixture there is green about nothing.
// Controls: UTC over the identical fixtures.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// havanaTestLocation is the second west-of-UTC zone whose DST jump lands on
// midnight (2026-03-08). santiagoTestLocation already exists in this package
// (day_feedback_dst_walk_test.go).
func havanaTestLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatalf("load America/Havana: %v", err)
	}
	return location
}

// bbtCycleAroundASkippedMidnight builds a cycle whose confirmed ovulation is
// the sixth cycle day: six undisturbed low readings fill the coverline window
// (cycle days 1-6), then three consecutive elevated days (7, 8, 9) trip the
// shared 3-over-6 detector. firstHighDay is day 7, so the detector names day 6
// — the day whose local midnight the zone skips.
func bbtCycleAroundASkippedMidnight(t *testing.T, days [9]string) []models.DailyLog {
	t.Helper()
	logs := make([]models.DailyLog, 0, len(days))
	for index, day := range days {
		value := 36.20
		if index >= 6 {
			value = 36.50
		}
		logs = append(logs, cyclesignalsCovBBTLog(t, day, value))
	}
	return logs
}

// TestInferBBTOvulationDateNamesTheSkippedMidnightDay is the red-before-fix
// case: the detector finds the shift either way, so the assertion is on the
// DATE the inference returns, not on whether it fired.
func TestInferBBTOvulationDateNamesTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)
	havana := havanaTestLocation(t)

	santiagoDays := [9]string{
		"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05",
		"2026-09-06", "2026-09-07", "2026-09-08", "2026-09-09",
	}
	havanaDays := [9]string{
		"2026-03-03", "2026-03-04", "2026-03-05", "2026-03-06", "2026-03-07",
		"2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11",
	}

	testCases := []struct {
		name          string
		location      *time.Location
		days          [9]string
		cycleStartDay string
		nextStartDay  string
		want          string
	}{
		{
			name:          "santiago: ovulation on the day local midnight is skipped",
			location:      santiago,
			days:          santiagoDays,
			cycleStartDay: "2026-09-01",
			nextStartDay:  "2026-09-29",
			want:          "2026-09-06",
		},
		{
			name:          "havana: ovulation on the day local midnight is skipped",
			location:      havana,
			days:          havanaDays,
			cycleStartDay: "2026-03-03",
			nextStartDay:  "2026-03-31",
			want:          "2026-03-08",
		},
		{
			name:          "control: the same september fixture in UTC",
			location:      time.UTC,
			days:          santiagoDays,
			cycleStartDay: "2026-09-01",
			nextStartDay:  "2026-09-29",
			want:          "2026-09-06",
		},
		{
			name:          "control: the same march fixture in UTC",
			location:      time.UTC,
			days:          havanaDays,
			cycleStartDay: "2026-03-03",
			nextStartDay:  "2026-03-31",
			want:          "2026-03-08",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Anchor the window exactly as InferUserLutealPhase and the calendar
			// grid do: CalendarDay in the request location.
			cycleStart := CalendarDay(cyclesignalsCovDay(t, testCase.cycleStartDay), testCase.location)
			nextStart := CalendarDay(cyclesignalsCovDay(t, testCase.nextStartDay), testCase.location)

			result := inferBBTOvulationDate(bbtCycleAroundASkippedMidnight(t, testCase.days), cycleStart, nextStart, testCase.location)
			if result.IsZero() {
				t.Fatal("expected the 3-over-6 detector to confirm a shift for this fixture")
			}
			if got := CalendarDayKey(result); got != testCase.want {
				t.Fatalf("inferred ovulation = %s, want %s (the step must not be taken from a location midnight the zone skips)", got, testCase.want)
			}
		})
	}
}

// TestInferEggWhiteOvulationDateNamesTheSkippedMidnightDay is the same defect
// on the fallback inferrer: peak day + 1 lands on the skipped midnight.
func TestInferEggWhiteOvulationDateNamesTheSkippedMidnightDay(t *testing.T) {
	santiago := santiagoTestLocation(t)
	havana := havanaTestLocation(t)

	testCases := []struct {
		name          string
		location      *time.Location
		peakDay       string
		cycleStartDay string
		nextStartDay  string
		want          string
	}{
		{
			name:          "santiago: peak + 1 is the day local midnight is skipped",
			location:      santiago,
			peakDay:       "2026-09-05",
			cycleStartDay: "2026-09-01",
			nextStartDay:  "2026-09-29",
			want:          "2026-09-06",
		},
		{
			name:          "havana: peak + 1 is the day local midnight is skipped",
			location:      havana,
			peakDay:       "2026-03-07",
			cycleStartDay: "2026-03-03",
			nextStartDay:  "2026-03-31",
			want:          "2026-03-08",
		},
		{
			name:          "control: the same september fixture in UTC",
			location:      time.UTC,
			peakDay:       "2026-09-05",
			cycleStartDay: "2026-09-01",
			nextStartDay:  "2026-09-29",
			want:          "2026-09-06",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cycleStart := CalendarDay(cyclesignalsCovDay(t, testCase.cycleStartDay), testCase.location)
			nextStart := CalendarDay(cyclesignalsCovDay(t, testCase.nextStartDay), testCase.location)

			logs := []models.DailyLog{cyclesignalsCovMucusLog(t, testCase.peakDay, models.CervicalMucusEggWhite)}
			result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, testCase.location)
			if result.IsZero() {
				t.Fatal("expected an ovulation estimate from the egg-white peak")
			}
			if got := CalendarDayKey(result); got != testCase.want {
				t.Fatalf("inferred ovulation = %s, want %s (peak + 1 must not be stepped from a location midnight the zone skips)", got, testCase.want)
			}
		})
	}
}

// TestInferEggWhiteOvulationDateStillClampsAPeakOnTheLastCycleDay guards the
// half of the fix that is invisible in the cases above: once the step leaves
// the request zone, the clamp has to leave it too. A UTC-midnight day is an
// EARLIER instant than the same day anchored in a UTC-minus zone, so a clamp
// left on `estimated.Before(nextStart)` reads the next cycle start as still
// ahead and returns it as the ovulation estimate.
func TestInferEggWhiteOvulationDateStillClampsAPeakOnTheLastCycleDay(t *testing.T) {
	santiago := santiagoTestLocation(t)

	testCases := []struct {
		name     string
		location *time.Location
	}{
		{name: "santiago, west of UTC", location: santiago},
		{name: "control: UTC", location: time.UTC},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Peak on 2026-09-06, next cycle start on 2026-09-07: peak + 1 reaches
			// the next start exactly, so the estimate must fall back to the peak.
			cycleStart := CalendarDay(cyclesignalsCovDay(t, "2026-08-11"), testCase.location)
			nextStart := CalendarDay(cyclesignalsCovDay(t, "2026-09-07"), testCase.location)

			logs := []models.DailyLog{cyclesignalsCovMucusLog(t, "2026-09-06", models.CervicalMucusEggWhite)}
			result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, testCase.location)
			if got := CalendarDayKey(result); got != "2026-09-06" {
				t.Fatalf("inferred ovulation = %s, want 2026-09-06: peak + 1 reaches the next cycle start, so the estimate clamps to the peak day", got)
			}
		})
	}
}
