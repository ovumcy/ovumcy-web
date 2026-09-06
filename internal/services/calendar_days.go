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
	// The projection bound keeps the request-local shape it has always had:
	// appendPredictedCycles compares it against a CalendarDay value built in
	// the same location, so both operands stay start-of-day in one zone.
	predictionMaps := buildCalendarPredictionMaps(user, logs, stats, CalendarDay(gridEnd, location), now, location)

	todayKey := DateAtLocation(now, location).Format("2006-01-02")

	// The grid is a run of CALENDAR days, so it is counted and stepped as
	// calendar days rather than by adding 24h-ish increments to an instant and
	// testing that instant against a bound. CalendarDaysBetween compares only
	// the calendar components of the two bounds, and the step runs over
	// UTC-anchored days, where no midnight is ever skipped.
	gridDayCount := CalendarDaysBetween(gridStart, gridEnd) + 1
	days := make([]CalendarDayState, 0, gridDayCount)
	for offset := range gridDayCount {
		day := gridStart.AddDate(0, 0, offset)
		days = append(days, buildCalendarDayState(day, monthStart, todayKey, latestLogByDate, hasDataMap, predictionMaps))
	}

	return days
}

// calendarGridBounds returns the first and last calendar day of the month grid,
// both inclusive and both anchored at UTC midnight — the package's canonical
// shape for a date-only value.
//
// The arithmetic deliberately leaves the request timezone: in a UTC-minus zone
// whose DST jump lands on midnight (America/Santiago 2026-09-06,
// America/Havana 2026-03-08) local midnight does not exist on that date, and
// AddDate resolves the missing wall clock BACKWARD into the previous calendar
// day. A bound built that way names the wrong day, and a grid stepped from it
// emits that day twice and drops the last day of the range. UTC has no
// transitions, so the same arithmetic there is exact for every zone.
func calendarGridBounds(monthStart time.Time, weekStart string) (time.Time, time.Time) {
	firstOfMonth := CalendarDay(monthStart, time.UTC)
	monthEnd := firstOfMonth.AddDate(0, 1, -1)
	startOffset := weekStartOffset(firstOfMonth.Weekday(), weekStart)
	endOffset := weekStartOffset(monthEnd.Weekday(), weekStart)
	gridStart := firstOfMonth.AddDate(0, 0, -startOffset)
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

	// Medical-safety suppression gate, the shared predicate every projected
	// surface gates on (PredictionsSuppressed): unpredictable-cycle mode, a
	// pregnancy pause, or a cycle running past the account's reference length by
	// more than a week (DashboardCycleOverdue). Past that point
	// stats.NextPeriodStart is a date the account's own data no longer supports:
	// appendPredictedCycles chains from it, so the grid painted a predicted period
	// in the PAST — one that never happened — and then a phantom window every
	// cycle length after it. Every prediction map stays empty here; the recorded
	// facts (logged period days, has-data, sex activity) are read elsewhere and
	// are untouched.
	if PredictionsSuppressed(user, stats) {
		return maps
	}

	// The fertility half of the projection carries the extra first-cycle floor:
	// until one cycle has been observed, the fertile window, the peak band and the
	// ovulation day are the onboarding slider projected forward, so the grid
	// paints none of them (FertilityProjectionSuppressed). The predicted period
	// days keep their anchor in a recorded cycle start and stay.
	fertilitySuppressed := FertilityProjectionSuppressed(user, stats)

	// The CURRENT cycle's window follows a thermal shift the owner's own
	// temperatures confirm — the same triple (day, window, status) the dashboard
	// and the JSON API publish, resolved once so the grid cannot shade the
	// projected window under a solid marker the BBT pass below has already moved
	// onto the confirmed day. The projected cycles chained after it (line
	// 379/404) keep the model's arithmetic: a confirmation is about this cycle
	// only. The resolver reads the same gate as the branch it sits in, so it
	// changes which days are shaded and never whether any are.
	currentStats, _ := ResolveConfirmedCycleStats(user, logs, stats, DateAtLocation(now, location), location)

	appendCurrentBaselinePeriod(predictedPeriodMap, stats, location)
	if !fertilitySuppressed {
		appendCurrentBaselinePreFertile(preFertileMap, currentStats, location)
		appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, currentStats.FertilityWindowStart, currentStats.FertilityWindowEnd, currentStats.OvulationDate)
		appendCalendarSingleDate(ovulationMap, currentStats.OvulationDate)
	}
	appendPredictedCycles(predictedPeriodMap, preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, stats, gridEnd, location, !fertilitySuppressed)
	appendPredictedStartRange(maps.predictedStartRange, user, stats, location)
	appendHistoricalCycles(preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, logs, stats, user, location)
	if !fertilitySuppressed {
		// The BBT pass only ever downgrades the projected ovulation day to
		// "tentative", so with the fertility maps withheld it would reintroduce
		// the very day the floor just removed, one shade lighter.
		appendCurrentCycleBBTSignal(user, logs, stats, now, ovulationMap, tentativeOvulationMap, location)
	}

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
	preFertileStart := AddCalendarDays(cycleStart, periodLength, location)

	fertilityStart := CalendarDay(stats.FertilityWindowStart, location)
	if fertilityStart.IsZero() {
		cycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
		window := PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
		if !window.Calculable || window.FertilityWindowStart.IsZero() {
			return
		}
		fertilityStart = window.FertilityWindowStart
	}

	preFertileEnd := AddCalendarDays(fertilityStart, -1, location)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
}

