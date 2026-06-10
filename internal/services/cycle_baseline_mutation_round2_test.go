package services

// cycle_baseline_mutation_round2_test.go
// Round-2 mutation-survivor kill-tests for cycle_baseline.go.
//
// Each test drives ApplyUserCycleBaseline with a caller-supplied CycleStats
// (the function does NOT recompute stats from logs) so that a boundary mutant
// on one of the cycle-length guards becomes observable on a RETURNED decision
// value (NextPeriodStart / MedianCycleLength / OvulationImpossible) — never on
// logs, markup, or error text.
//
// Reused existing package helpers (NOT redefined here):
//   - mustParseBaselineDay   (cycle_baseline_test.go)
//   - cyclebaselineCovOwner  (cycle_baseline_coverage_test.go)
// No new helpers are introduced.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ── Line 51: applyObservedBaseline `cycleLength > 0` (CONDITIONALS_BOUNDARY) ──
//
// In the !hasObservedCycleLengths branch, cycleLength is 0 or in [15,90]
// (IsValidOnboardingCycleLength). `>` vs `>=` differ ONLY at cycleLength==0.
// Original SKIPS the block (leaves the incoming Median/Average intact); the
// `>= 0` mutant WRITES MedianCycleLength=0 / AverageCycleLength=0. The incoming
// median drives the projected NextPeriodStart via predictedCycleLength, so the
// clobber is observable as a different returned date.
func TestApplyUserCycleBaseline_NoHistoryDoesNotClobberProvidedMedianWhenUserCycleLengthInvalid(t *testing.T) {
	// cycleLength==0 (CycleLength invalid) + !hasObservedCycleLengths is the only
	// input that distinguishes line 51 `cycleLength > 0` from the `>= 0` mutant.
	// The original PRESERVES the incoming MedianCycleLength (drives NextPeriodStart);
	// the mutant overwrites it to 0 -> prediction falls back to DefaultCycleLength=28.
	lp := mustParseBaselineDay(t, "2026-03-01")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     0, // invalid per IsValidOnboardingCycleLength -> cycleLength=0 -> branch runs
		PeriodLength:    5,
		LastPeriodStart: &lp, // anchors a non-zero stats.LastPeriodStart so projection runs
	}
	// Single period day: DetectCycleStarts yields 1 start => CycleLengths()==nil
	// => hasObservedCycleLengths==false (line 50 branch executes).
	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-03-01"), IsPeriod: true, Flow: models.FlowMedium},
	}
	now := mustParseBaselineDay(t, "2026-03-10")
	// Hand-built stats with a non-zero remembered median (same pattern the file
	// already uses for DetectCurrentPhase tests). Average=0 so predictedCycleLength
	// uses the median.
	stats := CycleStats{
		LastPeriodStart:    lp,
		MedianCycleLength:  40,
		AverageCycleLength: 0,
	}
	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	// ORIGINAL: median 40 preserved -> predictionCycleLength=40 -> 2026-03-01 + 40d.
	// MUTANT:   median clobbered to 0 -> predictionCycleLength=DefaultCycleLength(28) -> 2026-03-29.
	const want = "2026-04-10"
	if g := got.NextPeriodStart.Format("2006-01-02"); g != want {
		t.Fatalf("NextPeriodStart = %s, want %s (mutant clobbers provided median to 0 -> 2026-03-29)", g, want)
	}
	// Direct guard on the field the mutation touches.
	if got.MedianCycleLength != 40 {
		t.Fatalf("MedianCycleLength = %d, want 40 (provided median must not be overwritten when cycleLength==0)", got.MedianCycleLength)
	}
}

