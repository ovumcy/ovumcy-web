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

// TestMR3Stats_PrimaryKeyVariableRanges pins line 32 (!irregular &&
// DisplayNextPeriodUseRange). A regular owner with NextPeriod UseRange must
// yield "prediction.explainer.variable_ranges"; a regular owner with UseRange
// false must yield "". A negated mutant (`user.IrregularCycle`) flips both.
func TestMR3Stats_PrimaryKeyVariableRanges(t *testing.T) {
	user := mr3statsOwner() // IrregularCycle == false

	positive := DashboardCycleContext{DisplayNextPeriodUseRange: true}
	if got := predictionExplanationPrimaryKey(user, positive); got != "prediction.explainer.variable_ranges" {
		t.Fatalf("expected variable_ranges, got %q", got)
	}

	contrast := DashboardCycleContext{}
	if got := predictionExplanationPrimaryKey(user, contrast); got != "" {
		t.Fatalf("expected empty primary key when UseRange false, got %q", got)
	}
}
