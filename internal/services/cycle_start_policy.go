package services

import (
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const manualCycleStartFutureDays = 2
const manualCycleStartSuggestionGapDays = 15

var (
	ErrManualCycleStartDateInvalid        = errors.New("manual cycle start date invalid")
	ErrManualCycleStartReplaceRequired    = errors.New("manual cycle start replace required")
	ErrManualCycleStartConfirmationNeeded = errors.New("manual cycle start confirmation needed")
)

type ManualCycleStartPolicy struct {
	ConflictDate          time.Time
	PreviousStart         time.Time
	ShortGapDays          int
	PotentialImplantation bool
	ImplantationGapDays   int
}

func manualCycleStartMaxDate(now time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	today := DateAtLocation(now.In(location), location)
	return AddCalendarDays(today, manualCycleStartFutureDays, location)
}

func IsAllowedManualCycleStartDate(day time.Time, now time.Time, location *time.Location) bool {
	if day.IsZero() {
		return false
	}
	if location == nil {
		location = time.UTC
	}

	day = DateAtLocation(day, location)
	return !day.After(manualCycleStartMaxDate(now, location))
}

func ResolveManualCycleStartPolicy(user *models.User, logs []models.DailyLog, day time.Time, now time.Time, location *time.Location) ManualCycleStartPolicy {
	if location == nil {
		location = time.UTC
	}

	// The refusal reads the RAW input, like IsAllowedManualCycleStartDate
	// above: DateAtLocation has no zero short-circuit, so a projected zero day
	// is an ordinary year-1 calendar day in every zone with a non-zero offset
	// and IsZero() answers false there.
	if day.IsZero() {
		return ManualCycleStartPolicy{}
	}

	targetDay := DateAtLocation(day, location)
	policy := ManualCycleStartPolicy{
		ConflictDate: findCompetingCycleStart(logs, targetDay, location),
	}

	previousStart := LatestCycleStartAnchorBeforeOrOn(user, logs, AddCalendarDays(targetDay, -1, location), location)
	if previousStart.IsZero() {
		return policy
	}

	gapDays := CalendarDaysBetween(previousStart, targetDay)
	if gapDays > 0 && gapDays < manualCycleStartSuggestionGapDays {
		policy.PreviousStart = previousStart
		policy.ShortGapDays = gapDays
	}
	if implantationGapDays, ok := potentialImplantationGapDays(user, logs, targetDay, previousStart); ok {
		policy.PotentialImplantation = true
		policy.ImplantationGapDays = implantationGapDays
	}

	return policy
}

func potentialImplantationGapDays(user *models.User, logs []models.DailyLog, targetDay time.Time, previousStart time.Time) (int, bool) {
	filtered := filterLogsNotAfter(logs, AddCalendarDays(targetDay, -1, targetDay.Location()))
	stats := BuildCycleStats(filtered, targetDay.Add(-time.Second))
	cycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	// codecov:ignore:start -- unreachable from this caller, kept as a floor for
	// a future statistic that can report "unknown". stats are computed HERE by
	// BuildCycleStats, so either there are fewer than two detected cycle starts
	// (median and average both 0 -> predictedCycleLength returns
	// models.DefaultCycleLength) or the starts are distinct sorted calendar days
	// (every observed length >= 1 -> median > 0). Callers that pass CALLER-BUILT
	// CycleStats can still drive predictedCycleLength to 0 with a fractional
	// average in (0, 0.5) — applyProjectedBaseline is that shape and its guards
	// are live — which is why these two are left in place rather than removed.
	if cycleLength <= 0 {
		cycleLength = DashboardCycleReferenceLength(user, stats)
	}
	if cycleLength <= 0 {
		return 0, false
	}
	// codecov:ignore:end

	window := PredictCycleWindow(previousStart, cycleLength, stats.LutealPhase)
	if !window.Calculable || window.OvulationDate.IsZero() {
		return 0, false
	}

	// window.OvulationDate is a UTC-midnight date-only value from
	// PredictCycleWindow while targetDay is a location-midnight working value;
	// compare calendar days instead of instants. DateAtLocation on the
	// UTC-midnight value would shift the day backward in UTC-minus locales
	// (issue #48 class).
	gapDays := CalendarDaysBetween(window.OvulationDate, targetDay)
	if gapDays >= 6 && gapDays <= 12 {
		return gapDays, true
	}
	return 0, false
}

func LatestCycleStartAnchorBeforeOrOn(user *models.User, logs []models.DailyLog, day time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}

	targetDay := DateAtLocation(day.In(location), location)
	explicitStart := latestExplicitCycleStartBeforeOrOn(logs, targetDay, location)
	return latestCycleStartAnchorBeforeOrOn(user, explicitStart, targetDay, location)
}