// forEachCalendarDay visits `count` consecutive calendar days starting on the
// calendar date of `start`, ascending, each day exactly once. It is the single
// stepping point for the builders that fill the prediction maps.
//
// The walk is calendar-day arithmetic, never instant arithmetic. Adding 24h-ish
// increments to a location-midnight instant breaks in a zone whose DST jump
// lands on midnight (America/Santiago 2026-09-06, America/Havana 2026-03-08):
// the missing wall clock resolves BACKWARD into the previous calendar day, so
// the step writes that day's key a second time and the range loses a day — the
// transition day itself when the caller indexes by offset, the range's last day
// when it walks to a bound. Re-anchoring to UTC midnight — the same move
// CalendarDaysBetween makes on its operands — removes the transition from the
// walk entirely. The visited days carry calendar components only; every caller
// reads them through CalendarDayKey or CalendarDaysBetween, so the anchoring
// zone is not observable.
func forEachCalendarDay(start time.Time, count int, visit func(day time.Time)) {
	if start.IsZero() || count <= 0 {
		return
	}
	day := CalendarDay(start, time.UTC)
	for range count {
		visit(day)
		day = day.AddDate(0, 0, 1)
	}
}

// calendarRangeLength is the number of calendar days in the inclusive range
// [start, end], or zero when the range is empty. The comparison is by calendar
// day, not by instant: the two bounds routinely carry different midnight shapes
// (a request-location midnight against the UTC midnight PredictCycleWindow
// produces), and compared as instants in a UTC-minus zone the location-midnight
// bound sits hours past the UTC-midnight one on the same date.
func calendarRangeLength(start time.Time, end time.Time) int {
	if start.IsZero() || end.IsZero() {
		return 0
	}
	span := CalendarDaysBetween(start, end)
	if span < 0 {
		return 0
	}
	return span + 1
}

func appendCalendarDateRange(target map[string]bool, start time.Time, end time.Time) {
	forEachCalendarDay(start, calendarRangeLength(start, end), func(day time.Time) {
		target[CalendarDayKey(day)] = true
	})
}

func appendCalendarSingleDate(target map[string]bool, day time.Time) {
	if !day.IsZero() {
		target[day.Format("2006-01-02")] = true
	}
}

func appendFertilityWindow(fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, start time.Time, end time.Time, ovulationDate time.Time) {
	forEachCalendarDay(start, calendarRangeLength(start, end), func(day time.Time) {
		offset := CalendarDaysBetween(day, ovulationDate)
		if offset >= 0 && offset <= 2 {
			fertilityPeakMap[CalendarDayKey(day)] = true
			return
		}
		fertilityEdgeMap[CalendarDayKey(day)] = true
	})
}

