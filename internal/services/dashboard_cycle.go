package services

import (
	"math"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type DashboardCycleContext struct {
	CycleDayReference           int
	CycleDayWarning             bool
	CycleDataStale              bool
	PredictionDisabled          bool
	DisplayNextPeriodStart      time.Time
	DisplayNextPeriodEnd        time.Time
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

type dashboardPredictionDisplay struct {
	nextPeriodStart      time.Time
	nextPeriodEnd        time.Time
	nextPeriodRangeStart time.Time
	nextPeriodRangeEnd   time.Time
	nextPeriodUseRange   bool
	nextPeriodPrompt     bool
	nextPeriodNeedsData  bool
	ovulationDate        time.Time
	ovulationRangeStart  time.Time
	ovulationRangeEnd    time.Time
	ovulationUseRange    bool
	ovulationNeedsData   bool
	ovulationExact       bool
	ovulationImpossible  bool
}

func DashboardPredictionDisabled(user *models.User) bool {
	return user != nil && user.UnpredictableCycle
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
		return CalendarDay(stats.LastPeriodStart, location)
	}
	if user == nil || user.LastPeriodStart == nil || user.LastPeriodStart.IsZero() {
		return time.Time{}
	}
	return CalendarDay(*user.LastPeriodStart, location)
}

// dashboardPredictionRegularSpan returns the half-width, in days, of the
// next-period prediction range for users without irregular-cycle mode.
// Returns 0 when the user has too few completed cycles for the standard
// deviation to be meaningful, signalling the caller to show a single date.
//
// The span is round(StdDev) clamped to [1, 5]. The upper bound keeps the
// UI readable for high-variability cohorts (per-user SD ≈ 5–11 days in
// participants aged 45+ in Gibson et al., npj Digital Medicine 2023,
// Apple Women's Health Study, n=12,608).
func dashboardPredictionRegularSpan(stats CycleStats) int {
	if stats.CompletedCycleCount < 3 || stats.CycleLengthStdDev <= 0 {
		return 0
	}
	span := int(math.Round(stats.CycleLengthStdDev))
	if span < 1 {
		span = 1
	}
	if span > 5 {
		span = 5
	}
	return span
}

func dashboardIrregularPredictionRangeEnabled(user *models.User, stats CycleStats) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount >= 3 && stats.MinCycleLength > 0 && stats.MaxCycleLength >= stats.MinCycleLength
}

func DashboardPredictionRange(user *models.User, stats CycleStats, predictedStart time.Time, location *time.Location) (time.Time, time.Time, bool) {
	if predictedStart.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	if dashboardIrregularPredictionRangeEnabled(user, stats) {
		return CalendarDay(stats.LastPeriodStart.AddDate(0, 0, stats.MinCycleLength), location),
			CalendarDay(stats.LastPeriodStart.AddDate(0, 0, stats.MaxCycleLength), location),
			true
	}

	spanDays := dashboardPredictionRegularSpan(stats)
	if spanDays <= 0 {
		return time.Time{}, time.Time{}, false
	}
	return CalendarDay(predictedStart.AddDate(0, 0, -spanDays), location),
		CalendarDay(predictedStart.AddDate(0, 0, spanDays), location),
		true
}