func ShouldSuggestManualCycleStart(user *models.User, logs []models.DailyLog, logEntry models.DailyLog, day time.Time, now time.Time, location *time.Location) bool {
	return logEntry.IsPeriod && cycleStartGapSuggestsNewCycle(user, logs, logEntry, day, now, location)
}

// ShouldAskCycleStartQuestion reports whether the day form asks, inline next to
// the period toggle, whether a new cycle begins on this day.
//
// It is the suggestion policy above evaluated on the day as the form is about
// to leave it, which is why the stored IsPeriod flag must not gate it: bleeding
// starting is one event, so the question belongs to the toggle being turned on,
// and on a day marked as a period day for the first time the persisted entry
// still reads IsPeriod=false while the form renders. A day that already carries
// a competing cycle start inside its period cluster is excluded — answering yes
// there would have to replace that start, which is the separate manual control's
// confirmation flow, not a calm one-tap question.
func ShouldAskCycleStartQuestion(user *models.User, logs []models.DailyLog, logEntry models.DailyLog, day time.Time, now time.Time, location *time.Location) bool {
	if location == nil {
		location = time.UTC
	}
	if !cycleStartGapSuggestsNewCycle(user, logs, logEntry, day, now, location) {
		return false
	}
	return findCompetingCycleStart(logs, DateAtLocation(day.In(location), location), location).IsZero()
}

// cycleStartGapSuggestsNewCycle is the shared core: this day may be marked as a
// cycle start, is not one already, and sits far enough past the previous anchor
// that a new cycle is plausible.
func cycleStartGapSuggestsNewCycle(user *models.User, logs []models.DailyLog, logEntry models.DailyLog, day time.Time, now time.Time, location *time.Location) bool {
	if logEntry.CycleStart || !IsAllowedManualCycleStartDate(day, now, location) {
		return false
	}

	anchor := LatestCycleStartAnchorBeforeOrOn(user, logs, AddCalendarDays(day, -1, location), location)
	if anchor.IsZero() {
		return false
	}

	targetDay := DateAtLocation(day.In(location), location)
	gapDays := CalendarDaysBetween(anchor, targetDay)
	return gapDays >= manualCycleStartSuggestionGapDays
}

func findCompetingCycleStart(logs []models.DailyLog, day time.Time, location *time.Location) time.Time {
	clusterStart, clusterEnd, ok := manualCycleStartClusterBounds(logs, day, location)
	if !ok {
		return time.Time{}
	}

	conflict := time.Time{}
	for _, logEntry := range logs {
		if !logEntry.CycleStart {
			continue
		}

		logDay := CalendarDay(logEntry.Date, location)
		if sameCalendarDay(logDay, day) {
			continue
		}
		if !withinPeriodCluster(logDay, clusterStart, clusterEnd) {
			continue
		}
		if conflict.IsZero() || logDay.Before(conflict) {
			conflict = logDay
		}
	}

	return conflict
}

func manualCycleStartClusterBounds(logs []models.DailyLog, day time.Time, location *time.Location) (time.Time, time.Time, bool) {
	targetDay := DateAtLocation(day, location)
	hypotheticalLogs := logsWithSyntheticPeriodDay(logs, targetDay)
	clusters := buildPeriodClusters(hypotheticalLogs)
	for _, cluster := range clusters {
		if withinPeriodCluster(targetDay, cluster.Start, cluster.End) {
			return cluster.Start, cluster.End, true
		}
	}
	return time.Time{}, time.Time{}, false
}

// withinPeriodCluster reports whether a calendar day falls inside the bounds
// buildPeriodClusters produced. Those bounds are UTC-midnight values (dateOnly)
// while the days compared against them are location-midnight working values, so
// the day is re-anchored to UTC-midnight first. Comparing the two midnights
// directly is an instant comparison under a non-zero UTC offset, which places a
// day on the edge of its own cluster outside it — ahead of UTC the first day,
// behind UTC the last one (issue #48 class).
func withinPeriodCluster(day time.Time, clusterStart time.Time, clusterEnd time.Time) bool {
	canonicalDay := dateOnly(day)
	return !canonicalDay.Before(clusterStart) && !canonicalDay.After(clusterEnd)
}

func logsWithSyntheticPeriodDay(logs []models.DailyLog, day time.Time) []models.DailyLog {
	syntheticLogs := make([]models.DailyLog, 0, len(logs)+1)
	syntheticLogs = append(syntheticLogs, logs...)

	for _, logEntry := range logs {
		if !sameCalendarDay(dateOnly(logEntry.Date), day) {
			continue
		}
		if logEntry.IsPeriod {
			return syntheticLogs
		}
	}

	syntheticLogs = append(syntheticLogs, models.DailyLog{
		Date:     day,
		IsPeriod: true,
	})
	return syntheticLogs
}
