package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ResolvePregnancyPause reports whether cycle predictions should be paused
// because a positive pregnancy test is the user's most recent fertility
// signal, returning the date of that latest positive test when paused.
//
// Pause is active when a positive pregnancy test exists and no cycle starts
// strictly after it. "A cycle starts" is CycleBoundaries' answer over the same
// logs and context, never a raw IsPeriod+CycleStart flag: an unmarked bleeding
// run that the rule counts lifts the pause, while a spotting day or an
// uncertain mark — which open no cycle anywhere else — does not. A boundary on
// the same calendar day as the positive test does not lift the pause — the
// positive result wins ties. Stored dates are canonical UTC-midnight (migration
// 019 + DailyLog.BeforeSave), and both sides are compared as calendar days.
//
// The rule runs without ctx.Today: a lone bleeding day dated today or yesterday
// opens a cycle elsewhere only while the period may still be running, and a
// pause lifted on it would come back the day after when no second day follows —
// predictions and outbound notifications would resume for a pregnant owner for
// a day and then stop again. Only a marked start or a two-day run lifts the
// pause. Today still drops an onboarding start dated after it.
//
// The ovumcy-app resolvePregnancyPause still reads the raw flag; the two need
// the same rule for parity.
func ResolvePregnancyPause(logs []models.DailyLog, ctx BoundaryContext) (time.Time, bool) {
	var latestPositive time.Time
	for _, logEntry := range logs {
		if logEntry.PregnancyTest != models.PregnancyTestPositive {
			continue
		}
		if latestPositive.IsZero() || logEntry.Date.After(latestPositive) {
			latestPositive = logEntry.Date
		}
	}
	if latestPositive.IsZero() {
		return time.Time{}, false
	}

	starts := CycleBoundaries(logs, BoundaryContext{OnboardingStart: OnboardingBoundaryDay(ctx)})
	if len(starts) > 0 && CalendarDaysBetween(dateOnly(latestPositive), starts[len(starts)-1]) > 0 {
		return time.Time{}, false
	}
	return latestPositive, true
}
