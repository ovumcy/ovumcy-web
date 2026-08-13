package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type CalendarDayState struct {
	Date       time.Time
	DateString string
	Day        int
	InMonth    bool
	IsToday    bool
	IsFuture   bool

	OpenEditDirectly bool
	IsPeriod         bool
	IsPredicted      bool
	// IsPredictedStartWindow marks a day the next period may START on — the
	// range the dashboard prints as "Next period: X — Y". It is a different
	// quantity from IsPredicted (a projected bleeding day), so it is carried
	// separately rather than folded into it.
	IsPredictedStartWindow bool
	IsPreFertile           bool
	IsFertility            bool
	IsFertilityPeak        bool
	IsFertilityEdge        bool
	IsOvulation            bool
	IsTentativeOvulation   bool
	HasData                bool
	HasSex                 bool
}

func CalendarLogRange(monthStart time.Time) (time.Time, time.Time) {
	monthEnd := monthStart.AddDate(0, 1, -1)
	return monthStart.AddDate(0, 0, -70), monthEnd.AddDate(0, 0, 70)
}

func BuildCalendarDayStates(user *models.User, monthStart time.Time, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) []CalendarDayState {
	weekStart := models.DefaultWeekStart
	if user != nil {
		weekStart = NormalizeWeekStart(user.WeekStartsOn)
	}
	gridStart, gridEnd := calendarGridBounds(monthStart, weekStart)
	latestLogByDate, hasDataMap := buildCalendarLogMaps(logs)
	predictionMaps := buildCalendarPredictionMaps(user, logs, stats, gridEnd, now, location)

	todayKey := DateAtLocation(now, location).Format("2006-01-02")

	days := make([]CalendarDayState, 0, 42)
	for day := gridStart; !day.After(gridEnd); day = day.AddDate(0, 0, 1) {
		days = append(days, buildCalendarDayState(day, monthStart, todayKey, latestLogByDate, hasDataMap, predictionMaps))
	}

	return days
}

func calendarGridBounds(monthStart time.Time, weekStart string) (time.Time, time.Time) {
	monthEnd := monthStart.AddDate(0, 1, -1)
	startOffset := weekStartOffset(monthStart.Weekday(), weekStart)
	endOffset := weekStartOffset(monthEnd.Weekday(), weekStart)
	gridStart := monthStart.AddDate(0, 0, -startOffset)
	gridEnd := monthEnd.AddDate(0, 0, 6-endOffset)
	return gridStart, gridEnd
}

func buildCalendarLogMaps(logs []models.DailyLog) (map[string]models.DailyLog, map[string]bool) {
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

// calendarPredictionMaps is the per-day lookup the grid paints from: one set
// per projected concept, keyed by "2006-01-02". Named fields rather than a
// tuple of same-typed maps — the set grew to seven when the predicted start
// window arrived, and a positional swap between two map[string]bool arguments
// compiles silently.
type calendarPredictionMaps struct {
	predictedPeriod     map[string]bool
	predictedStartRange map[string]bool
	preFertile          map[string]bool
	fertilityEdge       map[string]bool
	fertilityPeak       map[string]bool
	ovulation           map[string]bool
	tentativeOvulation  map[string]bool
}

func newCalendarPredictionMaps() calendarPredictionMaps {
	return calendarPredictionMaps{
		predictedPeriod:     make(map[string]bool),
		predictedStartRange: make(map[string]bool),
		preFertile:          make(map[string]bool),
		fertilityEdge:       make(map[string]bool),
		fertilityPeak:       make(map[string]bool),
		ovulation:           make(map[string]bool),
		tentativeOvulation:  make(map[string]bool),
	}
}

func buildCalendarPredictionMaps(user *models.User, logs []models.DailyLog, stats CycleStats, gridEnd time.Time, now time.Time, location *time.Location) calendarPredictionMaps {
	maps := newCalendarPredictionMaps()
	predictedPeriodMap := maps.predictedPeriod
	preFertileMap := maps.preFertile
	fertilityEdgeMap := maps.fertilityEdge
	fertilityPeakMap := maps.fertilityPeak
	ovulationMap := maps.ovulation
	tentativeOvulationMap := maps.tentativeOvulation

	// Medical-safety suppression gate, the same three signals every projected
	// surface gates on: unpredictable-cycle mode, a pregnancy pause, or a cycle
	// running past the account's reference length by more than a week
	// (DashboardCycleOverdue). Past that point stats.NextPeriodStart is a date the
	// account's own data no longer supports: appendPredictedCycles chains from it,
	// so the grid painted a predicted period in the PAST — one that never happened
	// — and then a phantom window every cycle length after it. Every prediction map
	// stays empty here; the recorded facts (logged period days, has-data, sex
	// activity) are read elsewhere and are untouched.
	if DashboardPredictionDisabled(user) || stats.PregnancyPaused || DashboardCycleOverdue(user, stats) {
		return maps
	}

	appendCurrentBaselinePeriod(predictedPeriodMap, stats, location)
	appendCurrentBaselinePreFertile(preFertileMap, stats, location)
	appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, stats.FertilityWindowStart, stats.FertilityWindowEnd, stats.OvulationDate)
	appendCalendarSingleDate(ovulationMap, stats.OvulationDate)
	appendPredictedCycles(predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, stats, gridEnd, location)
	appendPredictedStartRange(maps.predictedStartRange, user, stats, location)
	appendHistoricalCycles(preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, logs, stats, user, location)
	appendCurrentCycleBBTSignal(user, logs, stats, now, ovulationMap, tentativeOvulationMap, location)

	return maps
}

