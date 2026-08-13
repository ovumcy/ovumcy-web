package services

import "testing"

// --- predictionExplanationPrimaryKey ---------------------------------------

// TestMR3Stats_PrimaryKeyIrregularSparse pins line 28 (irregular &&
// (NeedsData...)). An irregular owner with NextPeriod/Ovulation NeedsData must
// yield "prediction.explainer.irregular_sparse"; an irregular owner with all
// four display flags false must yield "". A negated mutant flips both.
func TestMR3Stats_PrimaryKeyIrregularSparse(t *testing.T) {
	user := mr3statsOwner()
	user.IrregularCycle = true

	positive := DashboardCycleContext{
		DisplayNextPeriodNeedsData: true,
		DisplayOvulationNeedsData:  true,
	}
	if got := predictionExplanationPrimaryKey(user, positive); got != "prediction.explainer.irregular_sparse" {
		t.Fatalf("expected irregular_sparse, got %q", got)
	}

	contrast := DashboardCycleContext{}
	if got := predictionExplanationPrimaryKey(user, contrast); got != "" {
		t.Fatalf("expected empty primary key when no display flags set, got %q", got)
	}
}

// TestMR3Stats_PrimaryKeyIrregularRanges pins line 30 (irregular &&
// (UseRange...)). An irregular owner with NextPeriod UseRange (and no
// NeedsData) must yield "prediction.explainer.irregular_ranges"; an irregular
// owner with all flags false must yield "". A negated mutant flips both.
func TestMR3Stats_PrimaryKeyIrregularRanges(t *testing.T) {
	user := mr3statsOwner()
	user.IrregularCycle = true

	positive := DashboardCycleContext{DisplayNextPeriodUseRange: true}
	if got := predictionExplanationPrimaryKey(user, positive); got != "prediction.explainer.irregular_ranges" {
		t.Fatalf("expected irregular_ranges, got %q", got)
	}

	contrast := DashboardCycleContext{}
	if got := predictionExplanationPrimaryKey(user, contrast); got != "" {
		t.Fatalf("expected empty primary key when no display flags set, got %q", got)
	}
}

// TestMR3Stats_PrimaryKeyRegularRangeHasNoExplainer guards the irregular
// operand of the surviving range branch from the other side. The regular-owner
// branch that used to return "prediction.explainer.variable_ranges" is gone, so
// a mutant dropping `user.IrregularCycle` from that condition would hand the
// irregular explainer to a regular owner in range mode; this pins the empty
// key that only the intact condition produces.
func TestMR3Stats_PrimaryKeyRegularRangeHasNoExplainer(t *testing.T) {
	user := mr3statsOwner() // IrregularCycle == false

	inRange := DashboardCycleContext{DisplayNextPeriodUseRange: true, DisplayOvulationUseRange: true}
	if got := predictionExplanationPrimaryKey(user, inRange); got != "" {
		t.Fatalf("expected no explainer for a regular owner in range mode, got %q", got)
	}

	contrast := DashboardCycleContext{}
	if got := predictionExplanationPrimaryKey(user, contrast); got != "" {
		t.Fatalf("expected empty primary key when UseRange false, got %q", got)
	}
}
