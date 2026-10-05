package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ResolvePregnancyPause reports whether cycle predictions should be paused
// because a positive pregnancy test is the user's most recent fertility
// signal, returning the date of that latest positive test when paused.
//
// Pause is active when a positive pregnancy test exists and no cycle start (a
// day flagged both IsPeriod and CycleStart) is logged on or after it. A cycle
// start on the same calendar day as the positive test lifts the pause: the
// pause copy tells the owner to log a new period to resume, and a start marked
// on the test day is that action. Stored dates are canonical UTC-midnight
// (migration 019 + DailyLog.BeforeSave), so time.Time comparison is
// calendar-day exact.
func ResolvePregnancyPause(logs []models.DailyLog) (time.Time, bool) {
	var latestPositive time.Time
	var latestCycleStart time.Time
	for _, logEntry := range logs {
		if logEntry.PregnancyTest == models.PregnancyTestPositive {
			if latestPositive.IsZero() || logEntry.Date.After(latestPositive) {
				latestPositive = logEntry.Date
			}
		}
		if logEntry.IsPeriod && logEntry.CycleStart {
			if latestCycleStart.IsZero() || logEntry.Date.After(latestCycleStart) {
				latestCycleStart = logEntry.Date
			}
		}
	}

	if latestPositive.IsZero() {
		return time.Time{}, false
	}
	if !latestCycleStart.IsZero() && !latestCycleStart.Before(latestPositive) {
		return time.Time{}, false
	}
	return latestPositive, true
}
