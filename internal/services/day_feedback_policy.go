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
	// The same today-bounded timeline BuildCycleStatsFromLogs derives from, for
	// the same reason: ResolvePregnancyPause lifts a pause on ANY cycle start
	// later than the positive test and has no today of its own, while manual
	// entry permits a start two days ahead. On the raw set that start decides
	// today, and this message would tell the owner their predictions had resumed
	// while every other surface still showed the pause. The two warnings below
	// keep the full set on purpose — each asks about the day being edited, not
	// about what the account's timeline supports today.
	timeline := filterLogsNotAfter(logs, today)
	stats := BuildCycleStats(timeline, today)
	// BuildCycleStats does not resolve the pregnancy pause itself (mirrors
	// StatsService.BuildCycleStatsFromLogs); resolve it here so the save
	// message can explain the paused predictions.
	if _, paused := ResolvePregnancyPause(timeline); paused {
		stats.PregnancyPaused = true
	}
	entry, err := service.FetchLogByDate(ctx, user.ID, day, location)
	if err != nil {
		return DayFeedbackState{}, err
	}

	state := DayFeedbackState{
		MessageKey: resolveDaySaveMessageKey(user, day, stats),
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
// panics there rather than reaching a guard in this package.
func resolveDaySaveMessageKey(user *models.User, day time.Time, stats CycleStats) string {
	// A positive pregnancy test pauses predictions (ResolvePregnancyPause);
	// explain the pause right at save time instead of a routine
	// self-care/fertile message, and carry the red-flag guidance.
	if stats.PregnancyPaused {
		return daySaveMessagePregnancyPaused
	}
	if user.UnpredictableCycle {
		return daySaveMessageNeutral
	}
	// `day` arrives as a location-midnight working value while the stats
	// fields carry UTC-midnight calendar days; re-anchor `day` to UTC-midnight
	// of its calendar components so the instant comparisons below cannot
	// shift a day across the UTC offset (issue #48 class).
	day = dateOnly(day)
	if !stats.LastPeriodStart.IsZero() {
		cycleDay := cycleDayAt(stats.LastPeriodStart, day)
		if cycleDay >= 1 && cycleDay <= 3 {
			return daySaveMessageSelfCare
		}
	}
	// The fertile line is a claim about right now, so it may only be made from
	// something the account recorded. Until the first cycle closes
	// (DashboardAwaitingFirstCycle, the same tier the dashboard withholds its
	// fertility surfaces at) there are no observed cycle lengths, so
	// predictedCycleLength falls through to models.DefaultCycleLength and the
	// window is that default projected forward with the default luteal phase.
	// Display confidence follows data confidence: where the only source is
	// configuration defaults, suppression is the floor and a qualifier is not
	// enough (docs/SECURITY_INVARIANTS.md -> medical safety), so the save falls
	// back to the neutral message rather than softening the fertile one.
	//
	// FOLLOW-UP: this is the fifth surface carrying that same zero-completed-cycle
	// floor, and the only one still spelling it out for itself — the calendar day
	// states, the .ics feed, the webhook reminder and the dashboard reminder
	// banner are being collapsed behind one shared suppression predicate in this
	// package. Fold this call into that predicate once it exists: a floor stated
	// at N of N+1 sites diverges the moment the shared one gains a disjunct or is
	// narrowed to let a recorded observation through, and this site would keep the
	// old rule silently.
	if !DashboardAwaitingFirstCycle(stats) &&
		!stats.FertilityWindowStart.IsZero() &&
		!day.Before(stats.FertilityWindowStart) &&
		!day.After(stats.FertilityWindowEnd) {
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
