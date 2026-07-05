package services

import "time"

// DashboardReminderBannerWindowDays is the threshold policy for issue #123:
// the in-app reminder banner ("Period likely today/tomorrow", or "…likely in
// ~N days" further out) only shows once the predicted date is within this
// many days of today, so the dashboard is not permanently cluttered with a
// months-away estimate.
//
// This is the fallback default for the planned per-user `reminder_lead_days`
// setting (a separate PR): keep it as a single named constant so wiring the
// setting later is a one-line swap.
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

// The banner copy is chosen by how close the predicted date is:
//   - day 0 (the event is today) uses the flat "_today" key,
//   - day 1 (tomorrow) uses the flat "_tomorrow" key,
//   - day 2 up to the threshold uses the "~N days" plural base key
//     (".one"/".few"/".many"/".other" per internal/i18n/plural.go), resolved
//     via i18n.TranslatePlural(messages, language, key, DaysUntil).
//
// The "_today"/"_tomorrow" keys are deliberately not plural groups (no
// ".one"/".other" siblings), so the locale parity test treats them as plain
// keys required verbatim in every locale rather than as CLDR plural bases.
const (
	DashboardReminderBannerPeriodKey            = "dashboard.reminder_banner_period"
	DashboardReminderBannerPeriodTodayKey       = "dashboard.reminder_banner_period_today"
	DashboardReminderBannerPeriodTomorrowKey    = "dashboard.reminder_banner_period_tomorrow"
	DashboardReminderBannerOvulationKey         = "dashboard.reminder_banner_ovulation"
	DashboardReminderBannerOvulationTodayKey    = "dashboard.reminder_banner_ovulation_today"
	DashboardReminderBannerOvulationTomorrowKey = "dashboard.reminder_banner_ovulation_tomorrow"
)

// DashboardReminderBanner is the pure result of the threshold policy: whether
// to show an in-app reminder banner on the dashboard, and if so, which
// prediction it summarizes and how many days away it is. It carries no
// rendering concerns beyond the i18n key selection — the disclaimer and
// estimate qualifier are rendered by the existing dashboard prediction
// surface (dashboard.prediction_disclaimer), not duplicated here.
//
// Countable reports whether TitleKey is the "~N days" plural copy (which the
// caller resolves with the day count) rather than the fixed "today"/"tomorrow"
// copy: only the plural variant carries a %d verb, so the template must not
// printf the count into the today/tomorrow strings.
type DashboardReminderBanner struct {
	Show        bool
	Kind        string
	TitleKey    string
	DaysUntil   int
	Countable   bool
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
	titleKey, countable := dashboardReminderBannerCopy(
		daysUntil,
		DashboardReminderBannerPeriodTodayKey,
		DashboardReminderBannerPeriodTomorrowKey,
		DashboardReminderBannerPeriodKey,
	)
	return DashboardReminderBanner{
		Show:      true,
		Kind:      DashboardReminderBannerKindPeriod,
		TitleKey:  titleKey,
		DaysUntil: daysUntil,
		Countable: countable,
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
	titleKey, countable := dashboardReminderBannerCopy(
		daysUntil,
		DashboardReminderBannerOvulationTodayKey,
		DashboardReminderBannerOvulationTomorrowKey,
		DashboardReminderBannerOvulationKey,
	)
	return DashboardReminderBanner{
		Show:        true,
		Kind:        DashboardReminderBannerKindOvulation,
		TitleKey:    titleKey,
		DaysUntil:   daysUntil,
		Countable:   countable,
		Approximate: !cycleContext.DisplayOvulationExact,
	}, true
}

// dashboardReminderBannerCopy selects the i18n key for a reminder banner from
// how many days away the predicted date is: day 0 uses the fixed "today"
// copy, day 1 the fixed "tomorrow" copy, and day 2+ the "~N days" plural
// base key. The bool return (countable) is true only for the plural case,
// where the caller interpolates the day count.
func dashboardReminderBannerCopy(daysUntil int, todayKey string, tomorrowKey string, countKey string) (string, bool) {
	switch daysUntil {
	case 0:
		return todayKey, false
	case 1:
		return tomorrowKey, false
	default:
		return countKey, true
	}
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
