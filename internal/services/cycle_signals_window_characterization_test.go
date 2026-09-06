package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These tests CHARACTERISE the rule the BBT detector applies and its deliberate
// departures from the full symptothermal protocol: an expectation here changes
// only by a product decision about the method, never as a repair.
//
// A third elevated day one hundredth short of the required margin is already
// pinned by TestBBTShiftThirdDayMarginHoldsAtExactlyTwoTenths ("one hundredth
// short of the margin is rejected") and is not restated here.
//
// Values sit on the 0.05 °C grid so the rounding onto stored units never decides
// a case.

// cyclesignalsWindowReading is one recorded temperature addressed by cycle day,
// so a fixture states the gap it is about rather than hiding it in dates.
type cyclesignalsWindowReading struct {
	cycleDay  int
	value     float64
	disturbed bool
}

const (
	cyclesignalsWindowCycleStart = "2026-01-01"
	cyclesignalsWindowNextStart  = "2026-01-29"
)

// cyclesignalsWindowShift is the elevated run every fixture below ends with. It
// clears BOTH coverlines its fixtures can produce, and clears the third-day
// margin above either with room to spare, so a case here turns on the window
// that was measured and never on the run that followed it: the margin boundary
// belongs to TestBBTShiftThirdDayMarginHoldsAtExactlyTwoTenths.
var cyclesignalsWindowShift = []cyclesignalsWindowReading{
	{cycleDay: 12, value: 36.40},
	{cycleDay: 13, value: 36.40},
	{cycleDay: 14, value: 36.60},
}

func cyclesignalsWindowLogs(t *testing.T, readings []cyclesignalsWindowReading) []models.DailyLog {
	t.Helper()
	cycleStart := cyclesignalsCovDay(t, cyclesignalsWindowCycleStart)

	logs := make([]models.DailyLog, 0, len(readings))
	for _, reading := range readings {
		entry := models.DailyLog{
			Date: AddCalendarDays(cycleStart, reading.cycleDay-1, time.UTC),
			BBT:  &reading.value,
		}
		if reading.disturbed {
			entry.CycleFactorKeys = []string{models.CycleFactorIllness}
		}
		logs = append(logs, entry)
	}
	return logs
}

// cyclesignalsWindowDetect runs the same two steps every owner surface runs, and
// returns the coverline the surfaces themselves never expose: a yes/no answer
// cannot tell two window definitions apart when both of them confirm the shift.
func cyclesignalsWindowDetect(t *testing.T, readings []cyclesignalsWindowReading) (int, float64, bool) {
	t.Helper()
	points := collectCycleBBTPoints(
		cyclesignalsWindowLogs(t, readings),
		cyclesignalsCovDay(t, cyclesignalsWindowCycleStart),
		cyclesignalsCovDay(t, cyclesignalsWindowNextStart),
		time.UTC,
	)
	return detectBBTShiftFirstHighDay(bbtSeriesFromPoints(points))
}

// TestBBTShiftWindowReachesBackPastAnUnrecordedDay characterises what the six
// are counted over: recorded values, not the six calendar days preceding the
// candidate. A skipped morning therefore lengthens the window backwards, and a
// warm reading old enough that a calendar window would have dropped it still
// sets the coverline. The two cases differ only in that oldest reading; both
// confirm the shift, so only the coverline separates the two readings of the
// rule.
func TestBBTShiftWindowReachesBackPastAnUnrecordedDay(t *testing.T) {
	cases := []struct {
		name          string
		oldestValue   float64
		wantCoverline float64
	}{
		{
			name:          "the sixth value back is the warm cycle day 5",
			oldestValue:   36.35,
			wantCoverline: 36.35,
		},
		{
			name:          "control: a flat cycle day 5 lowers the same coverline",
			oldestValue:   36.20,
			wantCoverline: 36.20,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			readings := append([]cyclesignalsWindowReading{
				{cycleDay: 5, value: testCase.oldestValue},
				{cycleDay: 6, value: 36.20},
				{cycleDay: 7, value: 36.20},
				{cycleDay: 8, value: 36.20},
				// Cycle day 9 is not recorded.
				{cycleDay: 10, value: 36.20},
				{cycleDay: 11, value: 36.20},
			}, cyclesignalsWindowShift...)

			firstHighDay, coverline, ok := cyclesignalsWindowDetect(t, readings)
			if !ok {
				t.Fatal("expected a confirmed shift across the unrecorded day")
			}
			if firstHighDay != 12 {
				t.Errorf("first elevated cycle day = %d, want 12", firstHighDay)
			}
			if coverline != testCase.wantCoverline {
				t.Errorf("coverline = %v, want %v", coverline, testCase.wantCoverline)
			}

			ovulation := inferBBTOvulationDate(
				cyclesignalsWindowLogs(t, readings),
				cyclesignalsCovDay(t, cyclesignalsWindowCycleStart),
				cyclesignalsCovDay(t, cyclesignalsWindowNextStart),
				time.UTC,
			)
			if got := ovulation.Format("2006-01-02"); got != "2026-01-11" {
				t.Errorf("ovulation estimate = %s, want 2026-01-11 (cycle day 11)", got)
			}
		})
	}
}

