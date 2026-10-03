package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	daySaveMessageSelfCare        = "dashboard.save_message_self_care"
	daySaveMessageFertile         = "dashboard.save_message_fertile"
	daySaveMessageNeutral         = "dashboard.save_message_neutral"
	daySaveMessagePregnancyPaused = "dashboard.save_message_pregnancy_paused"
)

type DayFeedbackState struct {
	MessageKey               string
	ShowSpottingCycleWarning bool
	ShowLongPeriodWarning    bool
	LongPeriodCycleStart     time.Time
}

func (service *DayService) ResolveDayFeedback(ctx context.Context, user *models.User, day time.Time, now time.Time, location *time.Location) (DayFeedbackState, error) {
	if location == nil {
		location = time.UTC
	}

	logs, err := service.logs.ListByUser(ctx, user.ID)
	if err != nil {
		return DayFeedbackState{}, err
	}

	day = DateAtLocation(day, location)
	today := DateAtLocation(now, location)
	// The dashboard's own derivation, step for step: the stats come from the same
	// two-year history window through BuildCycleStatsFromLogs (which bounds the
	// timeline at today, the pregnancy pause included, and applies the owner's
	// luteal baseline), and the window then goes through the same confirmed-shift
	// and suppression gate. Plain BuildCycleStats fixed the luteal phase at its
	// default of 14 and never saw a thermal shift, so the toast named days the
	// dashboard did not shade and stayed silent on days it did. The two warnings
	// below keep the full set on purpose — each asks about the day being edited,
	// not about what the account's timeline supports today.
	stats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, today, location), now, location)
	_, published, suppression := ConfirmedAndPublishedStats(user, logs, stats, today, location)
	entry, err := service.FetchLogByDate(ctx, user.ID, day, location)
	if err != nil {
		return DayFeedbackState{}, err
	}

	state := DayFeedbackState{
		MessageKey: resolveDaySaveMessageKey(user, day, today, published, suppression),
	}

	if shouldShowSpottingCycleWarning(logs, entry, day, location) {
		state.ShowSpottingCycleWarning = true
	}

	streakLength, cycleStart, ok := currentPeriodStreakAtDay(logs, day, location)
	if ok && streakLength > 8 && (user.LongPeriodWarningCycleStart == nil || !sameCalendarDay(CalendarDay(*user.LongPeriodWarningCycleStart, location), cycleStart)) {
		state.ShowLongPeriodWarning = true
		state.LongPeriodCycleStart = cycleStart
	}

	return state, nil
}

func (service *DayService) AcknowledgeLongPeriodWarning(ctx context.Context, userID uint, cycleStart time.Time, location *time.Location) error {
	if service == nil || service.users == nil || cycleStart.IsZero() {
		return nil
	}
	if location != nil {
		cycleStart = CalendarDay(cycleStart, location)
	}
	// Canonicalize to UTC-midnight so the stored value matches the date-only
	// convention used by every other date column (issue #48/#64).  Updates(map)
	// bypasses the GORM BeforeSave hook, so we must normalize here explicitly.
	y, m, d := cycleStart.Date()
	cycleStart = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return service.users.UpdateByID(ctx, userID, map[string]any{
		"long_period_warning_cycle_start": cycleStart,
	})
}

