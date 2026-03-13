package services

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const manualCycleStartFutureDays = 2
const manualCycleStartSuggestionGapDays = 15

var (
	ErrManualCycleStartDateInvalid        = errors.New("manual cycle start date invalid")
	ErrManualCycleStartReplaceRequired    = errors.New("manual cycle start replace required")
	ErrManualCycleStartConfirmationNeeded = errors.New("manual cycle start confirmation needed")
)

type ManualCycleStartPolicy struct {
	ConflictDate  time.Time
	PreviousStart time.Time
	ShortGapDays  int
}

func manualCycleStartMaxDate(now time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	today := DateAtLocation(now.In(location), location)
	return today.AddDate(0, 0, manualCycleStartFutureDays)
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

	targetDay := DateAtLocation(day.In(location), location)
	if targetDay.IsZero() {
		return ManualCycleStartPolicy{}
	}

	policy := ManualCycleStartPolicy{
		ConflictDate: findCompetingCycleStart(logs, targetDay, location),
	}

	previousStart := LatestCycleStartAnchorBeforeOrOn(user, logs, targetDay.AddDate(0, 0, -1), location)
	if previousStart.IsZero() {
		return policy
	}

	gapDays := int(targetDay.Sub(previousStart).Hours() / 24)
	if gapDays > 0 && gapDays < manualCycleStartSuggestionGapDays {
		policy.PreviousStart = previousStart
		policy.ShortGapDays = gapDays
	}

	return policy
}

func LatestCycleStartAnchorBeforeOrOn(user *models.User, logs []models.DailyLog, day time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}

	targetDay := DateAtLocation(day.In(location), location)
	explicitStarts := DetectExplicitCycleStarts(logs)
	for index := len(explicitStarts) - 1; index >= 0; index-- {
		explicitStart := DateAtLocation(explicitStarts[index], location)
		if explicitStart.After(targetDay) {
			continue
		}
		return explicitStart
	}

	if user != nil && user.LastPeriodStart != nil {
		fallback := DateAtLocation(user.LastPeriodStart.In(location), location)
		if !fallback.After(targetDay) {
			return fallback
		}
	}

	return time.Time{}
}

func ShouldSuggestManualCycleStart(user *models.User, logs []models.DailyLog, logEntry models.DailyLog, day time.Time, now time.Time, location *time.Location) bool {
	if !logEntry.IsPeriod || logEntry.CycleStart || !IsAllowedManualCycleStartDate(day, now, location) {
		return false
	}

	anchor := LatestCycleStartAnchorBeforeOrOn(user, logs, day.AddDate(0, 0, -1), location)
	if anchor.IsZero() {
		return false
	}

	targetDay := DateAtLocation(day.In(location), location)
	gapDays := int(targetDay.Sub(anchor).Hours() / 24)
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

		logDay := DateAtLocation(logEntry.Date, location)
		if sameCalendarDay(logDay, day) {
			continue
		}
		if logDay.Before(clusterStart) || logDay.After(clusterEnd) {
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
		if !targetDay.Before(cluster.Start) && !targetDay.After(cluster.End) {
			return cluster.Start, cluster.End, true
		}
	}
	return time.Time{}, time.Time{}, false
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
