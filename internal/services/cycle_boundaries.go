package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// periodClusterGapDays is the count of clear days between two period days from
// which the second one belongs to a new cluster: fewer than five clear days keep
// both in one bleeding episode.
const periodClusterGapDays = 5

// BoundaryContext is what the cycle-boundary rule reads beside the logged days.
// OnboardingStart is the owner's stored start (users.last_period_start), zero
// when there is none. Today is any instant on the owner's local today; it is
// what lets a lone bleeding day dated today or yesterday open a cycle, and it
// also drops an onboarding start dated after it. A zero Today switches both off.
type BoundaryContext struct {
	OnboardingStart time.Time
	Today           time.Time
}

// BoundaryContextFor builds the context for one owner: the stored onboarding
// start, read here and nowhere else, and the owner's local today.
func BoundaryContextFor(user *models.User, today time.Time) BoundaryContext {
	if user == nil {
		return BoundaryContext{Today: today}
	}
	return boundaryContextForStart(user.LastPeriodStart, today)
}

// boundaryContextForStart is BoundaryContextFor for a caller that holds the
// stored start column without a whole user row (the boot recompute's projection).
func boundaryContextForStart(lastPeriodStart *time.Time, today time.Time) BoundaryContext {
	ctx := BoundaryContext{Today: today}
	if lastPeriodStart != nil && !lastPeriodStart.IsZero() {
		ctx.OnboardingStart = *lastPeriodStart
	}
	return ctx
}

// OnboardingBoundaryDay is the calendar day the owner's stored start occupies as
// a cycle boundary (a UTC-midnight date-only value), zero when it is absent or
// dated after Today. It is the single reading of users.last_period_start — the
// boundary rule, the calendar's recorded cell and the out-of-date anchor all
// read it here. Adding a mood or a symptom on that date — to a non-period row,
// or over JSON to a date without a row — does not withdraw it: onboarding
// already recorded the period there. The owner withdraws it by un-ticking the
// period on that day (the day editor and the dashboard's Today form show it
// ticked when the day has no row, and post a hidden field saying the tick came
// from the stored start) or by deleting the day; either write clears the
// stored start in its own transaction.
func OnboardingBoundaryDay(ctx BoundaryContext) time.Time {
	if ctx.OnboardingStart.IsZero() {
		return time.Time{}
	}
	day := dateOnly(ctx.OnboardingStart)
	if !ctx.Today.IsZero() && CalendarDaysBetween(ctx.Today, day) > 0 {
		return time.Time{}
	}
	return day
}

// periodDay is one calendar day that counts toward a bleeding episode.
type periodDay struct {
	day       time.Time
	spotting  bool
	explicit  bool
	uncertain bool
}

type periodCluster struct {
	Start time.Time
	End   time.Time
	days  []periodDay
}

// isSpottingDay is the one reading of "spotting" for the cycle rule: the day's
// flow is spotting, or the owner's Spotting symptom is the only bleeding
// signal on it (no flow chosen). A day with a real flow is never spotting just
// because the symptom is ticked beside it.
func isSpottingDay(logEntry models.DailyLog) bool {
	flow := NormalizeDayFlow(logEntry.Flow)
	if flow == models.FlowSpotting {
		return true
	}
	return logEntry.HasSpottingSymptom && flow == models.FlowNone
}

// CycleBoundaries is the one function that says where cycles start. Every
// consumer of a cycle start — the completed-cycle count and lengths, the
// last-period-start and dashboard anchor, the calendar's recorded start, the
// stats insights and the luteal inference — reads its answer, so two surfaces
// cannot disagree about the same history.
//
// Period days group into clusters (fewer than five clear days between two of
// them keep one cluster). A cluster opens a cycle only when it has
//   - a non-spotting day the owner explicitly marked as a cycle start (and not
//     as uncertain), or
//   - two consecutive non-spotting period days, or
//   - a lone non-spotting period day dated today or yesterday: the period may
//     still be running, and it stops counting once a later day passes without a
//     second period day.
//
// The start is the earliest qualifying day (an explicit mark wins over the
// run): spotting days and days before it are not the start. Spotting never
// opens a cycle, even when marked. A cluster that does not qualify starts no
// cycle and does not split the cycle around it. A cluster whose only explicit
// mark is uncertain is withheld the same way.
//
// The onboarding start is a boundary of its own: it joins the grouping as an
// explicit non-spotting day, so a logged cluster it falls inside or adjoins
// yields one start, not two. The result is ascending, one UTC-midnight date per
// cycle, and is not clipped to Today: callers bound the logs they pass.
func CycleBoundaries(logs []models.DailyLog, ctx BoundaryContext) []time.Time {
	days := periodDaysOf(logs)
	if onboarding := OnboardingBoundaryDay(ctx); !onboarding.IsZero() {
		days = insertPeriodDay(days, periodDay{day: onboarding, explicit: true})
	}

	clusters := clusterPeriodDays(days)
	starts := make([]time.Time, 0, len(clusters))
	for _, cluster := range clusters {
		if start, opens := cluster.boundary(ctx.Today); opens {
			starts = append(starts, start)
		}
	}
	return starts
}

