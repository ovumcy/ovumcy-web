package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Cycle-day reminder decision (WEB-383). Like DecideDueReminders it is PURE:
// now and location are injected, the watermarks are read and never written,
// and nothing is delivered.
//
// A rule for cycle day N is due when ALL of these hold:
//
//   - a cycle start exists (stats.LastPeriodStart, i.e. CycleBoundaries — the
//     stored onboarding start is never read on its own here);
//   - the owner is not in a pregnancy pause;
//   - the owner-local today IS day N of that cycle, and the owner-local hour has
//     reached the rule's hour. There is no lead window and no next-day catch-up:
//     a reminder to take something on day N is wrong on any other day, so a day
//     the pass never observes is missed rather than delivered late;
//   - for N <= 2, the start is settled: a lone bleeding day logged today or
//     yesterday opens a provisional cycle that is withdrawn the day after when
//     no second day follows, so it must also be a boundary without that rule
//     (the construction ResolvePregnancyPause uses). From day 3 on an unsettled
//     lone day can no longer be the running start;
//   - the rule has not been sent within this cycle: last_sent_on is NULL or
//     strictly before the start. A start corrected forward after a send
//     therefore does not produce a second prompt in the same cycle;
//   - the cycle has not run past the owner's usual length
//     (DashboardCycleDayLooksStale against DashboardCycleReferenceLength, the
//     dashboard's out-of-date verdict, no grace). Past it the count cannot tell a
//     late period from an unlogged one, and an unlogged start turns "day 25"
//     into day 3 of a new cycle.
//
// A rule withheld ONLY by the last condition is "held": it is not sent, and the
// owner receives one neutral notice per cycle start instead, so a reminder they
// rely on never goes silent without a word. Unpredictable- and irregular-cycle
// modes do not suppress: a cycle-day count is observed, not projected.

const (
	DueReminderTypeCycleDay     = models.WebhookReminderTypeCycleDay
	DueReminderTypeCycleDayHeld = models.WebhookReminderTypeCycleDayHeld
)

// DueCycleDayReminder is one rule due for delivery right now.
type DueCycleDayReminder struct {
	Rule       models.CycleDayReminder
	Day        time.Time
	CycleDay   int
	CycleStart time.Time
}

// CycleDayHoldNotice is the single neutral notice owed when a rule was held.
// CycleStart is its watermark key.
type CycleDayHoldNotice struct {
	Day        time.Time
	CycleStart time.Time
}

// CycleDayDecision is the result of DecideDueCycleDayReminders. Watermarked
// counts the rules a watermark already covered (the notify pass reports them
// as idempotent skips).
type CycleDayDecision struct {
	Due         []DueCycleDayReminder
	Hold        *CycleDayHoldNotice
	Watermarked int
}

// DecideDueCycleDayReminders reports the owner's cycle-day reminders due now.
// holdWatermark is users.webhook_cycle_day_hold_sent_start as the caller's
// snapshot carried it.
func DecideDueCycleDayReminders(user *models.User, rules []models.CycleDayReminder, holdWatermark *time.Time, logs []models.DailyLog, now time.Time, location *time.Location) CycleDayDecision {
	if user == nil || len(rules) == 0 {
		return CycleDayDecision{}
	}
	today := DateAtLocation(now, location)
	hour := now.In(location).Hour()

	logs = FilterLogsToStatsHistory(logs, now, location)
	stats := BuildCycleStatsFromLogs(user, logs, now, location)
	if stats.PregnancyPaused || stats.LastPeriodStart.IsZero() {
		return CycleDayDecision{}
	}
	start := CalendarDay(stats.LastPeriodStart, location)
	cycleDay := CalendarDaysBetween(start, today) + 1
	if cycleDay <= 0 {
		return CycleDayDecision{}
	}
	held := DashboardCycleDayLooksStale(cycleDay, DashboardCycleReferenceLength(user, stats))
	settled := cycleDay > 2 || cycleStartIsSettled(user, logs, today, start)

	decision := CycleDayDecision{}
	holdOwed := false
	for _, rule := range rules {
		if rule.CycleDay != cycleDay || hour < rule.RemindAtHour {
			continue
		}
		if !settled {
			continue
		}
		if sentWithinCycle(rule.LastSentOn, start) {
			decision.Watermarked++
			continue
		}
		if held {
			holdOwed = true
			continue
		}
		decision.Due = append(decision.Due, DueCycleDayReminder{
			Rule:       rule,
			Day:        today,
			CycleDay:   cycleDay,
			CycleStart: start,
		})
	}
	if holdOwed {
		if watermarkCoversAnchor(holdWatermark, start) {
			decision.Watermarked++
		} else {
			decision.Hold = &CycleDayHoldNotice{Day: today, CycleStart: start}
		}
	}
	return decision
}

// cycleStartIsSettled reports whether start is a cycle boundary without the
// today/yesterday lone-day rule, the construction ResolvePregnancyPause uses.
func cycleStartIsSettled(user *models.User, logs []models.DailyLog, today time.Time, start time.Time) bool {
	ctx := BoundaryContextFor(user, today)
	settled := CycleBoundaries(filterLogsNotAfter(logs, today), BoundaryContext{OnboardingStart: OnboardingBoundaryDay(ctx)})
	for _, boundary := range settled {
		if CalendarDaysBetween(boundary, start) == 0 {
			return true
		}
	}
	return false
}

// sentWithinCycle reports a send dated on or after the running cycle's start.
func sentWithinCycle(lastSentOn *time.Time, start time.Time) bool {
	if lastSentOn == nil || lastSentOn.IsZero() {
		return false
	}
	return CalendarDaysBetween(start, *lastSentOn) >= 0
}
