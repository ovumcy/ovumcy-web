package services

import "github.com/ovumcy/ovumcy-web/internal/models"

// Late-cycle message keys. A cycle running past its expected end is a FACT
// about the account's own history, never a diagnosis, so no key here names a
// condition: the copy either states the measured excess over the owner's own
// observed range, states that the running cycle is still inside that range, or
// — when no personal range exists yet — states that only the cycle day is being
// shown. The last case is the important one: an account with no completed
// cycles has no "usual range" to be compared against, and inventing one at the
// most anxious moment in the product is worse than saying nothing.
const (
	LateCycleBeyondRangeKey     = "dashboard.late_cycle.beyond_range"
	LateCycleWithinRangeKey     = "dashboard.late_cycle.within_range"
	LateCycleNoPersonalRangeKey = "dashboard.late_cycle.no_personal_range"
)

// Late-cycle tones. Only a cycle measurably past the owner's own range earns
// the amber warning treatment; the two states that report "no fact to worry
// about yet" render muted, because amber on a reassurance reads as a verdict.
const (
	LateCycleToneWarning = "warning"
	LateCycleToneNeutral = "neutral"
)

// Late-cycle message forms, telling the template how to translate MessageKey.
// LateCycleFormCount selects the CLDR plural category from the day COUNT;
// LateCycleFormRange selects it from the range's UPPER bound (the «до N дней»
// rule) and substitutes both bounds; LateCycleFormPlain takes no parameter.
const (
	LateCycleFormPlain = "plain"
	LateCycleFormCount = "count"
	LateCycleFormRange = "range"
)

// LateCycleNotice is the single late-cycle message the dashboard renders, chosen
// by BuildLateCycleNotice and exposed to Playwright as data-late-cycle-key.
type LateCycleNotice struct {
	Visible    bool
	MessageKey string
	Tone       string
	Form       string
	Days       int
	RangeLow   int
	RangeHigh  int
}

// HasPersonalCycleRange reports whether the account has enough completed cycles
// for the product to speak of a personal cycle range at all.
//
// This is the SAME signal the stats page's "Prediction reliability … based on N
// completed cycles" card is gated on — buildStatsPredictionReliability calls
// this function — so the dashboard can never claim a usual range while the
// stats page still says the pattern is being built. Do not add a second
// threshold for late-cycle copy: the two would drift.
func HasPersonalCycleRange(user *models.User, completedCycleCount int) bool {
	return completedCycleCount >= statsMinimumInsightsCycles && !DashboardPredictionDisabled(user)
}

// BuildLateCycleNotice selects the late-cycle message for a cycle the dashboard
// already considers long (cycleDayLooksLong comes from
// DashboardCycleDayLooksLong, whose threshold this function deliberately does
// not widen). Unpredictable mode and a paused pregnancy never reach here: both
// return early from BuildDashboardCycleContext with the zero notice, so the
// recorded-facts-only surfaces stay silent.
func BuildLateCycleNotice(user *models.User, stats CycleStats, cycleDayLooksLong bool) LateCycleNotice {
	if !cycleDayLooksLong || stats.CurrentCycleDay <= 0 {
		return LateCycleNotice{}
	}

	if !HasPersonalCycleRange(user, stats.CompletedCycleCount) || stats.MaxCycleLength <= 0 {
		return LateCycleNotice{
			Visible:    true,
			MessageKey: LateCycleNoPersonalRangeKey,
			Tone:       LateCycleToneNeutral,
			Form:       LateCycleFormPlain,
		}
	}

	excessDays := stats.CurrentCycleDay - stats.MaxCycleLength
	if excessDays <= 0 {
		return LateCycleNotice{
			Visible:    true,
			MessageKey: LateCycleWithinRangeKey,
			Tone:       LateCycleToneNeutral,
			Form:       LateCycleFormRange,
			RangeLow:   stats.MinCycleLength,
			RangeHigh:  stats.MaxCycleLength,
		}
	}

	return LateCycleNotice{
		Visible:    true,
		MessageKey: LateCycleBeyondRangeKey,
		Tone:       LateCycleToneWarning,
		Form:       LateCycleFormCount,
		Days:       excessDays,
	}
}
