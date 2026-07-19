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

// "3-over-6" coverline rule (#249): the sliding coverline is the maximum of the
// bbtCoverlineWindow immediately preceding undisturbed recorded temperatures;
// a shift is bbtElevatedStreakDays calendar-consecutive days strictly above the
// coverline, with the third day at least bbtThirdDayMarginCelsius above it.
// Ovulation is estimated as the calendar day before the first elevated day.
const (
	bbtCoverlineWindow       = 6
	bbtElevatedStreakDays    = 3
	bbtThirdDayMarginCelsius = 0.2
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
	recordedDays, dayValues := bbtSeriesFromPoints(collectCycleBBTPoints(logs, cycleStart, nextStart, location))
	firstHighDay, _, ok := detectBBTShiftFirstHighDay(recordedDays, dayValues)
	if !ok {
		return time.Time{}
	}

	// Ovulation precedes the sustained thermal shift: basal temperature rises the
	// day after ovulation, so the estimate is the day before the first elevated
	// day (clamped to stay within the cycle).
	ovulationCycleDay := firstHighDay - 1
	// codecov:ignore:start -- defensive floor: firstHighDay is at least the 7th
	// recorded cycle day (the detector requires a full 6-value coverline window
	// before it), so ovulationCycleDay is always >= 6 and this clamp never fires
	// in practice.
	if ovulationCycleDay < 1 {
		ovulationCycleDay = firstHighDay
	}
	// codecov:ignore:end
	return cycleStart.AddDate(0, 0, ovulationCycleDay-1)
}

// detectBBTShiftFirstHighDay is the one shared "3-over-6" detector: luteal
// inference, the calendar tentative-ovulation signal, and the stats chart
// marker/coverline must all route through it so they never disagree.
//
// The sliding coverline for a candidate first elevated day is the MAX of the 6
// immediately preceding recorded temperatures (max, not mean, so ordinary
// follicular noise cannot slip past). A shift is three consecutive calendar
// days (cycle days n, n+1, n+2), all recorded, the first two strictly above
// the coverline and the third at least bbtThirdDayMarginCelsius above it.
// recordedDays must be sorted ascending; dayValues maps each recorded cycle
// day to its temperature. Returns the first elevated cycle day and the
// coverline in effect.
func detectBBTShiftFirstHighDay(recordedDays []int, dayValues map[int]float64) (int, float64, bool) {
	for index := bbtCoverlineWindow; index+bbtElevatedStreakDays-1 < len(recordedDays); index++ {
		dayOne := recordedDays[index]
		dayTwo := recordedDays[index+1]
		dayThree := recordedDays[index+2]
		if dayTwo != dayOne+1 || dayThree != dayTwo+1 {
			continue
		}

		coverline := dayValues[recordedDays[index-bbtCoverlineWindow]]
		for windowIndex := index - bbtCoverlineWindow + 1; windowIndex < index; windowIndex++ {
			if value := dayValues[recordedDays[windowIndex]]; value > coverline {
				coverline = value
			}
		}

		if dayValues[dayOne] <= coverline || dayValues[dayTwo] <= coverline {
			continue
		}
		if dayValues[dayThree] < coverline+bbtThirdDayMarginCelsius {
			continue
		}
		return dayOne, coverline, true
	}
	return 0, 0, false
}

// bbtSeriesFromPoints converts ordered points into the recordedDays/dayValues
// pair the shared detector consumes.
func bbtSeriesFromPoints(points []cycleBBTPoint) ([]int, map[int]float64) {
	recordedDays := make([]int, len(points))
	dayValues := make(map[int]float64, len(points))
	for index, point := range points {
		recordedDays[index] = point.CycleDay
		dayValues[point.CycleDay] = point.Value
	}
	return recordedDays, dayValues
}

// collectCycleBBTPoints builds the detection series: one undisturbed reading
// per calendar day (the latest same-day reading wins, matching the chart
// series). Days tagged illness or sleep_disruption are excluded entirely — a
// fever must neither inflate the coverline nor confirm an elevated streak.
func collectCycleBBTPoints(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) []cycleBBTPoint {
	pointByDay := make(map[int]cycleBBTPoint)
	for _, logEntry := range sortDailyLogs(logs) {
		if logEntry.BBT == nil || !IsValidDayBBT(logEntry.BBT) || isBBTDisturbedLog(logEntry) {
			continue
		}

		day := CalendarDay(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}

		cycleDay := CalendarDaysBetween(cycleStart, day) + 1
		pointByDay[cycleDay] = cycleBBTPoint{
			Date:     day,
			CycleDay: cycleDay,
			Value:    *logEntry.BBT,
		}
	}

	points := make([]cycleBBTPoint, 0, len(pointByDay))
	for _, point := range pointByDay {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].CycleDay < points[j].CycleDay
	})
	return points
}

// isBBTDisturbedLog reports whether the day carries a factor that distorts
// basal temperature independently of ovulation (#249 disturbance rejection).
func isBBTDisturbedLog(logEntry models.DailyLog) bool {
	for _, factorKey := range logEntry.CycleFactorKeys {
		if factorKey == models.CycleFactorIllness || factorKey == models.CycleFactorSleepDisruption {
			return true
		}
	}
	return false
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
