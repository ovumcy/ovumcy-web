package services

import (
	"math"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type DashboardCycleContext struct {
	CycleDayReference           int
	CycleDayWarning             bool
	CycleDataStale              bool
	PredictionDisabled          bool
	DisplayNextPeriodStart      time.Time
	DisplayNextPeriodRangeStart time.Time
	DisplayNextPeriodRangeEnd   time.Time
	DisplayNextPeriodUseRange   bool
	DisplayNextPeriodPrompt     bool
	DisplayNextPeriodNeedsData  bool
	DisplayOvulationDate        time.Time
	DisplayOvulationRangeStart  time.Time
	DisplayOvulationRangeEnd    time.Time
	DisplayOvulationUseRange    bool
	DisplayOvulationNeedsData   bool
	DisplayOvulationExact       bool
	DisplayOvulationImpossible  bool
	NextPeriodInPast            bool
	OvulationInPast             bool
}

func DashboardPredictionDisabled(user *models.User) bool {
	return user != nil && user.UnpredictableCycle
}

func dashboardPredictionExtraSpanDays(user *models.User) int {
	if user == nil {
		return 0
	}
	if NormalizeAgeGroup(user.AgeGroup) == models.AgeGroup35Plus {
		return 1
	}
	return 0
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

func predictionRangeSpanDays(stats CycleStats) int {
	switch {
	case stats.CompletedCycleCount < 3:
		return 4
	case stats.CompletedCycleCount < 6:
		return 3
	default:
		span := int(math.Round(stats.CycleLengthStdDev))
		if span < 1 {
			span = 1
		}
		return span
	}
}

func dashboardIrregularPredictionRangeEnabled(user *models.User, stats CycleStats) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount >= 3 && stats.MinCycleLength > 0 && stats.MaxCycleLength >= stats.MinCycleLength
}

func DashboardPredictionRange(user *models.User, stats CycleStats, predictedStart time.Time, location *time.Location) (time.Time, time.Time, bool) {
	if predictedStart.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	if dashboardIrregularPredictionRangeEnabled(user, stats) {
		return DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, stats.MinCycleLength), location),
			DateAtLocation(stats.LastPeriodStart.AddDate(0, 0, stats.MaxCycleLength), location),
			true
	}

	spanDays := predictionRangeSpanDays(stats)
	spanDays += dashboardPredictionExtraSpanDays(user)
	return DateAtLocation(predictedStart.AddDate(0, 0, -spanDays), location),
		DateAtLocation(predictedStart.AddDate(0, 0, spanDays), location),
		true
}

func DashboardOvulationRange(nextPeriodRangeStart time.Time, nextPeriodRangeEnd time.Time, lutealPhase int, location *time.Location) (time.Time, time.Time, bool) {
	if nextPeriodRangeStart.IsZero() || nextPeriodRangeEnd.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	resolvedLutealPhase := ResolveLutealPhase(lutealPhase)
	rangeStart := DateAtLocation(nextPeriodRangeStart.AddDate(0, 0, -resolvedLutealPhase), location)
	rangeEnd := DateAtLocation(nextPeriodRangeEnd.AddDate(0, 0, -resolvedLutealPhase), location)
	if rangeEnd.Before(rangeStart) {
		return time.Time{}, time.Time{}, false
	}

	return rangeStart, rangeEnd, true
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
	ovulationDate, _, _, ovulationExact, ovulationCalculable := PredictCycleWindow(
		cycleStart,
		cycleLength,
		stats.LutealPhase,
	)
	if ovulationCalculable && ovulationDate.Before(today) {
		cycleStart = ShiftCycleStartToFutureOvulation(cycleStart, ovulationDate, cycleLength, today)
		nextPeriodStart = DateAtLocation(cycleStart.AddDate(0, 0, cycleLength), today.Location())
		ovulationDate, _, _, ovulationExact, ovulationCalculable = PredictCycleWindow(
			cycleStart,
			cycleLength,
			stats.LutealPhase,
		)
	}
	if !ovulationCalculable {
		return nextPeriodStart, time.Time{}, false, true
	}
	return nextPeriodStart, ovulationDate, ovulationExact, false
}

func BuildDashboardCycleContext(user *models.User, stats CycleStats, today time.Time, location *time.Location) DashboardCycleContext {
	if DashboardPredictionDisabled(user) {
		return DashboardCycleContext{
			CycleDayReference:  DashboardCycleReferenceLength(user, stats),
			CycleDayWarning:    false,
			CycleDataStale:     false,
			PredictionDisabled: true,
		}
	}

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
	displayNextPeriodPrompt := stats.LastPeriodStart.IsZero()
	displayNextPeriodNeedsData := user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !displayNextPeriodStart.IsZero()
	displayOvulationRangeStart := time.Time{}
	displayOvulationRangeEnd := time.Time{}
	displayOvulationUseRange := false
	displayOvulationNeedsData := user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !stats.LastPeriodStart.IsZero()
	if !displayNextPeriodPrompt && !displayNextPeriodNeedsData {
		displayNextPeriodRangeStart, displayNextPeriodRangeEnd, displayNextPeriodUseRange = DashboardPredictionRange(user, stats, displayNextPeriodStart, location)
		if dashboardIrregularPredictionRangeEnabled(user, stats) {
			displayOvulationRangeStart, displayOvulationRangeEnd, displayOvulationUseRange = DashboardOvulationRange(
				displayNextPeriodRangeStart,
				displayNextPeriodRangeEnd,
				stats.LutealPhase,
				location,
			)
			if displayOvulationUseRange {
				displayOvulationDate = time.Time{}
				displayOvulationExact = false
			}
		}
	}
	if displayOvulationNeedsData {
		displayOvulationDate = time.Time{}
		displayOvulationExact = false
	}

	return DashboardCycleContext{
		CycleDayReference:           cycleDayReference,
		CycleDayWarning:             cycleDayWarning,
		CycleDataStale:              cycleDataStale,
		PredictionDisabled:          false,
		DisplayNextPeriodStart:      displayNextPeriodStart,
		DisplayNextPeriodRangeStart: displayNextPeriodRangeStart,
		DisplayNextPeriodRangeEnd:   displayNextPeriodRangeEnd,
		DisplayNextPeriodUseRange:   displayNextPeriodUseRange,
		DisplayNextPeriodPrompt:     displayNextPeriodPrompt,
		DisplayNextPeriodNeedsData:  displayNextPeriodNeedsData,
		DisplayOvulationDate:        displayOvulationDate,
		DisplayOvulationRangeStart:  displayOvulationRangeStart,
		DisplayOvulationRangeEnd:    displayOvulationRangeEnd,
		DisplayOvulationUseRange:    displayOvulationUseRange,
		DisplayOvulationNeedsData:   displayOvulationNeedsData,
		DisplayOvulationExact:       displayOvulationExact,
		DisplayOvulationImpossible:  displayOvulationImpossible,
		NextPeriodInPast:            displayNextPeriodUseRange && !displayNextPeriodRangeEnd.IsZero() && displayNextPeriodRangeEnd.Before(today),
		OvulationInPast:             (!displayOvulationUseRange && !displayOvulationImpossible && !displayOvulationDate.IsZero() && displayOvulationDate.Before(today)) || (displayOvulationUseRange && !displayOvulationRangeEnd.IsZero() && displayOvulationRangeEnd.Before(today)),
	}
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