// resolveDaySaveMessageKey requires a non-nil user: its only caller,
// ResolveDayFeedback, dereferences user.ID before it gets here, so a nil user
// panics there rather than reaching a guard in this package. It reads the
// PUBLISHED stats and the verdict that came with them
// (ConfirmedAndPublishedStats), never stats it derived itself: the fertile line
// may only name a window the dashboard would show. `today` is the owner's
// calendar day, in the same location-midnight shape as `day`.
func resolveDaySaveMessageKey(user *models.User, day time.Time, today time.Time, stats CycleStats, suppression PredictionSuppression) string {
	// A positive pregnancy test pauses predictions (ResolvePregnancyPause);
	// explain the pause right at save time instead of a routine
	// self-care/fertile message, and carry the red-flag guidance.
	if stats.PregnancyPaused {
		return daySaveMessagePregnancyPaused
	}
	if user.UnpredictableCycle {
		return daySaveMessageNeutral
	}
	// `day` arrives as a location-midnight working value, and so do the window
	// bounds: they come from the owner's baseline and the confirmed-shift
	// resolver, which anchor every date at the owner's local midnight. Each side
	// is therefore re-anchored to UTC-midnight of its own calendar components
	// before they are ordered, so the comparisons below read calendar days and
	// never instants (issue #48 class). Anchoring only `day` would be right in
	// UTC and one day off at an end of the window in every other zone.
	day = dateOnly(day)
	if !stats.LastPeriodStart.IsZero() {
		cycleDay := cycleDayAt(stats.LastPeriodStart, day)
		if cycleDay >= 1 && cycleDay <= 3 {
			return daySaveMessageSelfCare
		}
	}
	// The fertile line is an estimate about the window, so it may only be made
	// from one the other fertility surfaces would still publish — the verdict is
	// the fertility half of PredictionSuppression, the gate the calendar grid, the
	// .ics feed, the webhook reminder and the dashboard read (the window itself
	// is the confirmed one when the owner's temperatures have confirmed a shift,
	// and the personalised one when their luteal phase has been inferred). Two of
	// its tiers matter here
	// beyond the early returns above. Until the first cycle closes there are no
	// observed cycle lengths, so the window is the default length projected
	// forward with the default luteal phase: where the only source is
	// configuration defaults, suppression is the floor and a qualifier is not
	// enough (docs/SECURITY_INVARIANTS.md -> medical safety). And once the cycle
	// has run more than a week past its own length (DashboardCycleOverdue), the
	// window is a projection the account's data no longer supports: three 28-day
	// cycles beside one 300-day gap still placed it on days 9-14 of a cycle on
	// day 61, and saving one of those days called it fertile while every other
	// surface withheld the window. The save falls back to the neutral message
	// rather than softening the fertile one.
	//
	// A day already behind the owner is a backfill: the window is a projection
	// about the days ahead, and a past day the owner is recording after the fact
	// has nothing left to be estimated about, so the line would only read as a
	// claim about a day that is over. Only today (and the days ahead, which the
	// window does estimate) may carry it.
	//
	// Out-of-date data is a third tier, and the one PublishedStats does not answer
	// with a cleared window: it withholds the status and the phase and leaves the
	// projected dates beside the out-of-date banner. This message reads the window,
	// so it asks the same verdict (CycleDataStale on the published copy): a toast
	// naming a window the cycle has already outrun would say what both owner
	// pages, which print "unknown" there, do not.
	if !suppression.FertilitySuppressed &&
		!stats.CycleDataStale &&
		!day.Before(dateOnly(today)) &&
		!stats.FertilityWindowStart.IsZero() &&
		!day.Before(dateOnly(stats.FertilityWindowStart)) &&
		!day.After(dateOnly(stats.FertilityWindowEnd)) {
		return daySaveMessageFertile
	}
	return daySaveMessageNeutral
}

func shouldShowSpottingCycleWarning(logs []models.DailyLog, entry models.DailyLog, day time.Time, location *time.Location) bool {
	if !entry.IsPeriod || NormalizeDayFlow(entry.Flow) != models.FlowSpotting {
		return false
	}

	_, cycleStart, ok := currentPeriodStreakAtDay(logs, day, location)
	if !ok {
		return false
	}

	return sameCalendarDay(cycleStart, DateAtLocation(day, location))
}

func currentPeriodStreakAtDay(logs []models.DailyLog, day time.Time, location *time.Location) (int, time.Time, bool) {
	if len(logs) == 0 {
		return 0, time.Time{}, false
	}

	// The walk below is a run of CALENDAR days, so it is stepped as calendar
	// days: the requested day is re-anchored to UTC midnight and the cursor
	// moves there, the same convention BuildCalendarDayStates uses for the
	// month grid. Stepping inside the request zone re-enters time.Date there,
	// and in a UTC-minus zone whose DST jump lands on midnight
	// (America/Santiago 2026-09-06) the missing wall clock normalizes BACKWARD
	// into the previous calendar day: the step off 2026-09-07 landed on
	// 2026-09-05 and 2026-09-06 was never queried, undercounting a continuous
	// period by a day and jumping the gap that should have ended the walk. UTC
	// has no transitions, so the same arithmetic there visits every calendar
	// day exactly once for every request zone. Only the cursor's shape changes:
	// the calendar day it names on any other date is the one it named before.
	targetDay := CalendarDay(DateAtLocation(day, location), time.UTC)
	logByDay := make(map[string]models.DailyLog, len(logs))
	for _, logEntry := range sortDailyLogs(logs) {
		logByDay[CalendarDayKey(logEntry.Date)] = logEntry
	}

	current, ok := logByDay[CalendarDayKey(targetDay)]
	if !ok || !current.IsPeriod {
		return 0, time.Time{}, false
	}

	streak := 0
	cycleStart := targetDay
	for cursor := targetDay; ; cursor = cursor.AddDate(0, 0, -1) {
		logEntry, exists := logByDay[CalendarDayKey(cursor)]
		if !exists || !logEntry.IsPeriod {
			break
		}
		streak++
		cycleStart = cursor
	}
	return streak, cycleStart, true
}
