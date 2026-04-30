package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type CalendarDayState struct {
	Date                 time.Time
	DateString           string
	Day                  int
	InMonth              bool
	IsToday              bool
	OpenEditDirectly     bool
	IsPeriod             bool
	IsPredicted          bool
	IsPreFertile         bool
	IsFertility          bool
	IsFertilityPeak      bool
	IsFertilityEdge      bool
	IsOvulation          bool
	IsTentativeOvulation bool
	HasData              bool
	HasSex               bool
}

func CalendarLogRange(monthStart time.Time) (time.Time, time.Time) {
	monthEnd := monthStart.AddDate(0, 1, -1)
	return monthStart.AddDate(0, 0, -70), monthEnd.AddDate(0, 0, 70)
}

func BuildCalendarDayStates(user *models.User, monthStart time.Time, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) []CalendarDayState {
	gridStart, gridEnd := calendarGridBounds(monthStart)
	latestLogByDate, hasDataMap := buildCalendarLogMaps(logs, location)
	predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, tentativeOvulationMap := buildCalendarPredictionMaps(user, logs, stats, gridEnd, now, location)

	todayKey := DateAtLocation(now, location).Format("2006-01-02")

	days := make([]CalendarDayState, 0, 42)
	for day := gridStart; !day.After(gridEnd); day = day.AddDate(0, 0, 1) {
		days = append(days, buildCalendarDayState(day, monthStart, todayKey, latestLogByDate, hasDataMap, predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, tentativeOvulationMap))
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
		key := CalendarDayKey(logEntry.Date)
		existing, exists := latestLogByDate[key]
		if !exists || logEntry.Date.After(existing.Date) || (logEntry.Date.Equal(existing.Date) && logEntry.ID > existing.ID) {
			latestLogByDate[key] = logEntry
		}
		hasDataMap[key] = hasDataMap[key] || DayHasData(logEntry)
	}
	return latestLogByDate, hasDataMap
}

func buildCalendarPredictionMaps(user *models.User, logs []models.DailyLog, stats CycleStats, gridEnd time.Time, now time.Time, location *time.Location) (map[string]bool, map[string]bool, map[string]bool, map[string]bool, map[string]bool, map[string]bool) {
	predictedPeriodMap := make(map[string]bool)
	preFertileMap := make(map[string]bool)
	fertilityEdgeMap := make(map[string]bool)
	fertilityPeakMap := make(map[string]bool)
	ovulationMap := make(map[string]bool)
	tentativeOvulationMap := make(map[string]bool)

	if DashboardPredictionDisabled(user) {
		return predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, tentativeOvulationMap
	}

	appendCurrentBaselinePeriod(predictedPeriodMap, stats, location)
	appendCurrentBaselinePreFertile(preFertileMap, stats, location)
	appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, stats.FertilityWindowStart, stats.FertilityWindowEnd, stats.OvulationDate)
	appendCalendarSingleDate(ovulationMap, stats.OvulationDate)
	appendPredictedCycles(predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, stats, gridEnd, location)
	appendCurrentCycleBBTSignal(user, logs, stats, now, ovulationMap, tentativeOvulationMap, location)

	return predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, tentativeOvulationMap
}

func appendCurrentBaselinePeriod(predictedPeriodMap map[string]bool, stats CycleStats, location *time.Location) {
	if stats.LastPeriodStart.IsZero() {
		return
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	appendPredictedPeriod(predictedPeriodMap, CalendarDay(stats.LastPeriodStart, location), periodLength)
}

func appendCurrentBaselinePreFertile(preFertileMap map[string]bool, stats CycleStats, location *time.Location) {
	if stats.LastPeriodStart.IsZero() {
		return
	}

	cycleStart := CalendarDay(stats.LastPeriodStart, location)
	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	preFertileStart := cycleStart.AddDate(0, 0, periodLength)

	fertilityStart := CalendarDay(stats.FertilityWindowStart, location)
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

func appendFertilityWindow(fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, start time.Time, end time.Time, ovulationDate time.Time) {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return
	}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		offset := int(ovulationDate.Sub(day).Hours() / 24)
		if offset >= 0 && offset <= 2 {
			fertilityPeakMap[day.Format("2006-01-02")] = true
			continue
		}
		fertilityEdgeMap[day.Format("2006-01-02")] = true
	}
}

