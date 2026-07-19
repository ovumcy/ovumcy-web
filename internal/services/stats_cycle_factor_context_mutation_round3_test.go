package services

import "testing"

// --- classifyStatsCycleFactorComparison ------------------------------------

// TestMR3Stats_FactorComparisonShorterBoundary pins line 146
// (cycleLength <= baseline-delta) at the boundary. baseline 28, delta 2,
// cycleLength == 26 must classify as "shorter". Mutant `<` would yield
// "variable".
func TestMR3Stats_FactorComparisonShorterBoundary(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 28}
	cycleLength := 28 - statsCycleFactorComparisonDelta // 26

	if got := classifyStatsCycleFactorComparison(stats, cycleLength); got != "shorter" {
		t.Fatalf("expected shorter at cycleLength %d (baseline-delta), got %q", cycleLength, got)
	}
}

// TestMR3Stats_FactorComparisonShorterArithmetic pins line 146 arithmetic
// (baseline - delta). With baseline 28 and cycleLength == 28, the result must
// be "variable": a `+` mutant (baseline+delta = 30) would make 28 <= 30 true
// and misclassify it as "shorter".
func TestMR3Stats_FactorComparisonShorterArithmetic(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 28}

	if got := classifyStatsCycleFactorComparison(stats, 28); got != "variable" {
		t.Fatalf("expected variable at cycleLength == baseline (28), got %q", got)
	}
}

// TestMR3Stats_FactorComparisonLongerBoundary pins line 148
// (cycleLength >= baseline+delta) at the boundary. baseline 28, delta 2,
// cycleLength == 30 must classify as "longer". Mutant `>` would yield
// "variable".
func TestMR3Stats_FactorComparisonLongerBoundary(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 28}
	cycleLength := 28 + statsCycleFactorComparisonDelta // 30

	if got := classifyStatsCycleFactorComparison(stats, cycleLength); got != "longer" {
		t.Fatalf("expected longer at cycleLength %d (baseline+delta), got %q", cycleLength, got)
	}
}

// TestMR3Stats_FactorComparisonLongerArithmetic pins line 148 arithmetic
// (baseline + delta). With baseline 28, cycleLength == 28 must be "variable"
// (a `-` mutant of baseline+delta = 26 would make 28 >= 26 true and
// misclassify it as "longer"), and cycleLength == 30 must be "longer".
func TestMR3Stats_FactorComparisonLongerArithmetic(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 28}

	if got := classifyStatsCycleFactorComparison(stats, 28); got != "variable" {
		t.Fatalf("expected variable at cycleLength == baseline (28), got %q", got)
	}
	if got := classifyStatsCycleFactorComparison(stats, 30); got != "longer" {
		t.Fatalf("expected longer at cycleLength 30 (baseline+delta), got %q", got)
	}
}

// TestMR3Stats_FactorComparisonDegenerateBaseline pins both line 146 and 148
// `baseline > 0` guards. With no median and zero average, baseline resolves to
// 0; any cycleLength must classify as "variable". Without the guard, a very
// negative cycleLength would satisfy `<= baseline-delta` ("shorter") and a
// large cycleLength would satisfy `>= baseline+delta` ("longer").
func TestMR3Stats_FactorComparisonDegenerateBaseline(t *testing.T) {
	stats := CycleStats{MedianCycleLength: 0, AverageCycleLength: 0}

	if got := classifyStatsCycleFactorComparison(stats, -5); got != "variable" {
		t.Fatalf("expected variable for negative cycleLength with zero baseline, got %q", got)
	}
	if got := classifyStatsCycleFactorComparison(stats, 100); got != "variable" {
		t.Fatalf("expected variable for large cycleLength with zero baseline, got %q", got)
	}
}
