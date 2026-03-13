package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type DashboardCycleContext struct {
	CycleDayReference           int
	CycleDayWarning             bool
	CycleDataStale              bool
	DisplayNextPeriodStart      time.Time
	DisplayNextPeriodRangeStart time.Time
	DisplayNextPeriodRangeEnd   time.Time
	DisplayNextPeriodUseRange   bool
	DisplayNextPeriodDelayed    bool
	DisplayOvulationDate        time.Time
	DisplayOvulationExact       bool
	DisplayOvulationImpossible  bool
	NextPeriodInPast            bool
	OvulationInPast             bool
}

func DashboardCycleReferenceLength(user *models.User, stats CycleStats) int {
	if stats.AverageCycleLength > 0 {
		return int(stats.AverageCycleLength + 0.5)
	}
	if stats.MedianCycleLength > 0 {
		return stats.MedianCycleLength
	}
	if user != nil && IsValidOnboardingCycleLength(user.CycleLength) {
		return user.CycleLength
	}
	return models.DefaultCycleLength
}

func DashboardCycleDayLooksLong(currentDay int, referenceLength int) bool {
	if currentDay <= 0 || referenceLength <= 0 {
		return false
	}
	return currentDay > referenceLength+7
}

func DashboardCycleDataLooksStale(lastPeriodStart time.Time, today time.Time, referenceLength int) bool {
	if lastPeriodStart.IsZero() || referenceLength <= 0 || today.Before(lastPeriodStart) {
		return false
	}
	rawCycleDay := int(today.Sub(lastPeriodStart).Hours()/24) + 1
	return rawCycleDay > referenceLength
}

func DashboardCycleStaleAnchor(user *models.User, stats CycleStats, location *time.Location) time.Time {
	if !stats.LastPeriodStart.IsZero() {
		return DateAtLocation(stats.LastPeriodStart, location)
	}
	if user == nil || user.LastPeriodStart == nil || user.LastPeriodStart.IsZero() {
		return time.Time{}
	}
	return DateAtLocation(*user.LastPeriodStart, location)
}

func DashboardPredictionRange(user *models.User, stats CycleStats, location *time.Location) (time.Time, time.Time, bool) {
	if stats.LastPeriodStart.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	minCycleLength := stats.MinCycleLength
	maxCycleLength := stats.MaxCycleLength
	if minCycleLength <= 0 || maxCycleLength <= 0 || maxCycleLength < minCycleLength {
		referenceLength := DashboardCycleReferenceLength(user, stats)
		if referenceLength <= 0 {
			return time.Time{}, time.Time{}, false
		}
		minCycleLength = referenceLength - irregularCycleFallbackSpan
		if minCycleLength < 15 {
			minCycleLength = 15
		}
		maxCycleLength = referenceLength + irregularCycleFallbackSpan
		if maxCycleLength < minCycleLength {
			maxCycleLength = minCycleLength
		}
	}

	return DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, minCycleLength), location),
		DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, maxCycleLength), location),
		true
}

func DashboardShouldHideExactPrediction(currentCycleDay int, referenceLength int) bool {
	if currentCycleDay <= 0 || referenceLength <= 0 {
		return false
	}
	return currentCycleDay > referenceLength+7
}

func DashboardUpcomingPredictions(stats CycleStats, user *models.User, today time.Time, cycleLength int) (time.Time, time.Time, bool, bool) {
	nextPeriodStart := stats.NextPeriodStart
	ovulationDate := stats.OvulationDate
	ovulationExact := stats.OvulationExact
	ovulationImpossible := stats.OvulationImpossible

	if stats.LastPeriodStart.IsZero() || cycleLength <= 0 {
		return nextPeriodStart, ovulationDate, ovulationExact, ovulationImpossible
	}

	cycleStart, _, projectionOK := ProjectCycleStart(stats.LastPeriodStart, cycleLength, today)
	if !projectionOK {
		return nextPeriodStart, ovulationDate, ovulationExact, ovulationImpossible
	}

	nextPeriodStart = DateAtLocation(cycleStart.AddDate(0, 0, cycleLength), today.Location())
	predictedPeriodLength := DashboardPredictedPeriodLength(user, stats)
	ovulationDate, _, _, ovulationExact, ovulationCalculable := PredictCycleWindow(
		cycleStart,
		cycleLength,
		predictedPeriodLength,
	)
	if ovulationCalculable && ovulationDate.Before(today) {
		cycleStart = ShiftCycleStartToFutureOvulation(cycleStart, ovulationDate, cycleLength, today)
		nextPeriodStart = DateAtLocation(cycleStart.AddDate(0, 0, cycleLength), today.Location())
		ovulationDate, _, _, ovulationExact, ovulationCalculable = PredictCycleWindow(
			cycleStart,
			cycleLength,
			predictedPeriodLength,
		)
	}
	if !ovulationCalculable {
		return nextPeriodStart, time.Time{}, false, true
	}
	return nextPeriodStart, ovulationDate, ovulationExact, false
}