// newestBoundaryOpensClusterOf reports whether the newest cycle boundary over
// logs (under ctx, so the onboarding start joins the grouping as in
// CycleBoundaries) is the one opened by the period cluster that holds day. It is
// false when no cluster opens a cycle, and when the newest cycle starts in a
// later cluster than day's.
func newestBoundaryOpensClusterOf(logs []models.DailyLog, ctx BoundaryContext, day time.Time) bool {
	days := periodDaysOf(logs)
	if onboarding := OnboardingBoundaryDay(ctx); !onboarding.IsZero() {
		days = insertPeriodDay(days, periodDay{day: onboarding, explicit: true})
	}
	clusters := clusterPeriodDays(days)
	day = dateOnly(day)
	for index := len(clusters) - 1; index >= 0; index-- {
		if _, opens := clusters[index].boundary(ctx.Today); !opens {
			continue
		}
		return !day.Before(clusters[index].Start) && !day.After(clusters[index].End)
	}
	return false
}

// latestBoundaryOnOrBefore is the newest boundary dated on or before day, zero
// when there is none.
func latestBoundaryOnOrBefore(starts []time.Time, day time.Time) time.Time {
	for index := len(starts) - 1; index >= 0; index-- {
		if CalendarDaysBetween(day, starts[index]) <= 0 {
			return starts[index]
		}
	}
	return time.Time{}
}

func (cluster periodCluster) boundary(today time.Time) (time.Time, bool) {
	sawUncertain := false
	for _, entry := range cluster.days {
		if entry.spotting {
			continue
		}
		if entry.explicit {
			return entry.day, true
		}
		if entry.uncertain {
			sawUncertain = true
		}
	}
	if sawUncertain {
		return time.Time{}, false
	}

	for index, entry := range cluster.days {
		if entry.spotting {
			continue
		}
		if index+1 < len(cluster.days) {
			next := cluster.days[index+1]
			if !next.spotting && CalendarDaysBetween(entry.day, next.day) == 1 {
				return entry.day, true
			}
		}
		if !today.IsZero() {
			if sinceDay := CalendarDaysBetween(entry.day, today); sinceDay == 0 || sinceDay == 1 {
				return entry.day, true
			}
		}
	}
	return time.Time{}, false
}

// periodDaysOf lists the logged period days, one entry per calendar day and in
// date order. A day logged twice counts as spotting only when every entry is.
func periodDaysOf(logs []models.DailyLog) []periodDay {
	days := make([]periodDay, 0, len(logs))
	for _, logEntry := range sortDailyLogs(logs) {
		if !logEntry.IsPeriod {
			continue
		}
		entry := periodDay{
			day:       dateOnly(logEntry.Date),
			spotting:  isSpottingDay(logEntry),
			explicit:  logEntry.CycleStart && !logEntry.IsUncertain,
			uncertain: logEntry.CycleStart && logEntry.IsUncertain,
		}
		if last := len(days) - 1; last >= 0 && days[last].day.Equal(entry.day) {
			days[last].spotting = days[last].spotting && entry.spotting
			days[last].explicit = days[last].explicit || entry.explicit
			days[last].uncertain = days[last].uncertain || entry.uncertain
			continue
		}
		days = append(days, entry)
	}
	return days
}

// insertPeriodDay adds entry to the date-ordered days, merging with the day it
// shares a date with.
func insertPeriodDay(days []periodDay, entry periodDay) []periodDay {
	position := len(days)
	for index, existing := range days {
		if existing.day.Equal(entry.day) {
			days[index].spotting = false
			days[index].explicit = true
			return days
		}
		if existing.day.After(entry.day) {
			position = index
			break
		}
	}
	days = append(days, periodDay{})
	copy(days[position+1:], days[position:])
	days[position] = entry
	return days
}

// clusterPeriodDays is the one grouping of period days into bleeding episodes,
// shared by the boundary rule and the day form's competing-start check.
func clusterPeriodDays(days []periodDay) []periodCluster {
	clusters := make([]periodCluster, 0)
	for _, entry := range days {
		if len(clusters) > 0 {
			last := &clusters[len(clusters)-1]
			if CalendarDaysBetween(last.End, entry.day)-1 < periodClusterGapDays {
				last.days = append(last.days, entry)
				if entry.day.After(last.End) {
					last.End = entry.day
				}
				continue
			}
		}
		clusters = append(clusters, periodCluster{Start: entry.day, End: entry.day, days: []periodDay{entry}})
	}
	return clusters
}

// buildPeriodClusters groups the logged period days; the day form reads the
// bounds of the episode a day belongs to.
func buildPeriodClusters(logs []models.DailyLog) []periodCluster {
	return clusterPeriodDays(periodDaysOf(logs))
}