func DashboardOvulationRange(nextPeriodRangeStart time.Time, nextPeriodRangeEnd time.Time, lutealPhase int, location *time.Location) (time.Time, time.Time, bool) {
	if nextPeriodRangeStart.IsZero() || nextPeriodRangeEnd.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	resolvedLutealPhase := ResolveLutealPhase(lutealPhase)
	rangeStart := CalendarDay(nextPeriodRangeStart.AddDate(0, 0, -resolvedLutealPhase), location)
	rangeEnd := CalendarDay(nextPeriodRangeEnd.AddDate(0, 0, -resolvedLutealPhase), location)
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

	nextPeriodStart = CalendarDay(cycleStart.AddDate(0, 0, cycleLength), today.Location())
	ovulationDate, _, _, ovulationExact, ovulationCalculable := PredictCycleWindow(
		cycleStart,
		cycleLength,
		stats.LutealPhase,
	)
	if ovulationCalculable && ovulationDate.Before(today) {
		cycleStart = ShiftCycleStartToFutureOvulation(cycleStart, ovulationDate, cycleLength, today)
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
	display := buildDashboardPredictionDisplay(user, stats, today, location, cycleDayReference)

	return DashboardCycleContext{
		CycleDayReference:           cycleDayReference,
		CycleDayWarning:             cycleDayWarning,
		CycleDataStale:              cycleDataStale,
		PredictionDisabled:          false,
		DisplayNextPeriodStart:      display.nextPeriodStart,
		DisplayNextPeriodEnd:        display.nextPeriodEnd,
		DisplayNextPeriodRangeStart: display.nextPeriodRangeStart,
		DisplayNextPeriodRangeEnd:   display.nextPeriodRangeEnd,
		DisplayNextPeriodUseRange:   display.nextPeriodUseRange,
		DisplayNextPeriodPrompt:     display.nextPeriodPrompt,
		DisplayNextPeriodNeedsData:  display.nextPeriodNeedsData,
		DisplayOvulationDate:        display.ovulationDate,
		DisplayOvulationRangeStart:  display.ovulationRangeStart,
		DisplayOvulationRangeEnd:    display.ovulationRangeEnd,
		DisplayOvulationUseRange:    display.ovulationUseRange,
		DisplayOvulationNeedsData:   display.ovulationNeedsData,
		DisplayOvulationExact:       display.ovulationExact,
		DisplayOvulationImpossible:  display.ovulationImpossible,
		NextPeriodInPast:            dashboardNextPeriodInPast(display, today),
		OvulationInPast:             dashboardOvulationInPast(display, today),
	}
}

func buildDashboardPredictionDisplay(user *models.User, stats CycleStats, today time.Time, location *time.Location, cycleDayReference int) dashboardPredictionDisplay {
	nextPeriodStart, ovulationDate, ovulationExact, ovulationImpossible := DashboardUpcomingPredictions(
		stats,
		user,
		today,
		cycleDayReference,
	)

	display := dashboardPredictionDisplay{
		nextPeriodStart:     nextPeriodStart,
		nextPeriodEnd:       dashboardNextPeriodEnd(nextPeriodStart, stats, location),
		nextPeriodPrompt:    stats.LastPeriodStart.IsZero(),
		nextPeriodNeedsData: dashboardNeedsNextPeriodData(user, stats, nextPeriodStart),
		ovulationDate:       ovulationDate,
		ovulationNeedsData:  dashboardNeedsOvulationData(user, stats),
		ovulationExact:      ovulationExact,
		ovulationImpossible: ovulationImpossible,
	}
	if display.nextPeriodPrompt || display.nextPeriodNeedsData {
		return finalizeDashboardPredictionDisplay(display)
	}
	return finalizeDashboardPredictionDisplay(applyDashboardPredictionRanges(display, user, stats, location))
}

func dashboardNeedsNextPeriodData(user *models.User, stats CycleStats, nextPeriodStart time.Time) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !nextPeriodStart.IsZero()
}

func dashboardNeedsOvulationData(user *models.User, stats CycleStats) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !stats.LastPeriodStart.IsZero()
}

func applyDashboardPredictionRanges(display dashboardPredictionDisplay, user *models.User, stats CycleStats, location *time.Location) dashboardPredictionDisplay {
	display.nextPeriodRangeStart, display.nextPeriodRangeEnd, display.nextPeriodUseRange = DashboardPredictionRange(
		user,
		stats,
		display.nextPeriodStart,
		location,
	)
	if !dashboardIrregularPredictionRangeEnabled(user, stats) {
		return display
	}
	display.ovulationRangeStart, display.ovulationRangeEnd, display.ovulationUseRange = DashboardOvulationRange(
		display.nextPeriodRangeStart,
		display.nextPeriodRangeEnd,
		stats.LutealPhase,
		location,
	)
	if display.ovulationUseRange {
		display.ovulationDate = time.Time{}
		display.ovulationExact = false
	}
	return display
}

func finalizeDashboardPredictionDisplay(display dashboardPredictionDisplay) dashboardPredictionDisplay {
	if !display.ovulationNeedsData {
		return display
	}
	display.ovulationDate = time.Time{}
	display.ovulationExact = false
	return display
}

func dashboardNextPeriodEnd(nextPeriodStart time.Time, stats CycleStats, location *time.Location) time.Time {
	if nextPeriodStart.IsZero() {
		return time.Time{}
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	if periodLength <= 0 {
		return time.Time{}
	}

	return CalendarDay(nextPeriodStart.AddDate(0, 0, periodLength-1), location)
}

func dashboardNextPeriodInPast(display dashboardPredictionDisplay, today time.Time) bool {
	return display.nextPeriodUseRange && !display.nextPeriodRangeEnd.IsZero() && display.nextPeriodRangeEnd.Before(today)
}

func dashboardOvulationInPast(display dashboardPredictionDisplay, today time.Time) bool {
	if display.ovulationUseRange {
		return !display.ovulationRangeEnd.IsZero() && display.ovulationRangeEnd.Before(today)
	}
	return !display.ovulationImpossible && !display.ovulationDate.IsZero() && display.ovulationDate.Before(today)
}

func CompletedCycleTrendLengths(logs []models.DailyLog, now time.Time, location *time.Location) []int {
	starts := DetectCycleStarts(logs)
	if len(starts) < 2 {
		return nil
	}

	today := DateAtLocation(now, location)
	lengths := make([]int, 0, len(starts)-1)
	for index := 1; index < len(starts); index++ {
		previousStart := CalendarDay(starts[index-1], location)
		currentStart := CalendarDay(starts[index], location)
		if !currentStart.Before(today) {
			break
		}
		lengths = append(lengths, int(currentStart.Sub(previousStart).Hours()/24))
	}
	return lengths
}
