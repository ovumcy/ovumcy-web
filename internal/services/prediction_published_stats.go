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
		if projectedCyclePhase(stats.CurrentPhase) {
			stats.CurrentPhase = cyclePhaseUnknown
		}
	}
	if suppression.PredictionsSuppressed {
		stats.NextPeriodStart = time.Time{}
	}
	return stats, suppression
}

// projectedCyclePhase reports that a phase label's only source is the projected
// ovulation date — the field cleared above.
//
// resolveCyclePhase (cycles.go) answers "menstrual" from recorded period days
// and "unknown" when it cannot place an ovulation at all; the other three are
// today's position relative to stats.OvulationDate and say nothing else. So
// publishing one beside a cleared ovulation date states in a word the very claim
// the date was withheld for — a client reading "ovulation" learns the day it
// asked about, from a payload that answered null.
//
// It is the FERTILITY gate that clears these, the same one that clears the date
// they are read off — including in the zero-completed-cycle tier, where the
// projected ovulation day has only the onboarding cycle-length setting behind
// it. DashboardAwaitingFirstCycle's "the phase stays, it follows the recorded
// anchor" holds for menstrual, which does follow one; for the other three the
// anchor reaches them only through the projection this tier withholds, so
// keeping them published the withheld day in a word.
func projectedCyclePhase(phase string) bool {
	switch phase {
	case cyclePhaseFollicular, cyclePhaseOvulation, cyclePhaseLuteal:
		return true
	default:
		return false
	}
}