func BuildDashboardCycleContext(user *models.User, stats CycleStats, today time.Time, location *time.Location) DashboardCycleContext {
	cycleDayReference := DashboardCycleReferenceLength(user, stats)
	cycleDayWarning := DashboardCycleDayLooksLong(stats.CurrentCycleDay, cycleDayReference)
	cycleStaleAnchor := DashboardCycleStaleAnchor(user, stats, location)
	cycleDataStale := DashboardCycleDataLooksStale(cycleStaleAnchor, today, cycleDayReference)
	displayNextPeriodStart, displayOvulationDate, displayOvulationExact, displayOvulationImpossible := DashboardUpcomingPredictions(
		stats,
		user,
		today,
		cycleDayReference,
	)

	displayNextPeriodRangeStart := time.Time{}
	displayNextPeriodRangeEnd := time.Time{}
	displayNextPeriodUseRange := false
	displayNextPeriodDelayed := false

	if user != nil && user.IrregularCycle {
		displayNextPeriodRangeStart, displayNextPeriodRangeEnd, displayNextPeriodUseRange = DashboardPredictionRange(user, stats, location)
	}

	if !displayNextPeriodUseRange && DashboardShouldHideExactPrediction(stats.CurrentCycleDay, cycleDayReference) {
		displayNextPeriodStart = time.Time{}
		displayNextPeriodDelayed = true
	}

	return DashboardCycleContext{
		CycleDayReference:           cycleDayReference,
		CycleDayWarning:             cycleDayWarning,
		CycleDataStale:              cycleDataStale,
		DisplayNextPeriodStart:      displayNextPeriodStart,
		DisplayNextPeriodRangeStart: displayNextPeriodRangeStart,
		DisplayNextPeriodRangeEnd:   displayNextPeriodRangeEnd,
		DisplayNextPeriodUseRange:   displayNextPeriodUseRange,
		DisplayNextPeriodDelayed:    displayNextPeriodDelayed,
		DisplayOvulationDate:        displayOvulationDate,
		DisplayOvulationExact:       displayOvulationExact,
		DisplayOvulationImpossible:  displayOvulationImpossible,
		NextPeriodInPast:            !displayNextPeriodUseRange && !displayNextPeriodStart.IsZero() && displayNextPeriodStart.Before(today),
		OvulationInPast:             !displayOvulationImpossible && !displayOvulationDate.IsZero() && displayOvulationDate.Before(today),
	}
}

func DashboardPredictedPeriodLength(user *models.User, stats CycleStats) int {
	if user != nil && IsValidOnboardingPeriodLength(user.PeriodLength) {
		return user.PeriodLength
	}
	predictedPeriodLength := int(stats.AveragePeriodLength + 0.5)
	if predictedPeriodLength > 0 {
		return predictedPeriodLength
	}
	return models.DefaultPeriodLength
}

func CompletedCycleTrendLengths(logs []models.DailyLog, now time.Time, location *time.Location) []int {
	starts := DetectCycleStarts(logs)
	if len(starts) < 2 {
		return nil
	}

	today := DateAtLocation(now, location)
	lengths := make([]int, 0, len(starts)-1)
	for index := 1; index < len(starts); index++ {
		previousStart := DateAtLocation(starts[index-1], location)
		currentStart := DateAtLocation(starts[index], location)
		if !currentStart.Before(today) {
			break
		}
		lengths = append(lengths, int(currentStart.Sub(previousStart).Hours()/24))
	}
	return lengths
}
