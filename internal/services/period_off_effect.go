package services

import (
	"slices"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// PeriodOffEffect is what un-ticking the period on a day would change beyond
// that day: the following days the auto-fill walk would clear
// (ClearAutoFilledPeriodNeighbors) and whether the cycle boundaries move — a
// boundary the day opened, or the stored onboarding start the write withdraws
// (withdrawOnboardingStartOn). The day forms ask the owner to confirm only
// when there is such an effect; a plain un-tick asks nothing.
type PeriodOffEffect struct {
	ClearedDays     int
	MovesCycleStart bool
}

// Any reports whether the un-tick reaches past its own day.
func (effect PeriodOffEffect) Any() bool {
	return effect.ClearedDays > 0 || effect.MovesCycleStart
}

// periodOffEffectFor is a dry run of the day write that un-ticks the period on
// day (applyDayWriteAndAutoFill), read over the owner's logs as the form was
// rendered: a day the form shows unticked has nothing to un-tick, so the effect
// is measured from the state the owner sees. It follows the write step by step
// — the anchor loses its period and its cycle-start mark, the stored start on
// that date is withdrawn, and with auto-fill on the bare neighbours after a
// period start are cleared — and compares the whole boundary set, so a moved
// older boundary counts as much as the newest one.
func periodOffEffectFor(user *models.User, logs []models.DailyLog, day time.Time, now time.Time, location *time.Location) PeriodOffEffect {
	if user == nil {
		return PeriodOffEffect{}
	}
	if location == nil {
		location = time.UTC
	}

	byDay := make(map[string]int, len(logs))
	for index, entry := range logs {
		byDay[CalendarDayKey(entry.Date)] = index
	}
	dayKey := CalendarDayKey(day)
	anchorIndex, found := byDay[dayKey]
	var anchor models.DailyLog
	if found {
		anchor = logs[anchorIndex]
	}
	tickedFromStart := !found && onboardingStartTicks(user, day, now, location)
	if !anchor.IsPeriod && !tickedFromStart {
		return PeriodOffEffect{}
	}

	today := DateAtLocation(now, location)
	before := CycleBoundaries(logs, BoundaryContextFor(user, today))

	after := slices.Clone(logs)
	if found {
		after[anchorIndex].IsPeriod = false
		after[anchorIndex].CycleStart = false
		after[anchorIndex].IsUncertain = false
		after[anchorIndex].Flow = models.FlowNone
	}
	afterCtx := BoundaryContextFor(user, today)
	if user.LastPeriodStart != nil && CalendarDayKey(dateOnly(*user.LastPeriodStart)) == dayKey {
		afterCtx.OnboardingStart = time.Time{}
	}

	effect := PeriodOffEffect{}
	if found && user.AutoPeriodFill && !periodOffPreviousDayIsPeriod(logs, byDay, day, location) {
		periodLength := normalizedAutoFillPeriodLength(user.PeriodLength)
		for offset := 1; offset < periodLength; offset++ {
			index, ok := byDay[CalendarDayKey(AddCalendarDays(day, offset, location))]
			if !ok || !IsAutoFilledPeriodCandidate(logs[index], anchor.Flow) {
				break
			}
			after[index].IsPeriod = false
			after[index].Flow = models.FlowNone
			effect.ClearedDays++
		}
	}

	effect.MovesCycleStart = !slices.EqualFunc(before, CycleBoundaries(after, afterCtx), time.Time.Equal)
	return effect
}

func periodOffPreviousDayIsPeriod(logs []models.DailyLog, byDay map[string]int, day time.Time, location *time.Location) bool {
	index, ok := byDay[CalendarDayKey(AddCalendarDays(day, -1, location))]
	return ok && logs[index].IsPeriod
}
