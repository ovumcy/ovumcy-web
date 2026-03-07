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

	latestLoggedPeriodStart := findLatestLoggedPeriodStart(logs, location)
	cycleLength, periodLength := resolveUserCycleLengths(user)
	reliableCycleData := len(CycleLengths(logs)) >= 2
	applyObservedBaseline(&stats, user, latestLoggedPeriodStart, cycleLength, periodLength, reliableCycleData, location)
	applyProjectedBaseline(&stats, cycleLength, periodLength, reliableCycleData, location)

	today := DateAtLocation(now.In(location), location)
	stats.CurrentCycleDay = baselineCurrentCycleDay(stats.LastPeriodStart, cycleLength, today)
	stats.CurrentPhase = DetectCurrentPhase(stats, logs, today, location)
	return stats
}

func findLatestLoggedPeriodStart(logs []models.DailyLog, location *time.Location) time.Time {
	detectedStarts := DetectCycleStarts(logs)
	if len(detectedStarts) == 0 {
		return time.Time{}
	}
	return DateAtLocation(detectedStarts[len(detectedStarts)-1], location)
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

func applyObservedBaseline(stats *CycleStats, user *models.User, latestLoggedPeriodStart time.Time, cycleLength int, periodLength int, reliableCycleData bool, location *time.Location) {
	if !reliableCycleData {
		if cycleLength > 0 {
			stats.AverageCycleLength = float64(cycleLength)
			stats.MedianCycleLength = cycleLength
		}
		if periodLength > 0 {
			stats.AveragePeriodLength = float64(periodLength)
		}
		stats.LastPeriodStart = baselineLastPeriodStart(user, latestLoggedPeriodStart, location)
		return
	}

	if !latestLoggedPeriodStart.IsZero() {
		stats.LastPeriodStart = latestLoggedPeriodStart
	}
}

func baselineLastPeriodStart(user *models.User, latestLoggedPeriodStart time.Time, location *time.Location) time.Time {
	switch {
	case !latestLoggedPeriodStart.IsZero():
		return latestLoggedPeriodStart
	case user.LastPeriodStart != nil:
		return DateAtLocation(*user.LastPeriodStart, location)
	default:
		return time.Time{}
	}
}

func applyProjectedBaseline(stats *CycleStats, cycleLength int, periodLength int, reliableCycleData bool, location *time.Location) {
	if stats.LastPeriodStart.IsZero() || cycleLength <= 0 || (reliableCycleData && !stats.NextPeriodStart.IsZero()) {
		return
	}

	stats.NextPeriodStart = DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, cycleLength), location)
	projectedPeriodLength := int(stats.AveragePeriodLength + 0.5)
	if projectedPeriodLength <= 0 {
		projectedPeriodLength = periodLength
	}

	ovulationDate, fertilityWindowStart, fertilityWindowEnd, ovulationExact, ovulationCalculable := PredictCycleWindow(
		stats.LastPeriodStart,
		cycleLength,
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

func baselineCurrentCycleDay(lastPeriodStart time.Time, cycleLength int, today time.Time) int {
	if _, projectedCycleDay, ok := ProjectCycleStart(lastPeriodStart, cycleLength, today); ok {
		return projectedCycleDay
	}
	if !lastPeriodStart.IsZero() && !today.Before(lastPeriodStart) {
		return int(today.Sub(lastPeriodStart).Hours()/24) + 1
	}
	return 0
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
