package services

import (
	"math"
	"sort"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type cycleBBTPoint struct {
	Date     time.Time
	CycleDay int
	Value    float64
}

const (
	// bbtShiftThresholdDelta is the sustained rise over baseline (°C) that marks
	// the post-ovulatory thermal shift.
	bbtShiftThresholdDelta = 0.2
	// bbtBaselineWindow is the number of leading recorded BBT values averaged into
	// the pre-shift baseline; the shift is only sought after this window.
	bbtBaselineWindow = 5
)

func InferUserLutealPhase(logs []models.DailyLog, location *time.Location) (int, bool) {
	if location == nil {
		location = time.UTC
	}

	starts := ObservedCycleStarts(logs)
	if len(starts) < 3 {
		return defaultLutealPhaseDays, false
	}

	lutealLengths := make([]int, 0, len(starts)-1)
	for index := 0; index+1 < len(starts); index++ {
		start := CalendarDay(starts[index], location)
		nextStart := CalendarDay(starts[index+1], location)
		ovulationDate := inferObservedOvulationDate(logs, start, nextStart, location)
		if ovulationDate.IsZero() {
			continue
		}

		lutealLength := CalendarDaysBetween(ovulationDate, nextStart)
		if lutealLength < minLutealPhaseDays || lutealLength > 20 {
			continue
		}
		lutealLengths = append(lutealLengths, lutealLength)
	}

	if len(lutealLengths) < 2 {
		return defaultLutealPhaseDays, false
	}
	return int(math.Round(averageInts(lutealLengths))), true
}

func inferObservedOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	bbtDate := inferBBTOvulationDate(logs, cycleStart, nextStart, location)
	if !bbtDate.IsZero() {
		return bbtDate
	}
	return inferEggWhiteOvulationDate(logs, cycleStart, nextStart, location)
}

func inferBBTOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	points := collectCycleBBTPoints(logs, cycleStart, nextStart, location)
	if len(points) < bbtBaselineWindow {
		return time.Time{}
	}

	var baselineTotal float64
	for index := range bbtBaselineWindow {
		baselineTotal += points[index].Value
	}
	baseline := baselineTotal / float64(bbtBaselineWindow)

	recordedDays := make([]int, len(points))
	dayValues := make(map[int]float64, len(points))
	for index, point := range points {
		recordedDays[index] = point.CycleDay
		dayValues[point.CycleDay] = point.Value
	}

	firstHighDay, ok := detectBBTShiftFirstHighDay(recordedDays, dayValues, baseline)
	if !ok {
		return time.Time{}
	}

	// Ovulation precedes the sustained thermal shift: basal temperature rises the
	// day after ovulation, so the estimate is the day before the first elevated
	// day (clamped to stay within the cycle).
	ovulationCycleDay := firstHighDay - 1
	// codecov:ignore:start -- defensive floor: firstHighDay is the >=6th recorded
	// cycle day (the detector skips the leading baseline window), so
	// ovulationCycleDay is always >= 5 and this clamp never fires in practice.
	if ovulationCycleDay < 1 {
		ovulationCycleDay = firstHighDay
	}
	// codecov:ignore:end
	return cycleStart.AddDate(0, 0, ovulationCycleDay-1)
}

// detectBBTShiftFirstHighDay finds the earliest sustained post-ovulatory thermal
// shift and returns the cycle-day number of its first elevated day.
//
// A shift is three consecutive calendar days (cycle days n, n+1, n+2) — all
// recorded and all at or above baseline+bbtShiftThresholdDelta — sought only
// after the leading baseline window. recordedDays must be sorted ascending;
// dayValues maps each recorded cycle day to its temperature.
//
// Callers derive ovulation from the returned day (conventionally the day before
// the shift begins). Both the luteal-phase inference and the BBT chart marker
// share this detector so they never disagree on where the shift is.
func detectBBTShiftFirstHighDay(recordedDays []int, dayValues map[int]float64, baseline float64) (int, bool) {
	threshold := baseline + bbtShiftThresholdDelta
	for index := bbtBaselineWindow; index+2 < len(recordedDays); index++ {
		dayOne := recordedDays[index]
		dayTwo := recordedDays[index+1]
		dayThree := recordedDays[index+2]
		if dayTwo != dayOne+1 || dayThree != dayTwo+1 {
			continue
		}
		if dayValues[dayOne] < threshold || dayValues[dayTwo] < threshold || dayValues[dayThree] < threshold {
			continue
		}
		return dayOne, true
	}
	return 0, false
}

func collectCycleBBTPoints(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) []cycleBBTPoint {
	points := make([]cycleBBTPoint, 0)
	for _, logEntry := range logs {
		if logEntry.BBT == nil || !IsValidDayBBT(logEntry.BBT) {
			continue
		}

		day := CalendarDay(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}

		points = append(points, cycleBBTPoint{
			Date:     day,
			CycleDay: CalendarDaysBetween(cycleStart, day) + 1,
			Value:    *logEntry.BBT,
		})
	}

	sort.Slice(points, func(i, j int) bool {
		return points[i].Date.Before(points[j].Date)
	})
	return points
}

func inferEggWhiteOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	lastEggWhite := time.Time{}
	for _, logEntry := range sortDailyLogs(logs) {
		day := CalendarDay(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}
		if NormalizeDayCervicalMucus(logEntry.CervicalMucus) != models.CervicalMucusEggWhite {
			continue
		}
		lastEggWhite = day
	}
	if lastEggWhite.IsZero() {
		return lastEggWhite
	}

	// Peak-day rule: the last day of fertile-quality (egg-white) mucus is the peak
	// fertility signal, and ovulation most commonly follows it by about a day.
	// Estimate ovulation as the day after the peak, clamped to stay before the
	// next cycle start (a peak on the final cycle day keeps the peak day itself).
	estimated := lastEggWhite.AddDate(0, 0, 1)
	if !estimated.Before(nextStart) {
		return lastEggWhite
	}
	return estimated
}
