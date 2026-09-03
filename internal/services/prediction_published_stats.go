package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// PublishedStats returns the CycleStats a surface may PUBLISH — the computed
// stats with every forward-looking value the display policy refuses cleared, so
// the data cannot outlive the decision — together with the verdict that cleared
// it.
//
// Suppression on these surfaces used to be a template obligation — one boolean
// beside the full CycleStats it was supposed to hide — and a template that
// forgets the boolean, a new partial, a JSON view or a debug dump of the struct
// then publishes a claim the product has decided it must not make. Cleared here,
// the same forgetful surface renders nothing instead of a suppressed estimate.
// That failure was not hypothetical: GET /api/v1/stats/overview serialized the
// domain struct whole, so every date /stats withheld left the instance as JSON.
//
// It serves EVERY surface that publishes a projection — /stats, the dashboard,
// the JSON API, the webhook reminder pass and the .ics feed — and stays one
// function rather than one per surface: a second implementation of the clearing
// rule is precisely how /stats and the dashboard came to disagree in the first
// place. The two egress passes recompute their dates from the anchors this
// function does not touch (LastPeriodStart, LutealPhase) and withhold on the
// verdict, so there the clearing is a floor under a later edit rather than a
// change of what they send today.
//
// The verdict is returned rather than re-resolved by the caller: a surface that
// asks the predicates a second time can be holding cleared stats by then, and
// FertilityProjectionSuppressed reads fields this function empties.
//
// CurrentPhase is NOT cleared, and that is a decision rather than an omission.
// Phase and fertility are orthogonal axes here (#416): the
// taxonomy is menstrual/follicular/ovulation/luteal/unknown and "fertile" is a
// status, never a phase — so a phase label is not the window-or-fertility claim
// the medical-safety invariant withholds. It also cannot be cleared HERE alone:
// the dashboard hero rebuilds its own phase from the cycle geometry
// (dashboardCycleHeroCurrentPhase) and the header prefers the hero's, so
// emptying the published copy splits one page across two answers. A client
// reading a phase beside a null ovulation date has the suppression object to
// tell it why the date is absent.
//
// RECORDED history — observed cycle lengths, the last period start, the current
// cycle day — is never touched: it is fact, not projection, and the "facts only"
// tier exists precisely to keep showing it. Only the PUBLISHED copy is cleared;
// every builder behind a page reads the full stats, because the ribbon, the
// factor context and the cycle context each apply their own suppression rule to
// it.
func PublishedStats(user *models.User, stats CycleStats) (CycleStats, PredictionSuppression) {
	// The verdict is read off the UNCLEARED stats, so the fertility gate cannot
	// be answered from fields the clearing below has already emptied.
	suppression := ResolvePredictionSuppression(user, stats)

	// The two predicates clear different sets because they answer different
	// questions: FertilityProjectionSuppressed also covers the zero-cycles floor,
	// where the projected next period legitimately stays because its anchor is a
	// recorded start and only the length falls back.
	if suppression.FertilitySuppressed {
		stats.OvulationDate = time.Time{}
		stats.OvulationExact = false
		stats.FertilityWindowStart = time.Time{}
		stats.FertilityWindowEnd = time.Time{}
		stats.CurrentFertility = FertilityStatusUnknown
	}
	if suppression.PredictionsSuppressed {
		stats.NextPeriodStart = time.Time{}
	}
	return stats, suppression
}

// PublishedOverviewStats is PublishedStats plus the confirmed-ovulation
// substitution the on-screen surfaces already apply: the calendar's solid
// marker (calendar_days.go), the dashboard's ovulation line
// (dashboard_cycle.go) and the stats chart marker all resolve the CURRENT
// cycle's ovulation day through ConfirmedCurrentCycleOvulation, so a shift
// the owner's own temperatures already confirm outranks the model's
// projection everywhere an owner reads it.
//
// The JSON API was the one surface that skipped this: it read stats.
// OvulationDate straight off the model and published it even after a BBT
// shift had superseded it, while the grid, the dashboard and the chart had
// already moved on to the measured day. The substitution runs on the RAW
// stats, ahead of PublishedStats' own clearing, for the same reason the
// dashboard's runs ahead of its suppression branches: ConfirmedCurrentCycleOvulation
// already reads FertilityProjectionSuppressed itself, so a confirmed day
// never overrides a projection the gate would have withheld anyway, and
// running it first means PublishedStats sees the same OvulationDate every
// other surface renders.
//
// A confirmed day is a MEASUREMENT, not a less-certain reading of the model:
// OvulationExact is set true along with it, matching what the calendar's
// solid marker and the dashboard's line already mean by never showing the
// "approximate" caption beside a confirmed date (dashboard.html).
func PublishedOverviewStats(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) (CycleStats, PredictionSuppression) {
	if confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location); ok {
		stats.OvulationDate = confirmed
		stats.OvulationExact = true
	}
	return PublishedStats(user, stats)
}