// appendPredictedCycles chains the projected cycles across the visible grid.
// includeFertility is false in the first-cycle tier: the chained period days
// still descend from a recorded anchor, while the window inside each of them
// would be the onboarding slider projected forward one cycle at a time.
func appendPredictedCycles(predictedPeriodMap map[string]bool, preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, stats CycleStats, gridEnd time.Time, location *time.Location, includeFertility bool) {
	if stats.NextPeriodStart.IsZero() {
		return
	}

	predictedCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	if predictedCycleLength <= 0 {
		// The loop below advances the cursor by exactly this value, so a
		// non-positive step never moves it, never falsifies the bound, and
		// fills the prediction maps until the process dies. Zero is a real
		// return of predictedCycleLength (an average under 0.5 rounds to it),
		// and its other stepping caller, applyProjectedBaseline, guards it too
		// — the termination of this loop is this function's business, not the
		// callee's.
		//
		// codecov:ignore -- defensive: unreachable from production stats today.
		// Both writers of NextPeriodStart derive median and average from the
		// same observed lengths, so either both are zero (and the default
		// applies) or both describe at least one real cycle.
		// Regression: TestAppendPredictedCyclesTerminatesOnANonPositiveStep.
		return
	}
	predictedPeriodLength := predictedPeriodLength(stats.AveragePeriodLength)
	// The bound is a CALENDAR-DAY comparison, not an instant one: cycleStart
	// comes from CalendarDay and gridEnd from plain AddDate arithmetic, and in
	// a zone whose DST jump skips midnight the two carry different start-of-day
	// shapes (01:00 local against 00:00). Compared as instants, a projected
	// cycle falling exactly on the last grid day reads as past the grid and its
	// markers are never painted.
	for cycleStart := CalendarDay(stats.NextPeriodStart, location); CalendarDaysBetween(cycleStart, gridEnd) >= 0; cycleStart = AddCalendarDays(cycleStart, predictedCycleLength, location) {
		appendPredictedPeriod(predictedPeriodMap, cycleStart, predictedPeriodLength)
		if includeFertility {
			appendPredictedWindow(preFertileMap, fertilityEdgeMap, fertilityPeakMap, ovulationMap, cycleStart, predictedCycleLength, predictedPeriodLength, stats.LutealPhase, location)
		}
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
		preFertileStart := AddCalendarDays(cycleStart, periodLength, location)
		preFertileEnd := window.FertilityWindowStart.AddDate(0, 0, -1)
		appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
		ovulationMap[window.OvulationDate.Format("2006-01-02")] = true
		appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, window.FertilityWindowStart, window.FertilityWindowEnd, window.OvulationDate)
	}
}

func appendPredictedPeriod(predictedPeriodMap map[string]bool, cycleStart time.Time, predictedPeriodLength int) {
	forEachCalendarDay(cycleStart, predictedPeriodLength, func(day time.Time) {
		predictedPeriodMap[CalendarDayKey(day)] = true
	})
}

func appendPredictedWindow(preFertileMap map[string]bool, fertilityEdgeMap map[string]bool, fertilityPeakMap map[string]bool, ovulationMap map[string]bool, cycleStart time.Time, predictedCycleLength int, predictedPeriodLength int, lutealPhase int, location *time.Location) {
	window := PredictCycleWindow(cycleStart, predictedCycleLength, ResolveLutealPhase(lutealPhase))
	if !window.Calculable {
		return
	}

	// cycleStart arrives from appendPredictedCycles as a REQUEST-ZONE midnight,
	// so this step needs the same treatment as its siblings; location is
	// threaded in for it. window.FertilityWindowStart on the next line is a
	// PredictCycleWindow output and therefore already UTC-anchored, where the
	// step is exact — the two lines look asymmetric because their operands are.
	preFertileStart := AddCalendarDays(cycleStart, predictedPeriodLength, location)
	preFertileEnd := window.FertilityWindowStart.AddDate(0, 0, -1)
	appendCalendarDateRange(preFertileMap, preFertileStart, preFertileEnd)
	ovulationMap[window.OvulationDate.Format("2006-01-02")] = true
	appendFertilityWindow(fertilityEdgeMap, fertilityPeakMap, window.FertilityWindowStart, window.FertilityWindowEnd, window.OvulationDate)
}

func appendCurrentCycleBBTSignal(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, ovulationMap map[string]bool, tentativeOvulationMap map[string]bool, location *time.Location) {
	// The PROJECTED ovulation date is no longer a precondition, only the subject
	// of the downgrade below: a projection the model withheld (a median cycle too
	// short to place an ovulation, so no ovulation date and no next period start)
	// used to silence the grid about a shift the owner's own temperatures had
	// already confirmed. The window derived from a confirmed day needs no
	// projection, so the pass now runs on the recorded anchor alone and simply
	// has nothing to downgrade when there is no projected day.
	if user == nil || !user.TrackBBT || stats.LastPeriodStart.IsZero() {
		return
	}

	cycleStart := CalendarDay(stats.LastPeriodStart, location)
	today := DateAtLocation(now, location)
	if today.Before(cycleStart) {
		return
	}

	ovulationSignal, confirmed := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location)
	if !stats.OvulationDate.IsZero() {
		projectedKey := CalendarDayKey(stats.OvulationDate)
		delete(ovulationMap, projectedKey)
		if !confirmed {
			tentativeOvulationMap[projectedKey] = true
		}
	}
	if !confirmed {
		return
	}

	// A detected shift CONFIRMS an ovulation that has already happened; it never
	// predicts one. So the solid marker belongs on the day the detector named,
	// not on the projection that day just superseded. Leaving it on
	// stats.OvulationDate is how the grid and the stats chart came to name two
	// different days for one shift: probableOvulationMarkerIndex marks the same
	// firstHighDay-1 day this inference returns, so the chart moved and only the
	// grid stayed behind on the model's date.
	ovulationMap[CalendarDayKey(ovulationSignal)] = true
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
