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
	latestExplicitCycleStart := latestExplicitCycleStartBeforeOrOn(logs, today, location)
	cycleLength, periodLength, lutealPhase := resolveUserCycleLengths(user)
	if inferredLutealPhase, ok := InferUserLutealPhase(logs, location); ok {
		lutealPhase = inferredLutealPhase
	}
	hasObservedCycleLengths := len(CycleLengths(logs)) >= 1
	applyObservedBaseline(&stats, user, latestExplicitCycleStart, cycleLength, periodLength, hasObservedCycleLengths, today, location)
	applyProjectedBaseline(&stats, cycleLength, lutealPhase, location)

	stats.CurrentCycleDay = baselineCurrentCycleDay(stats.LastPeriodStart, today)
	stats.CurrentPhase = DetectCurrentPhase(stats, logs, today, location)
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

func applyObservedBaseline(stats *CycleStats, user *models.User, latestExplicitCycleStart time.Time, cycleLength int, periodLength int, hasObservedCycleLengths bool, today time.Time, location *time.Location) {
	if !hasObservedCycleLengths {
		if cycleLength > 0 {
			stats.AverageCycleLength = float64(cycleLength)
			stats.MedianCycleLength = cycleLength
		}
		if periodLength > 0 {
			stats.AveragePeriodLength = float64(periodLength)
		}
		stats.LastPeriodStart = baselineLastPeriodStart(user, latestExplicitCycleStart, today, location)
		return
	}

	stats.LastPeriodStart = baselineLastPeriodStart(user, latestExplicitCycleStart, today, location)
}

func baselineLastPeriodStart(user *models.User, latestExplicitCycleStart time.Time, today time.Time, location *time.Location) time.Time {
	return latestCycleStartAnchorBeforeOrOn(user, latestExplicitCycleStart, today, location)
}

func applyProjectedBaseline(stats *CycleStats, cycleLength int, lutealPhase int, location *time.Location) {
	if stats.LastPeriodStart.IsZero() {
		return
	}

	predictionCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	if predictionCycleLength <= 0 {
		predictionCycleLength = cycleLength
	}
	if predictionCycleLength <= 0 {
		return
	}

	stats.NextPeriodStart = CalendarDay(stats.LastPeriodStart.AddDate(0, 0, predictionCycleLength), location)
	stats.LutealPhase = ResolveLutealPhase(lutealPhase)

	window := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		stats.LutealPhase,
	)
	if !window.Calculable {
		clearPredictedCycleWindow(stats)
		return
	}

	stats.OvulationDate = CalendarDay(window.OvulationDate, location)
	stats.OvulationExact = window.OvulationExact
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = locationDateOrZero(window.FertilityWindowStart, location)
	stats.FertilityWindowEnd = locationDateOrZero(window.FertilityWindowEnd, location)
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
	projectedStart := CalendarDay(lastPeriodStart.AddDate(0, 0, cyclesElapsed*cycleLength), today.Location())
	projectedCycleDay := (elapsedDays % cycleLength) + 1
	return projectedStart, projectedCycleDay, true
}

func ShiftCycleStartToFutureOvulation(cycleStart time.Time, ovulationDate time.Time, cycleLength int, today time.Time) time.Time {
	if cycleLength <= 0 || !ovulationDate.Before(today) {
		return cycleStart
	}
	lagDays := CalendarDaysBetween(ovulationDate, today)
	shiftCycles := lagDays/cycleLength + 1
	return CalendarDay(cycleStart.AddDate(0, 0, shiftCycles*cycleLength), today.Location())
}

func sameCalendarDay(a time.Time, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}
