package models

// LutealPhaseRecomputeRow is the read projection ListOwnerLutealPhaseRows
// returns: the three columns the one-shot boot recompute of the derived
// users.luteal_phase cache needs, and nothing else.
//
// ID names the row the pass may update, Timezone is what a request-free pass
// has instead of a browser (it resolves which calendar day the owner is on),
// and LutealPhase is the stored estimate the recomputed value is compared
// against — a row the recompute agrees with is never written.
//
// It is intentionally NOT models.User: LoadSettingsByID stays the single
// settings whitelist, and this projection is scoped to the recompute so the
// batch query never over-selects sensitive per-account columns.
type LutealPhaseRecomputeRow struct {
	ID          uint   `gorm:"column:id"`
	Timezone    string `gorm:"column:timezone"`
	LutealPhase int    `gorm:"column:luteal_phase"`
}
