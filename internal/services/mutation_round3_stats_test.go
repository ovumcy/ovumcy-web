package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mr3statsOwner returns a minimal owner user; callers tweak cycle flags.
func mr3statsOwner() *models.User {
	return &models.User{Role: models.RoleOwner}
}

// mr3statsRegularStats returns CycleStats whose length spread is regular
// (spread <= irregularCycleSpreadDays = 7), so IsIrregularCycleSpread == false.
func mr3statsRegularStats() CycleStats {
	return CycleStats{MinCycleLength: 28, MaxCycleLength: 30}
}

// mr3statsPhaseContext builds a completed-cycle phase context with a fixed
// UTC anchor, the given period length and ovulation day, and a cycle long
// enough that all probed day numbers fall strictly before NextStart.
func mr3statsPhaseContext(periodLength, ovulationDay int) completedCyclePhaseContext {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return completedCyclePhaseContext{
		Start:        start,
		NextStart:    start.AddDate(0, 0, 60),
		CycleLength:  60,
		PeriodLength: periodLength,
		OvulationDay: ovulationDay,
	}
}

// mr3statsDayForNumber returns the calendar day whose one-based cycle-day
// number (relative to ctx.Start) equals dayNumber.
func mr3statsDayForNumber(ctx completedCyclePhaseContext, dayNumber int) time.Time {
	return ctx.Start.AddDate(0, 0, dayNumber-1)
}

// --- buildStatsPredictionReliability ---------------------------------------

// TestMR3Stats_ReliabilityVariableAtThreeCycles pins line 337
// (variablePattern && sampleCount >= minimumPhaseInsightCycles). A variable
// owner with exactly minimumPhaseInsightCycles (3) completed cycles must yield
// "stats.reliability.variable". Mutant `> 3` would drop to "building".
func TestMR3Stats_ReliabilityVariableAtThreeCycles(t *testing.T) {
	user := mr3statsOwner()
	user.IrregularCycle = true
	flags := StatsFlags{CompletedCycleCount: minimumPhaseInsightCycles}

	_, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if labelKey != "stats.reliability.variable" {
		t.Fatalf("expected stats.reliability.variable at %d cycles, got %q", minimumPhaseInsightCycles, labelKey)
	}
}

// TestMR3Stats_ReliabilityStableAtWindow pins line 339
// (sampleCount >= cyclePredictionWindow). A non-variable owner with exactly
// cyclePredictionWindow (6) completed cycles must yield
// "stats.reliability.stable". Mutant `> 6` would drop to "building".
func TestMR3Stats_ReliabilityStableAtWindow(t *testing.T) {
	user := mr3statsOwner()
	flags := StatsFlags{CompletedCycleCount: cyclePredictionWindow}

	sampleCount, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if sampleCount != cyclePredictionWindow {
		t.Fatalf("expected sampleCount %d, got %d", cyclePredictionWindow, sampleCount)
	}
	if labelKey != "stats.reliability.stable" {
		t.Fatalf("expected stats.reliability.stable at %d cycles, got %q", cyclePredictionWindow, labelKey)
	}
}

// TestMR3Stats_ReliabilityBuildingAtThreeCycles pins line 341
// (sampleCount >= minimumPhaseInsightCycles). A non-variable owner with exactly
// minimumPhaseInsightCycles (3) completed cycles must yield
// "stats.reliability.building". Mutant `> 3` would stay "early".
func TestMR3Stats_ReliabilityBuildingAtThreeCycles(t *testing.T) {
	user := mr3statsOwner()
	flags := StatsFlags{CompletedCycleCount: minimumPhaseInsightCycles}

	_, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if labelKey != "stats.reliability.building" {
		t.Fatalf("expected stats.reliability.building at %d cycles, got %q", minimumPhaseInsightCycles, labelKey)
	}
}

// --- phaseForCompletedCycleDay ---------------------------------------------

// TestMR3Stats_PhaseLastMenstrualDay pins line 98 (dayNumber <= PeriodLength).
// The last menstrual day (dayNumber == PeriodLength) must classify as
// "menstrual". Mutant `<` would misclassify it as "follicular".
func TestMR3Stats_PhaseLastMenstrualDay(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)
	day := mr3statsDayForNumber(ctx, ctx.PeriodLength) // dayNumber == 5

	phase := phaseForCompletedCycleDay(day, ctx, time.UTC)

	if phase != "menstrual" {
		t.Fatalf("expected menstrual at day %d (PeriodLength), got %q", ctx.PeriodLength, phase)
	}
}

// TestMR3Stats_PhaseOvulationDay pins line 100 (dayNumber == OvulationDay).
// Day == OvulationDay must classify as "ovulation"; a contrast day (6, which
// is after PeriodLength and before OvulationDay) must classify as
// "follicular". Mutant `!=` flips both classifications.
func TestMR3Stats_PhaseOvulationDay(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)

	ovulationDay := mr3statsDayForNumber(ctx, ctx.OvulationDay) // dayNumber == 14
	if phase := phaseForCompletedCycleDay(ovulationDay, ctx, time.UTC); phase != "ovulation" {
		t.Fatalf("expected ovulation at day %d, got %q", ctx.OvulationDay, phase)
	}

	contrastDay := mr3statsDayForNumber(ctx, 6) // not ovulation, between menstrual and ovulation
	if phase := phaseForCompletedCycleDay(contrastDay, ctx, time.UTC); phase != "follicular" {
		t.Fatalf("expected follicular at day 6 (non-ovulation contrast), got %q", phase)
	}
}

// TestMR3Stats_PhaseFollicularLutealBoundary pins line 102
// (dayNumber < OvulationDay). The day immediately before ovulation must be
// "follicular"; the day immediately after must be "luteal". Mutant `<=` would
// make the post-ovulation day "follicular".
func TestMR3Stats_PhaseFollicularLutealBoundary(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)

	beforeOv := mr3statsDayForNumber(ctx, ctx.OvulationDay-1) // dayNumber == 13
	if phase := phaseForCompletedCycleDay(beforeOv, ctx, time.UTC); phase != "follicular" {
		t.Fatalf("expected follicular at day %d (ovulation-1), got %q", ctx.OvulationDay-1, phase)
	}

	afterOv := mr3statsDayForNumber(ctx, ctx.OvulationDay+1) // dayNumber == 15
	if phase := phaseForCompletedCycleDay(afterOv, ctx, time.UTC); phase != "luteal" {
		t.Fatalf("expected luteal at day %d (ovulation+1), got %q", ctx.OvulationDay+1, phase)
	}
}

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
