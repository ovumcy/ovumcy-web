package models

import "time"

// LutealPhaseRecomputeRow is the read projection ListOwnerLutealPhaseRows
// returns: the four columns the one-shot boot recompute of the derived
// users.luteal_phase cache needs, and nothing else.
//
// ID names the row the pass may update, Timezone is what a request-free pass
// has instead of a browser (it resolves which calendar day the owner is on),
// LutealPhase is the stored estimate the recomputed value is compared
// against — a row the recompute agrees with is never written — and
// LastPeriodStart is the owner's onboarding start, a cycle boundary of its own
// that the derivation reads exactly as a day save does.
//
// It is intentionally NOT models.User: LoadSettingsByID stays the single
// settings whitelist, and this projection is scoped to the recompute so the
// batch query never over-selects sensitive per-account columns.
type LutealPhaseRecomputeRow struct {
	ID              uint       `gorm:"column:id"`
	Timezone        string     `gorm:"column:timezone"`
	LutealPhase     int        `gorm:"column:luteal_phase"`
	LastPeriodStart *time.Time `gorm:"column:last_period_start"`
}