func appendPredictedCycles(predictedPeriodMap map[string]bool, preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, stats CycleStats, gridEnd time.Time, location *time.Location) {
	if stats.NextPeriodStart.IsZero() {
		return
	}

	predictedCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	predictedPeriodLength := predictedPeriodLength(stats.AveragePeriodLength)
	for cycleStart := CalendarDay(stats.NextPeriodStart, location); !cycleStart.After(gridEnd); cycleStart = cycleStart.AddDate(0, 0, predictedCycleLength) {
		appendPredictedPeriod(predictedPeriodMap, cycleStart, predictedPeriodLength)
		appendPredictedWindow(preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, cycleStart, predictedCycleLength, predictedPeriodLength, stats.LutealPhase)
	}
}

func appendPredictedPeriod(predictedPeriodMap map[string]bool, cycleStart time.Time, predictedPeriodLength int) {
	for offset := 0; offset < predictedPeriodLength; offset++ {
		day := cycleStart.AddDate(0, 0, offset)
		predictedPeriodMap[day.Format("2006-01-02")] = true
	}
}

func appendPredictedWindow(preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, cycleStart time.Time, predictedCycleLength int, predictedPeriodLength int, lutealPhase int) {
	ovulationDate, fertilityStart, fertilityEnd, _, calculable := PredictCycleWindow(cycleStart, predictedCycleLength, ResolveLutealPhase(lutealPhase))
	if !calculable {
		return
	}

	preFertileStart := cycleStart.AddDate(0, 0, predictedPeriodLength)
	preFertileEnd := fertilityStart.AddDate(0, 0, -1)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
	ovulationMap[ovulationDate.Format("2006-01-02")] = true
	appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, fertilityStart, fertilityEnd, ovulationDate)
}

func appendCurrentCycleBBTSignal(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, ovulationMap map[string]bool, tentativeOvulationMap map[string]bool, location *time.Location) {
	if user == nil || !user.TrackBBT || stats.LastPeriodStart.IsZero() || stats.OvulationDate.IsZero() || stats.NextPeriodStart.IsZero() {
		return
	}

	cycleStart := CalendarDay(stats.LastPeriodStart, location)
	today := DateAtLocation(now, location)
	if today.Before(cycleStart) {
		return
	}

	ovulationSignal := inferBBTOvulationDate(filterLogsNotAfter(logs, today), cycleStart, CalendarDay(stats.NextPeriodStart, location), location)
	if !ovulationSignal.IsZero() {
		return
	}

	key := CalendarDayKey(stats.OvulationDate)
	delete(ovulationMap, key)
	tentativeOvulationMap[key] = true
}

func buildCalendarDayState(day time.Time, monthStart time.Time, todayKey string, latestLogByDate map[string]models.DailyLog, hasDataMap map[string]bool, predictedPeriodMap map[string]bool, preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, tentativeOvulationMap map[string]bool) CalendarDayState {
	key := day.Format("2006-01-02")
	entry, hasEntry := latestLogByDate[key]
	isOvulation := ovulationMap[key]
	isTentativeOvulation := tentativeOvulationMap[key]
	isFertilityPeak := fertilityPeakMap[key]
	isFertilityEdge := fertilityEdgeMap[key]
	openEditDirectly := !hasDataMap[key]

	return CalendarDayState{
		Date:                 day,
		DateString:           key,
		Day:                  day.Day(),
		InMonth:              day.Month() == monthStart.Month(),
		IsToday:              key == todayKey,
		OpenEditDirectly:     openEditDirectly,
		IsPeriod:             hasEntry && entry.IsPeriod,
		IsPredicted:          predictedPeriodMap[key],
		IsPreFertile:         preFertileMap[key],
		IsFertility:          (isFertilityEdge || isFertilityPeak) && !isOvulation && !isTentativeOvulation,
		IsFertilityPeak:      isFertilityPeak,
		IsFertilityEdge:      isFertilityEdge,
		IsOvulation:          isOvulation,
		IsTentativeOvulation: isTentativeOvulation,
		HasData:              hasDataMap[key],
		HasSex:               hasEntry && NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone,
	}
}
