package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type CalendarDayState struct {
	Date         time.Time
	DateString   string
	Day          int
	InMonth      bool
	IsToday      bool
	IsPeriod     bool
	IsPredicted  bool
	IsPreFertile bool
	IsFertility  bool
	IsOvulation  bool
	HasData      bool
	HasSex       bool
}

func CalendarLogRange(monthStart time.Time) (time.Time, time.Time) {
	monthEnd := monthStart.AddDate(0, 1, -1)
	return monthStart.AddDate(0, 0, -70), monthEnd.AddDate(0, 0, 70)
}

func BuildCalendarDayStates(monthStart time.Time, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) []CalendarDayState {
	gridStart, gridEnd := calendarGridBounds(monthStart)
	latestLogByDate, hasDataMap := buildCalendarLogMaps(logs, location)
	predictedPeriodMap, preFertileMap, fertilityMap, ovulationMap := buildCalendarPredictionMaps(stats, gridEnd, location)

	todayKey := DateAtLocation(now, location).Format("2006-01-02")

	days := make([]CalendarDayState, 0, 42)
	for day := gridStart; !day.After(gridEnd); day = day.AddDate(0, 0, 1) {
		days = append(days, buildCalendarDayState(day, monthStart, todayKey, latestLogByDate, hasDataMap, predictedPeriodMap, preFertileMap, fertilityMap, ovulationMap))
	}

	return days
}

func calendarGridBounds(monthStart time.Time) (time.Time, time.Time) {
	monthEnd := monthStart.AddDate(0, 1, -1)
	gridStart := monthStart.AddDate(0, 0, -int(monthStart.Weekday()))
	gridEnd := monthEnd.AddDate(0, 0, 6-int(monthEnd.Weekday()))
	return gridStart, gridEnd
}

func buildCalendarLogMaps(logs []models.DailyLog, location *time.Location) (map[string]models.DailyLog, map[string]bool) {
	latestLogByDate := make(map[string]models.DailyLog)
	hasDataMap := make(map[string]bool)
	for _, logEntry := range logs {
		key := DateAtLocation(logEntry.Date, location).Format("2006-01-02")
		existing, exists := latestLogByDate[key]
		if !exists || logEntry.Date.After(existing.Date) || (logEntry.Date.Equal(existing.Date) && logEntry.ID > existing.ID) {
			latestLogByDate[key] = logEntry
		}
		hasDataMap[key] = hasDataMap[key] || DayHasData(logEntry)
	}
	return latestLogByDate, hasDataMap
}

func buildCalendarPredictionMaps(stats CycleStats, gridEnd time.Time, location *time.Location) (map[string]bool, map[string]bool, map[string]bool, map[string]bool) {
	predictedPeriodMap := make(map[string]bool)
	preFertileMap := make(map[string]bool)
	fertilityMap := make(map[string]bool)
	ovulationMap := make(map[string]bool)

	appendCurrentBaselinePeriod(predictedPeriodMap, stats, location)
	appendCurrentBaselinePreFertile(preFertileMap, stats, location)
	appendCalendarDateRange(fertilityMap, stats.FertilityWindowStart, stats.FertilityWindowEnd)
	appendCalendarSingleDate(ovulationMap, stats.OvulationDate)
	appendPredictedCycles(predictedPeriodMap, preFertileMap, fertilityMap, ovulationMap, stats, gridEnd, location)

	return predictedPeriodMap, preFertileMap, fertilityMap, ovulationMap
}

