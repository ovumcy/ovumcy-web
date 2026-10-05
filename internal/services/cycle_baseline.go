package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func ApplyUserCycleBaseline(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) CycleStats {
	if user == nil || user.Role != models.RoleOwner {
		return stats
	}
	if location == nil {
		location = time.UTC
	}

	today := DateAtLocation(now.In(location), location)
	boundaryCtx := BoundaryContextFor(user, today)
	boundaries := CycleBoundaries(filterLogsNotAfter(logs, today), boundaryCtx)
	cycleLength, periodLength, lutealPhase := resolveUserCycleLengths(user)
	inferredLutealPhase, inferred := InferUserLutealPhase(logs, location, boundaryCtx)
	if inferred {
		lutealPhase = inferredLutealPhase
	}
	hasObservedCycleLengths := len(cycleLengths(boundaries)) >= 1
	anchor := time.Time{}
	if latest := latestBoundaryOnOrBefore(boundaries, today); !latest.IsZero() {
		anchor = CalendarDay(latest, location)
	}
	applyObservedBaseline(&stats, anchor, cycleLength, periodLength, hasObservedCycleLengths)
	projected := applyProjectedBaseline(&stats, user, cycleLength, lutealPhase, location)
	// The inference can succeed (it reads the same boundaries the history
	// statistics do) while the baseline finds no anchor to project from; then
	// stats.LutealPhase still holds BuildCycleStats's value, not the inferred one.
	stats.LutealPhasePersonalised = inferred && projected

	stats.CurrentCycleDay = baselineCurrentCycleDay(stats.LastPeriodStart, today)
	stats.CurrentPhase = DetectCurrentPhase(stats, logs, today, location)
	setFertilityStatus(&stats, today, FertilityBasisProjection)
	return stats
}

func resolveUserCycleLengths(user *models.User) (int, int, int) {
	cycleLength := 0
	if IsValidOnboardingCycleLength(user.CycleLength) {
		cycleLength = user.CycleLength
	}

	periodLength := 0
	if IsValidOnboardingPeriodLength(user.PeriodLength) {
		periodLength = user.PeriodLength
	}
	if periodLength <= 0 {
		periodLength = models.DefaultPeriodLength
	}

	return cycleLength, periodLength, ResolveLutealPhase(user.LutealPhase)
}

// applyObservedBaseline writes the anchor CycleBoundaries produced into the
// stats, and the owner's configured lengths while no cycle has been observed.
func applyObservedBaseline(stats *CycleStats, anchor time.Time, cycleLength int, periodLength int, hasObservedCycleLengths bool) {
	if !hasObservedCycleLengths {
		if cycleLength > 0 {
			stats.AverageCycleLength = float64(cycleLength)
			stats.MedianCycleLength = cycleLength
		}
		if periodLength > 0 {
			stats.AveragePeriodLength = float64(periodLength)
		}
	}
	stats.LastPeriodStart = anchor
}

// applyProjectedBaseline reports whether it wrote lutealPhase into stats: false
// on the two early returns, where stats.LutealPhase is left untouched.
func applyProjectedBaseline(stats *CycleStats, user *models.User, cycleLength int, lutealPhase int, location *time.Location) bool {
	if stats.LastPeriodStart.IsZero() {
		return false
	}

	predictionCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	if predictionCycleLength <= 0 {
		predictionCycleLength = cycleLength
	}
	if predictionCycleLength <= 0 {
		return false
	}

	stats.NextPeriodStart = projectedDay(AddCalendarDays(stats.LastPeriodStart, predictionCycleLength, location))
	stats.LutealPhase = ResolveLutealPhase(lutealPhase)

	window := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		stats.LutealPhase,
	)
	if !window.Calculable {
		clearPredictedCycleWindow(stats)
		return true
	}
	if projectedDay(window.OvulationDate).IsZero() {
		clearUnspellableCycleWindow(stats)
		return true
	}

	fertilityStart, fertilityEnd := window.FertilityWindowStart, window.FertilityWindowEnd
	if dashboardIrregularPredictionRangeEnabled(user, *stats) {
		fertilityStart, fertilityEnd = irregularFertilityWindow(window, stats.LastPeriodStart, stats.MinCycleLength, stats.MaxCycleLength, stats.LutealPhase)
		// The widened window's last day is the latest ovulation, not the median
		// one, so it is that day which must still be spellable; past 9999-12-31
		// the window goes as a whole, exactly as an unspellable median one does.
		if projectedDay(fertilityEnd).IsZero() {
			clearUnspellableCycleWindow(stats)
			return true
		}
	}

	stats.OvulationDate = CalendarDay(window.OvulationDate, location)
	stats.OvulationExact = window.OvulationExact
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = locationDateOrZero(fertilityStart, location)
	stats.FertilityWindowEnd = locationDateOrZero(fertilityEnd, location)
	return true
}

