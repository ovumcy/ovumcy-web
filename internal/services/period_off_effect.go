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

// MessageKey is the stable locale key of the question this effect asks, without
// the plural suffix the count picks; empty when it asks nothing.
func (effect PeriodOffEffect) MessageKey() string {
	switch {
	case effect.ClearedDays > 0 && effect.MovesCycleStart:
		return "dashboard.period_off_confirm.moves_start_and_clears"
	case effect.ClearedDays > 0:
		return "dashboard.period_off_confirm.clears"
	case effect.MovesCycleStart:
		return "dashboard.period_off_confirm.moves_start"
	}
	return ""
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

	cleared := map[string]bool{dayKey: true}
	effect := PeriodOffEffect{}
	if found && user.AutoPeriodFill && !periodOffPreviousDayIsPeriod(logs, byDay, day, location) {
		periodLength := normalizedAutoFillPeriodLength(user.PeriodLength)
		for offset := 1; offset < periodLength; offset++ {
			key := CalendarDayKey(AddCalendarDays(day, offset, location))
			index, ok := byDay[key]
			if !ok || !IsAutoFilledPeriodCandidate(logs[index], anchor.Flow) {
				break
			}
			cleared[key] = true
			effect.ClearedDays++
		}
	}

	// Only period days take part in the boundaries (periodDaysOf), so both
	// sides are read over them alone rather than over a copy of the history.
	before := make([]models.DailyLog, 0, len(logs)/4)
	after := make([]models.DailyLog, 0, len(logs)/4)
	for _, entry := range logs {
		if !entry.IsPeriod {
			continue
		}
		before = append(before, entry)
		if !cleared[CalendarDayKey(entry.Date)] {
			after = append(after, entry)
		}
	}
	today := DateAtLocation(now, location)
	afterCtx := BoundaryContextFor(user, today)
	if user.LastPeriodStart != nil && CalendarDayKey(dateOnly(*user.LastPeriodStart)) == dayKey {
		afterCtx.OnboardingStart = time.Time{}
	}
	effect.MovesCycleStart = !slices.EqualFunc(CycleBoundaries(before, BoundaryContextFor(user, today)), CycleBoundaries(after, afterCtx), time.Time.Equal)
	return effect
}

func periodOffPreviousDayIsPeriod(logs []models.DailyLog, byDay map[string]int, day time.Time, location *time.Location) bool {
	index, ok := byDay[CalendarDayKey(AddCalendarDays(day, -1, location))]
	return ok && logs[index].IsPeriod
}