func appendCurrentBaselinePeriod(predictedPeriodMap map[string]bool, stats CycleStats, location *time.Location) {
	if stats.LastPeriodStart.IsZero() {
		return
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	appendPredictedPeriod(predictedPeriodMap, DateAtLocation(stats.LastPeriodStart, location), periodLength)
}

func appendCurrentBaselinePreFertile(preFertileMap map[string]bool, stats CycleStats, location *time.Location) {
	if stats.LastPeriodStart.IsZero() {
		return
	}

	cycleStart := DateAtLocation(stats.LastPeriodStart, location)
	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	preFertileStart := cycleStart.AddDate(0, 0, periodLength)

		fertilityStart := DateAtLocation(stats.FertilityWindowStart, location)
	if fertilityStart.IsZero() {
		cycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
		_, computedFertilityStart, _, _, calculable := PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
		if !calculable || computedFertilityStart.IsZero() {
			return
		}
		fertilityStart = computedFertilityStart
	}

	preFertileEnd := fertilityStart.AddDate(0, 0, -1)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
}

func appendCalendarDateRange(target map[string]bool, start time.Time, end time.Time) {
	if start.IsZero() || end.IsZero() {
		return
	}
	if end.Before(start) {
		return
	}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		target[day.Format("2006-01-02")] = true
	}
}

func appendCalendarSingleDate(target map[string]bool, day time.Time) {
	if !day.IsZero() {
		target[day.Format("2006-01-02")] = true
	}
}

func appendPredictedCycles(predictedPeriodMap map[string]bool, preFertileMap map[string]bool, fertilityMap map[string]bool, ovulationMap map[string]bool, stats CycleStats, gridEnd time.Time, location *time.Location) {
	if stats.NextPeriodStart.IsZero() {
		return
	}

	predictedCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	predictedPeriodLength := predictedPeriodLength(stats.AveragePeriodLength)
	for cycleStart := DateAtLocation(stats.NextPeriodStart, location); !cycleStart.After(gridEnd); cycleStart = cycleStart.AddDate(0, 0, predictedCycleLength) {
		appendPredictedPeriod(predictedPeriodMap, cycleStart, predictedPeriodLength)
		appendPredictedWindow(preFertileMap, fertilityMap, ovulationMap, cycleStart, predictedCycleLength, predictedPeriodLength, stats.LutealPhase)
	}
}

func appendPredictedPeriod(predictedPeriodMap map[string]bool, cycleStart time.Time, predictedPeriodLength int) {
	for offset := 0; offset < predictedPeriodLength; offset++ {
		day := cycleStart.AddDate(0, 0, offset)
		predictedPeriodMap[day.Format("2006-01-02")] = true
	}
}

func appendPredictedWindow(preFertileMap map[string]bool, fertilityMap map[string]bool, ovulationMap map[string]bool, cycleStart time.Time, predictedCycleLength int, predictedPeriodLength int, lutealPhase int) {
	ovulationDate, fertilityStart, fertilityEnd, _, calculable := PredictCycleWindow(cycleStart, predictedCycleLength, ResolveLutealPhase(lutealPhase))
	if !calculable {
		return
	}

	preFertileStart := cycleStart.AddDate(0, 0, predictedPeriodLength)
	preFertileEnd := fertilityStart.AddDate(0, 0, -1)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
	ovulationMap[ovulationDate.Format("2006-01-02")] = true
	appendCalendarDateRange(fertilityMap, fertilityStart, fertilityEnd)
}

func buildCalendarDayState(day time.Time, monthStart time.Time, todayKey string, latestLogByDate map[string]models.DailyLog, hasDataMap map[string]bool, predictedPeriodMap map[string]bool, preFertileMap map[string]bool, fertilityMap map[string]bool, ovulationMap map[string]bool) CalendarDayState {
	key := day.Format("2006-01-02")
	entry, hasEntry := latestLogByDate[key]
	isOvulation := ovulationMap[key]

	return CalendarDayState{
		Date:         day,
		DateString:   key,
		Day:          day.Day(),
		InMonth:      day.Month() == monthStart.Month(),
		IsToday:      key == todayKey,
		IsPeriod:     hasEntry && entry.IsPeriod,
		IsPredicted:  predictedPeriodMap[key],
		IsPreFertile: preFertileMap[key],
		IsFertility:  fertilityMap[key] && !isOvulation,
		IsOvulation:  isOvulation,
		HasData:      hasDataMap[key],
		HasSex:       hasEntry && NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone,
	}
}
