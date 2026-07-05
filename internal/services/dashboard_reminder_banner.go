package services

import "time"

// DashboardReminderBannerWindowDays is the threshold policy for issue #123:
// the in-app reminder banner ("Period likely in ~N days" / "Ovulation
// likely in ~N days") only shows once the predicted date is within this many
// days of today, so the dashboard is not permanently cluttered with a
// months-away estimate.
const DashboardReminderBannerWindowDays = 3

const (
	// DashboardReminderBannerKindPeriod and DashboardReminderBannerKindOvulation
	// identify which existing prediction the banner is summarizing. A period
	// reminder takes priority over an ovulation reminder when both predictions
	// fall inside the window in the same request (see
	// BuildDashboardReminderBanner), since the period date is the more
	// actionable of the two for the owner.
	DashboardReminderBannerKindPeriod    = "period"
	DashboardReminderBannerKindOvulation = "ovulation"
)

// DashboardReminderBannerPeriodKey and DashboardReminderBannerOvulationKey are
// the i18n plural-group base keys (".one"/".few"/".many"/".other" per
// internal/i18n/plural.go) for the two banner copies. Callers resolve the
// final string via i18n.TranslatePlural(messages, language, key, DaysUntil).
const (
	DashboardReminderBannerPeriodKey    = "dashboard.reminder_banner_period"
	DashboardReminderBannerOvulationKey = "dashboard.reminder_banner_ovulation"
)

// DashboardReminderBanner is the pure result of the threshold policy: whether
// to show an in-app reminder banner on the dashboard, and if so, which
// prediction it summarizes and how many days away it is. It carries no
// rendering concerns beyond the i18n key selection — the disclaimer and
// estimate qualifier are rendered by the existing dashboard prediction
// surface (dashboard.prediction_disclaimer), not duplicated here.
type DashboardReminderBanner struct {
	Show        bool
	Kind        string
	TitleKey    string
	DaysUntil   int
	Approximate bool
}

// BuildDashboardReminderBanner derives the in-app reminder banner (issue
// #123) from the dashboard's already-computed cycle prediction context. It
// is pure: today is injected by the caller (ultimately the per-request
// timezone-aware `now` resolved in internal/api), never read from
// time.Now(), and only the single-date prediction fields already exposed by
// BuildDashboardCycleContext are consulted — no new prediction math.
//
// The banner is suppressed (Show=false) whenever:
//   - predictions are disabled or paused (PredictionDisabled),
//   - the cycle context has no single-date estimate to summarize (needs more
//     data, ovulation is impossible, or the corresponding date is zero),
//   - the context is showing an uncertainty *range* instead of a single date
//     (DisplayNextPeriodUseRange / DisplayOvulationUseRange) — "~N days"
//     implies one estimated date, so a range is left to the existing range
//     display rather than reduced to a single number here,
//   - the predicted date has already passed,
//   - the predicted date is further away than
//     DashboardReminderBannerWindowDays.
//
// When both the next period and ovulation fall inside the window on the same
// request, the period reminder is returned (see DashboardReminderBannerKindPeriod).
func BuildDashboardReminderBanner(cycleContext DashboardCycleContext, today time.Time) DashboardReminderBanner {
	if cycleContext.PredictionDisabled {
		return DashboardReminderBanner{}
	}

	if banner, ok := dashboardReminderBannerForPeriod(cycleContext, today); ok {
		return banner
	}
	if banner, ok := dashboardReminderBannerForOvulation(cycleContext, today); ok {
		return banner
	}
	return DashboardReminderBanner{}
}

func dashboardReminderBannerForPeriod(cycleContext DashboardCycleContext, today time.Time) (DashboardReminderBanner, bool) {
	if cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayNextPeriodNeedsData || cycleContext.DisplayNextPeriodPrompt {
		return DashboardReminderBanner{}, false
	}
	daysUntil, ok := dashboardReminderBannerDaysUntil(cycleContext.DisplayNextPeriodStart, today)
	if !ok {
		return DashboardReminderBanner{}, false
	}
	return DashboardReminderBanner{
		Show:      true,
		Kind:      DashboardReminderBannerKindPeriod,
		TitleKey:  DashboardReminderBannerPeriodKey,
		DaysUntil: daysUntil,
	}, true
}

func dashboardReminderBannerForOvulation(cycleContext DashboardCycleContext, today time.Time) (DashboardReminderBanner, bool) {
	if cycleContext.DisplayOvulationUseRange || cycleContext.DisplayOvulationNeedsData || cycleContext.DisplayOvulationImpossible {
		return DashboardReminderBanner{}, false
	}
	daysUntil, ok := dashboardReminderBannerDaysUntil(cycleContext.DisplayOvulationDate, today)
	if !ok {
		return DashboardReminderBanner{}, false
	}
	return DashboardReminderBanner{
		Show:        true,
		Kind:        DashboardReminderBannerKindOvulation,
		TitleKey:    DashboardReminderBannerOvulationKey,
		DaysUntil:   daysUntil,
		Approximate: !cycleContext.DisplayOvulationExact,
	}, true
}

// dashboardReminderBannerDaysUntil applies the threshold policy to a single
// predicted calendar date: not-yet-calculable (zero date) or already-past
// dates are rejected, then the remaining dates are accepted only within
// DashboardReminderBannerWindowDays (inclusive at both the 0-day and the
// N-day boundary). predictedDate and today are both calendar-date-only
// values (see CalendarDay/DateAtLocation in day_utils.go), so a plain day
// count avoids any time-of-day/location skew.
func dashboardReminderBannerDaysUntil(predictedDate time.Time, today time.Time) (int, bool) {
	if predictedDate.IsZero() {
		return 0, false
	}
	daysUntil := int(predictedDate.Sub(today).Hours() / 24)
	if daysUntil < 0 || daysUntil > DashboardReminderBannerWindowDays {
		return 0, false
	}
	return daysUntil, true
}
