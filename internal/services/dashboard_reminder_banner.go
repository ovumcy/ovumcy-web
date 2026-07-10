package services

import "time"

// DashboardReminderBannerWindowDays is the threshold policy for issue #123:
// the in-app reminder banner ("Period likely today/tomorrow", or "…likely in
// ~N days" further out) only shows once the predicted date is within this
// many days of today, so the dashboard is not permanently cluttered with a
// months-away estimate.
//
// This is now the FALLBACK default consulted by
// dashboardReminderBannerWindowDays below, used only when the owner's
// per-user reminder_lead_days (models.User.ReminderLeadDays, issue #124
// slice 1) is zero/unset or otherwise out of the save-time bound. It must
// keep this exact value (3) since it also matches the reminder_lead_days
// column default, so an owner who never touched the setting sees identical
// banner behavior before and after this switch.
const DashboardReminderBannerWindowDays = 3

// dashboardReminderBannerWindowDays resolves the "show within N days"
// threshold for a single request from the owner's reminder_lead_days:
//
//   - leadDays <= 0 (the zero value for a row predating the column, or an
//     explicit 0) falls back to DashboardReminderBannerWindowDays. 0 is a
//     valid *webhook* lead — "only on the day itself", see
//     MinReminderLeadDays in webhook_settings_service.go — but treating it
//     as a literal banner threshold would silently regress every existing
//     owner's banner to day-0-only, so the banner treats 0 as unset instead.
//   - A value above MaxReminderLeadDays — already rejected/clamped at save
//     by NormalizeReminderLeadDays, re-clamped here defensively in case the
//     column is ever populated by another path — is capped at
//     MaxReminderLeadDays rather than falling back, so a deliberately large
//     lead still governs the banner, just bounded to the same range the
//     save path enforces.
//   - Any other value (1..MaxReminderLeadDays) is used as-is.
func dashboardReminderBannerWindowDays(leadDays int) int {
	if leadDays <= 0 {
		return DashboardReminderBannerWindowDays
	}
	if leadDays > MaxReminderLeadDays {
		return MaxReminderLeadDays
	}
	return leadDays
}

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
// leadDays is the owner's models.User.ReminderLeadDays (issue #124 slice 1),
// forwarded by the caller from the already-loaded user — this function does
// no DB access. See dashboardReminderBannerWindowDays for how it resolves to
// the actual threshold, including the fallback/clamp rules.
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
//   - the predicted date is further away than the resolved window (the
//     owner's clamped reminder_lead_days, or DashboardReminderBannerWindowDays
//     as its fallback).
//
// When both the next period and ovulation fall inside the window on the same
// request, the period reminder is returned (see DashboardReminderBannerKindPeriod).
func BuildDashboardReminderBanner(cycleContext DashboardCycleContext, today time.Time, leadDays int) DashboardReminderBanner {
	if cycleContext.PredictionDisabled {
		return DashboardReminderBanner{}
	}
	windowDays := dashboardReminderBannerWindowDays(leadDays)

	if banner, ok := dashboardReminderBannerForPeriod(cycleContext, today, windowDays); ok {
		return banner
	}
	if banner, ok := dashboardReminderBannerForOvulation(cycleContext, today, windowDays); ok {
		return banner
	}
	return DashboardReminderBanner{}
}

func dashboardReminderBannerForPeriod(cycleContext DashboardCycleContext, today time.Time, windowDays int) (DashboardReminderBanner, bool) {
	if cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayNextPeriodNeedsData || cycleContext.DisplayNextPeriodPrompt {
		return DashboardReminderBanner{}, false
	}
	daysUntil, ok := dashboardReminderBannerDaysUntil(cycleContext.DisplayNextPeriodStart, today, windowDays)
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

func dashboardReminderBannerForOvulation(cycleContext DashboardCycleContext, today time.Time, windowDays int) (DashboardReminderBanner, bool) {
	if cycleContext.DisplayOvulationUseRange || cycleContext.DisplayOvulationNeedsData || cycleContext.DisplayOvulationImpossible {
		return DashboardReminderBanner{}, false
	}
	daysUntil, ok := dashboardReminderBannerDaysUntil(cycleContext.DisplayOvulationDate, today, windowDays)
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
// windowDays (inclusive at both the 0-day and the N-day boundary) — the
// already-resolved per-request threshold from
// dashboardReminderBannerWindowDays (the owner's clamped reminder_lead_days,
// or DashboardReminderBannerWindowDays as its fallback). predictedDate and
// today are calendar-date-only values (see CalendarDay/DateAtLocation in
// day_utils.go) that may carry different midnight shapes (location-midnight vs
// UTC-midnight); CalendarDaysBetween re-anchors both to UTC-midnight so the day
// count is immune to time-of-day/location skew and to a DST transition between
// the two dates.
func dashboardReminderBannerDaysUntil(predictedDate time.Time, today time.Time, windowDays int) (int, bool) {
	if predictedDate.IsZero() {
		return 0, false
	}
	daysUntil := CalendarDaysBetween(today, predictedDate)
	if daysUntil < 0 || daysUntil > windowDays {
		return 0, false
	}
	return daysUntil, true
}