// ── Line 75: applyProjectedBaseline `predictionCycleLength <= 0` (BOUNDARY) ──
//
// `<= 0` vs `< 0` diverge ONLY when predictedCycleLength returns exactly 0.
// A caller-supplied AverageCycleLength in (0, 0.5) makes predictedCycleLength
// take its `average > 0` branch and return int(0.3+0.5)=0. With a valid
// user.CycleLength=28, the line-76 fallback rescues the original (length 28),
// so line 75 alone decides whether projection runs.
//   - original (<=0): predictionCycleLength becomes 28 -> NextPeriodStart set.
//   - mutant   (<0):  predictionCycleLength stays 0 -> line 78 early-returns -> zero.
func TestCyclebaselineCov_FractionalAverageTriggersCycleLengthFallback(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-02-01")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28, // valid (15..90) -> resolveUserCycleLengths -> cycleLength=28
		PeriodLength:    5,
		LastPeriodStart: &lp, // anchor -> stats.LastPeriodStart resolves non-zero (line 70 guard passes)
	}
	// Two explicit period-start days => CycleLengths(logs) has length >= 1
	// => hasObservedCycleLengths == true => applyObservedBaseline keeps the
	// injected AverageCycleLength (does not overwrite it).
	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
	}
	now := mustParseBaselineDay(t, "2026-02-10")

	// Inject a fractional average in (0, 0.5): predictedCycleLength returns
	// int(0.3+0.5) = 0, so predictionCycleLength == 0 at line 75.
	stats := CycleStats{
		AverageCycleLength: 0.3,
		MedianCycleLength:  0, // irrelevant: average>0 branch short-circuits
		LastPeriodStart:    lp,
	}

	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if got.LastPeriodStart.IsZero() {
		t.Fatalf("precondition failed: expected non-zero LastPeriodStart so projection is reached")
	}
	// Decisive assertion: original applies the cycleLength=28 fallback and
	// projects; mutant early-returns leaving NextPeriodStart zero.
	if got.NextPeriodStart.IsZero() {
		t.Fatalf("expected NextPeriodStart to be set via cycleLength fallback when predictionCycleLength==0; got zero (mutation <=0 -> <0 survived)")
	}
	// Tighten: fallback length is 28, so NextPeriodStart == LastPeriodStart + 28d.
	want := got.LastPeriodStart.AddDate(0, 0, 28).Format("2006-01-02")
	if g := got.NextPeriodStart.Format("2006-01-02"); g != want {
		t.Fatalf("expected NextPeriodStart=%s (LastPeriodStart + 28d fallback), got %s", want, g)
	}
}

// ── Line 78: applyProjectedBaseline `predictionCycleLength <= 0` (BOUNDARY) ──
//
// `<= 0` vs `< 0` diverge ONLY when predictionCycleLength == 0. A fractional
// caller-supplied average (0.4 -> int(0.9)=0) plus an invalid user.CycleLength
// (0 -> cycleLength=0, so the line-76 fallback keeps 0) reaches line 78 with 0.
//   - original (<=0): returns at line 79 -> NextPeriodStart zero, OvulationImpossible false.
//   - mutant   (<0):  falls through -> NextPeriodStart = LastPeriodStart + 0d (non-zero),
//     then PredictCycleWindow(.,0,.) is not calculable -> clearPredictedCycleWindow
//     sets OvulationImpossible = true.
func TestCyclebaselineCov_NoProjectionWhenResolvedCycleLengthIsZero(t *testing.T) {
	lp := mustParseBaselineDay(t, "2026-03-01")
	// CycleLength 0 is invalid (IsValidOnboardingCycleLength requires >=15),
	// so resolveUserCycleLengths returns cycleLength = 0 -> line 76 keeps
	// predictionCycleLength at 0.
	user := cyclebaselineCovOwner(0, 5, 0, &lp)

	// No logs: hasObservedCycleLengths is false, but applyObservedBaseline
	// will NOT overwrite the crafted average because cycleLength == 0.
	logs := []models.DailyLog{}

	// Craft stats directly (same convention as the DetectCurrentPhase tests
	// in this file). AverageCycleLength = 0.4 -> predictedCycleLength takes
	// the `average > 0` branch and returns int(0.4+0.5) = int(0.9) = 0.
	// MedianCycleLength = 0 so no fallback rescues it.
	stats := CycleStats{
		AverageCycleLength: 0.4,
		MedianCycleLength:  0,
	}
	now := mustParseBaselineDay(t, "2026-03-10")

	got := ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	// Anchor must be set so the line-70 guard passes and we actually reach
	// line 78 (sanity precondition, not the kill assertion).
	if got.LastPeriodStart.IsZero() {
		t.Fatalf("precondition: expected non-zero LastPeriodStart anchor")
	}

	// KILL ASSERTION: with no usable resolved cycle length, the original
	// `<= 0` guard returns before projecting, leaving NextPeriodStart zero.
	// The `< 0` mutant falls through and sets NextPeriodStart =
	// LastPeriodStart.AddDate(0,0,0) == LastPeriodStart (a nonsensical
	// 0-day "next period").
	if !got.NextPeriodStart.IsZero() {
		t.Fatalf("expected NextPeriodStart to stay zero when no cycle length resolves; got %s (mutant projects a 0-day cycle)",
			got.NextPeriodStart.Format("2006-01-02"))
	}

	// Secondary, independent kill signal: the mutant runs
	// clearPredictedCycleWindow (cycleLength 0 < 15 => not calculable),
	// flipping OvulationImpossible to true; the original never gets there.
	if got.OvulationImpossible {
		t.Fatalf("expected OvulationImpossible=false (guard returned early); mutant set it true via clearPredictedCycleWindow")
	}
}
