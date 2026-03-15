package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func latestCycleStartAnchorBeforeOrOn(user *models.User, explicitStart time.Time, day time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}

	targetDay := DateAtLocation(day.In(location), location)
	explicitAnchor := normalizedCycleStartAnchorBeforeOrOn(explicitStart, targetDay, location)
	userAnchor := normalizedUserCycleStartAnchorBeforeOrOn(user, targetDay, location)
	return moreRecentCycleStartAnchor(explicitAnchor, userAnchor)
}

func latestExplicitCycleStartBeforeOrOn(logs []models.DailyLog, day time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}

	explicitStarts := DetectExplicitCycleStarts(logs)
	targetDay := DateAtLocation(day.In(location), location)
	for index := len(explicitStarts) - 1; index >= 0; index-- {
		explicitStart := DateAtLocation(explicitStarts[index], location)
		if explicitStart.After(targetDay) {
			continue
		}
		return explicitStart
	}
	return time.Time{}
}

func normalizedCycleStartAnchorBeforeOrOn(anchor time.Time, day time.Time, location *time.Location) time.Time {
	if anchor.IsZero() {
		return time.Time{}
	}

	normalized := DateAtLocation(anchor.In(location), location)
	if !day.IsZero() && normalized.After(day) {
		return time.Time{}
	}
	return normalized
}

func normalizedUserCycleStartAnchorBeforeOrOn(user *models.User, day time.Time, location *time.Location) time.Time {
	if user == nil || user.LastPeriodStart == nil || user.LastPeriodStart.IsZero() {
		return time.Time{}
	}
	return normalizedCycleStartAnchorBeforeOrOn(*user.LastPeriodStart, day, location)
}

func moreRecentCycleStartAnchor(first time.Time, second time.Time) time.Time {
	switch {
	case first.IsZero():
		return second
	case second.IsZero():
		return first
	case second.After(first):
		return second
	default:
		return first
	}
}
