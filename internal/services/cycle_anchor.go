package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// LatestCycleStartAnchorBeforeOrOn is the newest CycleBoundaries start dated on
// or before day, in location's calendar: the one answer behind the dashboard
// anchor, last_period_start and the manual-start policy. Days after `day` are
// ignored — a future anchor never moves the running cycle — and `day` is also
// the today the running-period rule reads.
func LatestCycleStartAnchorBeforeOrOn(user *models.User, logs []models.DailyLog, day time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}

	// `day` arrives as a localized "today" anchor (an instant projected to the
	// user's calendar) — DateAtLocation is appropriate. Boundaries are date-only
	// UTC values and use CalendarDay so a UTC-midnight storage representation
	// does not shift the calendar day.
	targetDay := DateAtLocation(day, location)
	observed := filterLogsNotAfter(logs, targetDay)
	starts := CycleBoundaries(observed, BoundaryContextFor(user, targetDay))
	latest := latestBoundaryOnOrBefore(starts, targetDay)
	if latest.IsZero() {
		return time.Time{}
	}
	return CalendarDay(latest, location)
}
