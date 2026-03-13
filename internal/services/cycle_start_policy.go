package services

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const manualCycleStartFutureDays = 1
const manualCycleStartSuggestionGapDays = 15

var ErrManualCycleStartDateInvalid = errors.New("manual cycle start date invalid")

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