// appendPredictedStartRange marks the days the NEXT period may start on. That
// range is the quantity the dashboard prints as "Next period: X — Y", while the
// shaded predicted-period days are the projected bleeding itself: two different
// facts that shared one shading on the grid until wave 3.
//
// It reads DashboardPredictionRange rather than deriving a second range — one
// definition, two surfaces — so a day is marked only where the dashboard would
// already show a range (enough completed cycles for the spread to mean
// something), and never once the three suppression signals above have emptied
// the projected maps. Only the next cycle carries a window: the cycles chained
// after it are projections of a projection, and widening those would present
// manufactured spread as measured spread.
func appendPredictedStartRange(startRangeMap map[string]bool, user *models.User, stats CycleStats, location *time.Location) {
	rangeStart, rangeEnd, hasRange := DashboardPredictionRange(user, stats, CalendarDay(stats.NextPeriodStart, location), location)
	if !hasRange {
		return
	}
	appendCalendarDateRange(startRangeMap, rangeStart, rangeEnd)
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
		window := PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
		if !window.Calculable || window.FertilityWindowStart.IsZero() {
			return
		}
		fertilityStart = window.FertilityWindowStart
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
		offset := CalendarDaysBetween(day, ovulationDate)
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

// appendHistoricalCycles paints fertile-window, ovulation, and pre-fertile
// markers onto past completed cycles. A cycle is considered "completed" when a
// later cycle_start exists in the supplied logs; the most recent cycle_start
// has no successor and is therefore handled by the existing current-baseline /
// predicted-cycles paths instead. Gated on the user's ShowHistoricalPhases
// preference so that the upstream behavior (predictions only) remains the
// default for users who want it.
func appendHistoricalCycles(preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, logs []models.DailyLog, stats CycleStats, user *models.User, location *time.Location) {
	if user == nil || !user.ShowHistoricalPhases {
		return
	}

	starts := make([]time.Time, 0, len(logs))
	for _, log := range logs {
		if log.CycleStart {
			starts = append(starts, CalendarDay(log.Date, location))
		}
	}
	if len(starts) < 2 {
		return
	}

	luteal := ResolveLutealPhase(stats.LutealPhase)
	periodLength := predictedPeriodLength(stats.AveragePeriodLength)

	for index := range len(starts) - 1 {
		cycleStart := starts[index]
		nextStart := starts[index+1]
		cycleLen := CalendarDaysBetween(cycleStart, nextStart)
		if cycleLen <= 0 {
			continue
		}
		window := PredictCycleWindow(cycleStart, cycleLen, luteal)
		if !window.Calculable {
			continue
		}
		preFertileStart := cycleStart.AddDate(0, 0, periodLength)
		preFertileEnd := window.FertilityWindowStart.AddDate(0, 0, -1)
		appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
		ovulationMap[window.OvulationDate.Format("2006-01-02")] = true
		appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, window.FertilityWindowStart, window.FertilityWindowEnd, window.OvulationDate)
	}
}

func appendPredictedPeriod(predictedPeriodMap map[string]bool, cycleStart time.Time, predictedPeriodLength int) {
	for offset := range predictedPeriodLength {
		day := cycleStart.AddDate(0, 0, offset)
		predictedPeriodMap[day.Format("2006-01-02")] = true
	}
}

func appendPredictedWindow(preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, cycleStart time.Time, predictedCycleLength int, predictedPeriodLength int, lutealPhase int) {
	window := PredictCycleWindow(cycleStart, predictedCycleLength, ResolveLutealPhase(lutealPhase))
	if !window.Calculable {
		return
	}

	preFertileStart := cycleStart.AddDate(0, 0, predictedPeriodLength)
	preFertileEnd := window.FertilityWindowStart.AddDate(0, 0, -1)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
	ovulationMap[window.OvulationDate.Format("2006-01-02")] = true
	appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, window.FertilityWindowStart, window.FertilityWindowEnd, window.OvulationDate)
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

func buildCalendarDayState(day time.Time, monthStart time.Time, todayKey string, latestLogByDate map[string]models.DailyLog, hasDataMap map[string]bool, predictions calendarPredictionMaps) CalendarDayState {
	key := day.Format("2006-01-02")
	entry, hasEntry := latestLogByDate[key]
	isOvulation := predictions.ovulation[key]
	isTentativeOvulation := predictions.tentativeOvulation[key]
	isFertilityPeak := predictions.fertilityPeak[key]
	isFertilityEdge := predictions.fertilityEdge[key]
	openEditDirectly := !hasDataMap[key]

	return CalendarDayState{
		Date:                   day,
		DateString:             key,
		Day:                    day.Day(),
		InMonth:                day.Month() == monthStart.Month(),
		IsToday:                key == todayKey,
		IsFuture:               key > todayKey,
		OpenEditDirectly:       openEditDirectly,
		IsPeriod:               hasEntry && entry.IsPeriod,
		IsPredicted:            predictions.predictedPeriod[key],
		IsPredictedStartWindow: predictions.predictedStartRange[key],
		IsPreFertile:           predictions.preFertile[key],
		IsFertility:            (isFertilityEdge || isFertilityPeak) && !isOvulation && !isTentativeOvulation,
		IsFertilityPeak:        isFertilityPeak,
		IsFertilityEdge:        isFertilityEdge,
		IsOvulation:            isOvulation,
		IsTentativeOvulation:   isTentativeOvulation,
		HasData:                hasDataMap[key],
		HasSex:                 hasEntry && NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone,
	}
}