// TestBBTShiftWindowTakesSixRecordedValuesWhateverTheGapCount characterises how
// many gaps the window tolerates: any number, because the six are recordings.
// Zhu et al. (JMIR Mhealth Uhealth, 2021) instead score the window over calendar
// days and allow one missing day of six; a window holding only four of its six
// calendar days, as here, yields no detection under that reading. Which of the
// two is preferable is a product question — this pins which one ships.
func TestBBTShiftWindowTakesSixRecordedValuesWhateverTheGapCount(t *testing.T) {
	cases := []struct {
		name          string
		oldestValue   float64
		wantCoverline float64
	}{
		{
			name:          "two gaps pull the window back to cycle day 4",
			oldestValue:   36.35,
			wantCoverline: 36.35,
		},
		{
			name:          "control: a flat cycle day 4 lowers the same coverline",
			oldestValue:   36.20,
			wantCoverline: 36.20,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			readings := append([]cyclesignalsWindowReading{
				{cycleDay: 4, value: testCase.oldestValue},
				{cycleDay: 5, value: 36.20},
				// Cycle days 6 and 8 are not recorded.
				{cycleDay: 7, value: 36.20},
				{cycleDay: 9, value: 36.20},
				{cycleDay: 10, value: 36.20},
				{cycleDay: 11, value: 36.20},
			}, cyclesignalsWindowShift...)

			firstHighDay, coverline, ok := cyclesignalsWindowDetect(t, readings)
			if !ok {
				t.Fatal("expected a confirmed shift across two unrecorded days")
			}
			if firstHighDay != 12 {
				t.Errorf("first elevated cycle day = %d, want 12", firstHighDay)
			}
			if coverline != testCase.wantCoverline {
				t.Errorf("coverline = %v, want %v", coverline, testCase.wantCoverline)
			}
		})
	}
}

// TestBBTShiftDisturbedDayDoesNotConsumeAWindowSlot characterises the second
// half of the disturbance rule. Its first half — a warm disturbed reading does
// not inflate the coverline — is
// TestCycleSignals_InferBBTOvulationDate_IllnessDayExcludedFromCoverline, which
// reads the rule through the ovulation date. Here the excluded reading is
// ordinary, so nothing is inflated and the only observable difference is that
// the day is gone from the series entirely: the sixth value back moves one
// recorded day further, exactly as an unrecorded day makes it move. A tagged day
// is dropped whole, without asking whether its value stands out from the
// window — Sensiplan's Ausklammern removes at most an outlying value and keeps
// the day's place in the count.
func TestBBTShiftDisturbedDayDoesNotConsumeAWindowSlot(t *testing.T) {
	cases := []struct {
		name          string
		disturbed     bool
		wantCoverline float64
	}{
		{
			name:          "an illness-tagged cycle day 9 leaves the window six recordings deep",
			disturbed:     true,
			wantCoverline: 36.35,
		},
		{
			name:          "control: the same reading untagged fills the slot and drops the coverline",
			disturbed:     false,
			wantCoverline: 36.20,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			readings := append([]cyclesignalsWindowReading{
				{cycleDay: 5, value: 36.35},
				{cycleDay: 6, value: 36.20},
				{cycleDay: 7, value: 36.20},
				{cycleDay: 8, value: 36.20},
				{cycleDay: 9, value: 36.20, disturbed: testCase.disturbed},
				{cycleDay: 10, value: 36.20},
				{cycleDay: 11, value: 36.20},
			}, cyclesignalsWindowShift...)

			firstHighDay, coverline, ok := cyclesignalsWindowDetect(t, readings)
			if !ok {
				t.Fatal("expected a confirmed shift")
			}
			if firstHighDay != 12 {
				t.Errorf("first elevated cycle day = %d, want 12", firstHighDay)
			}
			if coverline != testCase.wantCoverline {
				t.Errorf("coverline = %v, want %v", coverline, testCase.wantCoverline)
			}
		})
	}
}

// TestBBTShiftSlowRiseIsNotConfirmed characterises the strictest departure from
// the source protocol: Sensiplan's first exception rule accepts a rise whose
// third day misses the 0.2 °C margin by waiting for a fourth day, which only has
// to stay above the coverline — it would confirm this cycle on the evening of
// cycle day 15. The product never does, and the reason is not the margin alone:
// the coverline SLIDES with the candidate, so the first elevated days of a slow
// rise enter the window of every later candidate and lift the line to their own
// level. A rise that climbs in steps smaller than the margin therefore raises
// the bar it is being measured against, and no candidate in it can qualify.
func TestBBTShiftSlowRiseIsNotConfirmed(t *testing.T) {
	readings := []cyclesignalsWindowReading{
		{cycleDay: 6, value: 36.20},
		{cycleDay: 7, value: 36.20},
		{cycleDay: 8, value: 36.20},
		{cycleDay: 9, value: 36.20},
		{cycleDay: 10, value: 36.20},
		{cycleDay: 11, value: 36.20},
		{cycleDay: 12, value: 36.30},
		{cycleDay: 13, value: 36.30},
		{cycleDay: 14, value: 36.30},
		{cycleDay: 15, value: 36.50},
	}

	if firstHighDay, coverline, ok := cyclesignalsWindowDetect(t, readings); ok {
		t.Fatalf("expected no shift for a stepwise rise, got first elevated day %d over coverline %v", firstHighDay, coverline)
	}

	ovulation := inferBBTOvulationDate(
		cyclesignalsWindowLogs(t, readings),
		cyclesignalsCovDay(t, cyclesignalsWindowCycleStart),
		cyclesignalsCovDay(t, cyclesignalsWindowNextStart),
		time.UTC,
	)
	if !ovulation.IsZero() {
		t.Fatalf("expected no ovulation estimate from a stepwise rise, got %s", ovulation.Format("2006-01-02"))
	}
}
