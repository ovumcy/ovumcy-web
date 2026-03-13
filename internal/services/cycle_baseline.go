package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func ApplyUserCycleBaseline(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) CycleStats {
	if user == nil || user.Role != models.RoleOwner {
		return stats
	}
	if location == nil {
		location = time.UTC
	}

	latestExplicitCycleStart := findLatestExplicitCycleStart(logs, location)
	cycleLength, periodLength := resolveUserCycleLengths(user)
	hasObservedCycleLengths := len(CycleLengths(logs)) >= 1
	today := DateAtLocation(now.In(location), location)
	applyObservedBaseline(&stats, user, latestExplicitCycleStart, cycleLength, periodLength, hasObservedCycleLengths, today, location)
	applyProjectedBaseline(&stats, cycleLength, periodLength, location)

	stats.CurrentCycleDay = baselineCurrentCycleDay(stats.LastPeriodStart, today)
	stats.CurrentPhase = DetectCurrentPhase(stats, logs, today, location)
	return stats
}

func findLatestExplicitCycleStart(logs []models.DailyLog, location *time.Location) time.Time {
	explicitStarts := DetectExplicitCycleStarts(logs)
	if len(explicitStarts) == 0 {
		return time.Time{}
	}
	return DateAtLocation(explicitStarts[len(explicitStarts)-1], location)
}

func resolveUserCycleLengths(user *models.User) (int, int) {
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
	return cycleLength, periodLength
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
	userLastPeriodStart := time.Time{}
	if user != nil && user.LastPeriodStart != nil {
		userLastPeriodStart = DateAtLocation(*user.LastPeriodStart, location)
	}
	if !userLastPeriodStart.IsZero() && !today.IsZero() && userLastPeriodStart.After(today) {
		userLastPeriodStart = time.Time{}
	}
	if !latestExplicitCycleStart.IsZero() {
		latestExplicitCycleStart = DateAtLocation(latestExplicitCycleStart, location)
		if today.IsZero() || !latestExplicitCycleStart.After(manualCycleStartMaxDate(today, location)) {
			return latestExplicitCycleStart
		}
	}
	if !userLastPeriodStart.IsZero() {
		return userLastPeriodStart
	}
	return time.Time{}
}

func applyProjectedBaseline(stats *CycleStats, cycleLength int, periodLength int, location *time.Location) {
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

	stats.NextPeriodStart = DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, predictionCycleLength), location)
	projectedPeriodLength := int(stats.AveragePeriodLength + 0.5)
	if projectedPeriodLength <= 0 {
		projectedPeriodLength = periodLength
	}

	ovulationDate, fertilityWindowStart, fertilityWindowEnd, ovulationExact, ovulationCalculable := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		projectedPeriodLength,
	)
	if !ovulationCalculable {
		clearPredictedCycleWindow(stats)
		return
	}

	stats.OvulationDate = DateAtLocation(ovulationDate, location)
	stats.OvulationExact = ovulationExact
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = locationDateOrZero(fertilityWindowStart, location)
	stats.FertilityWindowEnd = locationDateOrZero(fertilityWindowEnd, location)
}

func locationDateOrZero(day time.Time, location *time.Location) time.Time {
	if day.IsZero() {
		return time.Time{}
	}
	return DateAtLocation(day, location)
}

func baselineCurrentCycleDay(lastPeriodStart time.Time, today time.Time) int {
	if lastPeriodStart.IsZero() || today.Before(lastPeriodStart) {
		return 0
	}
	return int(today.Sub(lastPeriodStart).Hours()/24) + 1
}

func DetectCurrentPhase(stats CycleStats, logs []models.DailyLog, today time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	periodByDate := make(map[string]bool, len(logs))
	for _, logEntry := range logs {
		if logEntry.IsPeriod {
			periodByDate[DateAtLocation(logEntry.Date, location).Format("2006-01-02")] = true
		}
	}
	if periodByDate[today.Format("2006-01-02")] {
		return "menstrual"
	}

	periodLength := int(stats.AveragePeriodLength + 0.5)
	if periodLength <= 0 {
		periodLength = models.DefaultPeriodLength
	}
	if !stats.LastPeriodStart.IsZero() {
		periodEnd := DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, periodLength-1), location)
		if betweenCalendarDaysInclusive(today, stats.LastPeriodStart, periodEnd) {
			return "menstrual"
		}
	}

	if stats.OvulationImpossible {
		return "unknown"
	}

	if !stats.OvulationDate.IsZero() {
		switch {
		case sameCalendarDay(today, stats.OvulationDate):
			return "ovulation"
		case betweenCalendarDaysInclusive(today, stats.FertilityWindowStart, stats.FertilityWindowEnd):
			return "fertile"
		case today.Before(stats.OvulationDate):
			return "follicular"
		default:
			return "luteal"
		}
	}

	return "unknown"
}

func ProjectCycleStart(lastPeriodStart time.Time, cycleLength int, today time.Time) (time.Time, int, bool) {
	if lastPeriodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}, 0, false
	}
	if today.Before(lastPeriodStart) {
		return lastPeriodStart, 0, true
	}

	elapsedDays := int(today.Sub(lastPeriodStart).Hours() / 24)
	cyclesElapsed := elapsedDays / cycleLength
	projectedStart := DateAtLocation(lastPeriodStart.AddDate(0, 0, cyclesElapsed*cycleLength), today.Location())
	projectedCycleDay := (elapsedDays % cycleLength) + 1
	return projectedStart, projectedCycleDay, true
}

func ShiftCycleStartToFutureOvulation(cycleStart time.Time, ovulationDate time.Time, cycleLength int, today time.Time) time.Time {
	if cycleLength <= 0 || !ovulationDate.Before(today) {
		return cycleStart
	}
	lagDays := int(today.Sub(ovulationDate).Hours() / 24)
	shiftCycles := lagDays/cycleLength + 1
	return DateAtLocation(cycleStart.AddDate(0, 0, shiftCycles*cycleLength), today.Location())
}

func sameCalendarDay(a time.Time, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func betweenCalendarDaysInclusive(day time.Time, start time.Time, end time.Time) bool {
	if start.IsZero() || end.IsZero() {
		return false
	}
	return (day.Equal(start) || day.After(start)) && (day.Equal(end) || day.Before(end))
}