// irregularFertilityWindow is the current cycle's fertile window for an account
// in the irregular mode the dashboard shows a next-period and ovulation RANGE
// for (dashboardIrregularPredictionRangeEnabled). The median window alone is
// six days placed by one cycle length, while the same account is shown an
// ovulation range spanning its shortest to its longest recent cycle; a status
// read against the six days called the rest of that range "outside the window"
// on the very days the dashboard named as possible ovulation days.
//
// The window runs from the shortest cycle's window start to the longest
// cycle's ovulation day: [ovulation(min) - 5, ovulation(max)]. Both ends come
// from PredictCycleWindow itself, so the start keeps its clamp to the recorded
// cycle start and the ovulation arithmetic is the projection's own. A shortest
// cycle too short for any ovulation to be placed in it starts the window at
// that same clamp, the cycle start: the earliest start any shorter cycle could
// reach. The median ovulation day is left alone — this widens the window a
// status is read against, not the day the projection names.
func irregularFertilityWindow(median CycleWindowPrediction, periodStart time.Time, minCycleLength int, maxCycleLength int, lutealPhase int) (time.Time, time.Time) {
	start := dateOnly(periodStart)
	if earliest := PredictCycleWindow(periodStart, minCycleLength, lutealPhase); earliest.Calculable {
		start = earliest.FertilityWindowStart
	}
	end := median.FertilityWindowEnd
	if latest := PredictCycleWindow(periodStart, maxCycleLength, lutealPhase); latest.Calculable {
		end = latest.OvulationDate
	}
	return start, end
}

func locationDateOrZero(day time.Time, location *time.Location) time.Time {
	if day.IsZero() {
		return time.Time{}
	}
	return CalendarDay(day, location)
}

func baselineCurrentCycleDay(lastPeriodStart time.Time, today time.Time) int {
	if lastPeriodStart.IsZero() {
		return 0
	}
	// Both arguments may carry request-location wall clocks (DateAtLocation /
	// CalendarDay per issue #48), so subtracting them as instants is offset-
	// and DST-sensitive. cycleDayAt counts a pure calendar-day difference via
	// CalendarDaysBetween, immune to both.
	return cycleDayAt(lastPeriodStart, today)
}

func DetectCurrentPhase(stats CycleStats, logs []models.DailyLog, today time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	return resolveCyclePhase(stats, logs, today, cyclePhaseOptions{location: location, includeProjectedPeriod: true})
}

func ProjectCycleStart(lastPeriodStart time.Time, cycleLength int, today time.Time) (time.Time, int, bool) {
	if lastPeriodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}, 0, false
	}
	if today.Before(lastPeriodStart) {
		return lastPeriodStart, 0, true
	}

	elapsedDays := CalendarDaysBetween(lastPeriodStart, today)
	cyclesElapsed := elapsedDays / cycleLength
	projectedStart := AddCalendarDays(lastPeriodStart, cyclesElapsed*cycleLength, today.Location())
	projectedCycleDay := (elapsedDays % cycleLength) + 1
	return projectedStart, projectedCycleDay, true
}

// RunningCycleNextPeriodStart is the projected start of the period that closes
// the cycle running from lastPeriodStart, re-anchored in today's location. It is
// never rolled forward a cycle (unlike ProjectCycleStart): a period expected
// yesterday is late, not a month away. Zero when no cycle can be projected.
func RunningCycleNextPeriodStart(lastPeriodStart time.Time, cycleLength int, today time.Time) time.Time {
	if lastPeriodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}
	}
	return AddCalendarDays(lastPeriodStart, cycleLength, today.Location())
}

// ShiftCycleStartToFutureOvulation rolls the cycle anchor forward whole cycles
// until the predicted ovulation is no longer in the past. The guard counts
// calendar days, matching the lag arithmetic below it: ovulationDate arrives as
// a UTC-midnight date-only value and today as a location-midnight working
// value, so comparing them as instants fired the shift on the ovulation day
// itself in every non-UTC zone (issue #48 class).
func ShiftCycleStartToFutureOvulation(cycleStart time.Time, ovulationDate time.Time, cycleLength int, today time.Time) time.Time {
	if cycleLength <= 0 || CalendarDaysBetween(ovulationDate, today) <= 0 {
		return cycleStart
	}
	lagDays := CalendarDaysBetween(ovulationDate, today)
	shiftCycles := lagDays/cycleLength + 1
	return AddCalendarDays(cycleStart, shiftCycles*cycleLength, today.Location())
}

func sameCalendarDay(a time.Time, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}
