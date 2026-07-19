package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// cyclesignalsCovDay parses a "YYYY-MM-DD" string into a UTC midnight time.Time.
// Prefixed to avoid collision with mustParseDay/mustParseBaselineDay already in other test files.
func cyclesignalsCovDay(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		t.Fatalf("cyclesignalsCovDay: parse %q: %v", s, err)
	}
	return v
}

// cyclesignalsCovPeriodLog returns a DailyLog marked as period with the given date.
func cyclesignalsCovPeriodLog(t *testing.T, date string) models.DailyLog {
	t.Helper()
	return models.DailyLog{Date: cyclesignalsCovDay(t, date), IsPeriod: true, Flow: models.FlowMedium}
}

// cyclesignalsCovBBTLog returns a DailyLog with a BBT reading (no period).
func cyclesignalsCovBBTLog(t *testing.T, date string, bbt float64) models.DailyLog {
	t.Helper()
	return models.DailyLog{Date: cyclesignalsCovDay(t, date), BBT: models.NewBBT(bbt)}
}

// cyclesignalsCovMucusLog returns a DailyLog with cervical mucus set (no period).
func cyclesignalsCovMucusLog(t *testing.T, date string, mucus string) models.DailyLog {
	t.Helper()
	return models.DailyLog{Date: cyclesignalsCovDay(t, date), CervicalMucus: mucus}
}

// ---------------------------------------------------------------------------
// Line 18 — nil location fallback: InferUserLutealPhase must not panic and
// must produce results identical to passing time.UTC explicitly.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_NilLocationDoesNotPanic(t *testing.T) {
	// Build enough period logs to form >= 3 observed cycle starts and
	// enough BBT readings to detect ovulation in each cycle.
	logs := cyclesignalsCovBuildLutealLogs(t)

	phaseNil, okNil := InferUserLutealPhase(logs, nil)
	phaseUTC, okUTC := InferUserLutealPhase(logs, time.UTC)

	// Both calls must agree — nil location must behave like time.UTC.
	if okNil != okUTC {
		t.Fatalf("nil location ok=%v differs from UTC ok=%v", okNil, okUTC)
	}
	if phaseNil != phaseUTC {
		t.Fatalf("nil location phase=%d differs from UTC phase=%d", phaseNil, phaseUTC)
	}
}

// ---------------------------------------------------------------------------
// Line 23 — guard: fewer than 3 observed starts must return the default.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_FewerThanThreeStartsReturnsDefault(t *testing.T) {
	// Only 2 period clusters → 2 observed starts → must return default, false.
	logs := []models.DailyLog{
		cyclesignalsCovPeriodLog(t, "2025-01-01"),
		cyclesignalsCovPeriodLog(t, "2025-01-02"),
		cyclesignalsCovPeriodLog(t, "2025-01-29"),
		cyclesignalsCovPeriodLog(t, "2025-01-30"),
	}
	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if ok {
		t.Fatalf("expected ok=false with only 2 observed starts, got ok=true phase=%d", phase)
	}
	if phase != defaultLutealPhaseDays {
		t.Fatalf("expected default luteal phase %d, got %d", defaultLutealPhaseDays, phase)
	}
}

func TestCycleSignals_InferUserLutealPhase_ExactlyThreeStartsIsAccepted(t *testing.T) {
	// Exactly 3 observed starts is the minimum the >=3 guard accepts — the
	// positive complement of ...FewerThanThreeStartsReturnsDefault. The fixture
	// builds 3 starts with two BBT-confirmed 14-day luteal phases.
	// Cycles: Jan1→Jan29 (28 days), Jan29→Feb26 (28 days).
	logs := cyclesignalsCovBuildLutealLogs(t)

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatal("expected ok=true: exactly 3 observed starts must be accepted")
	}
	if phase != 14 {
		t.Fatalf("expected inferred luteal phase 14, got %d", phase)
	}
}

// ---------------------------------------------------------------------------
// Line 28 — loop bound: the last pair of starts must be processed.
// Verify by building exactly 3 starts where only the last pair yields a
// valid ovulation date via BBT, and checking that the result is influenced
// by that last pair.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_LastStartPairIsIncluded(t *testing.T) {
	// Three starts: Jan1, Jan29, Feb26.
	// First pair (Jan1→Jan29): add BBT readings for ovulation ~Jan15.
	// Second pair (Jan29→Feb26): add BBT readings for ovulation ~Feb12.
	// Both pairs are processed; if the loop stopped one short, the second
	// pair would be missing.
	logs := cyclesignalsCovBuildLutealLogs(t)

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true with two valid BBT-detected cycles")
	}
	// phase must be a rounded average in [minLutealPhaseDays, 20].
	if phase < minLutealPhaseDays || phase > 20 {
		t.Fatalf("inferred luteal phase %d out of valid range [%d,20]", phase, minLutealPhaseDays)
	}
}

// ---------------------------------------------------------------------------
// Lines 36–37 (NOT COVERED) — luteal length computation and range filter.
// A cycle whose BBT-computed luteal length falls outside [minLuteal, 20]
// must be silently skipped, and a cycle with a valid length must be counted.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_OutOfRangeLutealLengthIsSkipped(t *testing.T) {
	// Three starts: Jan1, Jan29, Feb26 (28-day cycles).
	// Cycle 1 (Jan1→Jan29): first high Jan16 → ovulation Jan15 → luteal = 14 (valid).
	// Cycle 2 (Jan29→Feb26): first high Feb23 → ovulation Feb22 → luteal = 4
	// (< minLutealPhaseDays → skipped).
	//
	// With only 1 valid luteal length (< 2), must return default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Period clusters.
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — 6-day coverline window then rise Jan16-18.
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-18"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — 6-day coverline window then rise Feb23-25.
		// ovulation = Feb23−1 = Feb22 → luteal = Feb26 − Feb22 = 4 → skipped.
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-23"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-24"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-25"), BBT: models.NewBBT(36.50)},
	}

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	// Only 1 valid luteal length → returns default, false.
	if ok {
		t.Fatalf("expected ok=false when only one cycle has a valid luteal length, got phase=%d", phase)
	}
	if phase != defaultLutealPhaseDays {
		t.Fatalf("expected default luteal phase %d, got %d", defaultLutealPhaseDays, phase)
	}
}

func TestCycleSignals_InferUserLutealPhase_LutealLengthOverTwentyIsSkipped(t *testing.T) {
	// Cycle where ovulation is very early → luteal > 20 → skipped.
	// Three starts: Jan1, Jan29, Feb26.
	// Cycle 1: first high Jan8 → ovulation Jan7 → luteal = Jan29−Jan7 = 22 (> 20 → skipped).
	// Cycle 2: first high Feb13 → ovulation Feb12 → luteal = 14 (valid).
	// Only 1 valid → default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — coverline window Jan1-6 then rise Jan8-10
		// (ovulation = Jan8−1 = Jan7; luteal = Jan29−Jan7 = 22 > 20).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-08"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-09"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-10"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — valid: rise Feb13-15 → ovulation Feb12 → luteal = 14.
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-15"), BBT: models.NewBBT(36.50)},
	}

	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if ok {
		t.Fatalf("expected ok=false (only one valid cycle), got phase=%d", phase)
	}
	if phase != defaultLutealPhaseDays {
		t.Fatalf("expected default %d, got %d", defaultLutealPhaseDays, phase)
	}
}

// ---------------------------------------------------------------------------
// Line 43 — guard: fewer than 2 valid luteal lengths returns default.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_FewerThanTwoValidLutealLengthsReturnsDefault(t *testing.T) {
	// Three observed starts but no BBT data → no ovulation dates found →
	// no luteal lengths accumulated → returns default, false.
	logs := []models.DailyLog{
		cyclesignalsCovPeriodLog(t, "2025-01-01"),
		cyclesignalsCovPeriodLog(t, "2025-01-29"),
		cyclesignalsCovPeriodLog(t, "2025-02-26"),
	}
	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if ok {
		t.Fatalf("expected ok=false with no BBT data, got phase=%d", phase)
	}
	if phase != defaultLutealPhaseDays {
		t.Fatalf("expected default %d, got %d", defaultLutealPhaseDays, phase)
	}
}

// ---------------------------------------------------------------------------
// Line 43 — guard positive side: exactly 2 valid luteal lengths must succeed.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_TwoValidLutealLengthsProducesResult(t *testing.T) {
	logs := cyclesignalsCovBuildLutealLogs(t)
	_, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true with two valid BBT-inferred cycles")
	}
}

// ---------------------------------------------------------------------------
// Detector minimum data: without 6 undisturbed recorded temperatures before
// the candidate first high day, no shift can be detected.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_FewerThanSixWindowPointsReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	// Only 5 low readings before the rise → the 6-value coverline window
	// never fills, so even a clear 3-day rise must not be detected.
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-15", 36.5),
		cyclesignalsCovBBTLog(t, "2025-01-16", 36.5),
		cyclesignalsCovBBTLog(t, "2025-01-17", 36.5),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected zero date with only 5 window points, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferBBTOvulationDate_FlatSeriesReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	// 9 points all at the same temperature → nothing above the coverline.
	logs := make([]models.DailyLog, 0, 9)
	for dayNumber := 1; dayNumber <= 9; dayNumber++ {
		logs = append(logs, cyclesignalsCovBBTLog(t, time.Date(2025, 1, dayNumber, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), 36.2))
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected zero date when no rise exists, got %s", result.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Threshold boundaries: days 1-2 strictly above the coverline; the third day
// exactly at coverline+0.2 counts (>=), just below does not.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_ThirdDayExactlyAtMarginCountsAsShift(t *testing.T) {
	// Coverline window at 36.00 (integer-valued so 36.00+0.2 = 36.20 is exact
	// in float64). Days 1-2 of the rise are above the coverline but below the
	// margin; the third is exactly coverline+0.2 → shift confirmed.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	low := 36.00
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", low),
		cyclesignalsCovBBTLog(t, "2025-01-02", low),
		cyclesignalsCovBBTLog(t, "2025-01-03", low),
		cyclesignalsCovBBTLog(t, "2025-01-04", low),
		cyclesignalsCovBBTLog(t, "2025-01-05", low),
		cyclesignalsCovBBTLog(t, "2025-01-06", low),
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.10),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.10),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.20), // exactly coverline+0.2
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if result.IsZero() {
		t.Fatalf("expected shift when third day is exactly at coverline+0.2 (>= must include equality)")
	}
	// First high day = Jan07 → ovulation = day before = Jan06.
	if got := result.Format("2006-01-02"); got != "2025-01-06" {
		t.Fatalf("expected ovulation on 2025-01-06, got %s", got)
	}
}

func TestCycleSignals_InferBBTOvulationDate_ThirdDayBelowMarginReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	low := 36.00
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", low),
		cyclesignalsCovBBTLog(t, "2025-01-02", low),
		cyclesignalsCovBBTLog(t, "2025-01-03", low),
		cyclesignalsCovBBTLog(t, "2025-01-04", low),
		cyclesignalsCovBBTLog(t, "2025-01-05", low),
		cyclesignalsCovBBTLog(t, "2025-01-06", low),
		// All three above the coverline, but the third stays below +0.2.
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.10),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.10),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.15),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no shift when the third day is below coverline+0.2, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferBBTOvulationDate_FirstDayAtCoverlineIsNotElevated(t *testing.T) {
	// A first-streak day exactly EQUAL to the coverline is not elevated
	// (strictly above required) — the max-of-6 semantics would otherwise let
	// ordinary follicular noise through.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	low := 36.00
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", low),
		cyclesignalsCovBBTLog(t, "2025-01-02", low),
		cyclesignalsCovBBTLog(t, "2025-01-03", low),
		cyclesignalsCovBBTLog(t, "2025-01-04", low),
		cyclesignalsCovBBTLog(t, "2025-01-05", low),
		cyclesignalsCovBBTLog(t, "2025-01-06", low),
		cyclesignalsCovBBTLog(t, "2025-01-07", low), // = coverline → not elevated
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.30),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.30),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no shift when the candidate first day equals the coverline, got %s", result.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Coverline = MAX of the window, calendar adjacency, and disturbance
// exclusion (#249 acceptance).
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_CoverlineIsMaxOfWindowNotMean(t *testing.T) {
	// One warm-but-undisturbed day (36.45) inside the window lifts the MAX
	// coverline above the rise values → no detection. A mean-based baseline
	// ((5·36.0+36.45)/6 ≈ 36.08, threshold ≈ 36.28) would wrongly detect it.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.45), // window max
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.40),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.40),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.40),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no shift when the window max exceeds the rise values, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferBBTOvulationDate_IllnessDayExcludedFromCoverline(t *testing.T) {
	// Same series as ...CoverlineIsMaxOfWindowNotMean, but the warm day is
	// tagged illness → it is excluded, the coverline drops back to 36.00, and
	// the genuine shift is detected (#249: the false-negative case).
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	feverDay := cyclesignalsCovBBTLog(t, "2025-01-03", 36.45)
	feverDay.CycleFactorKeys = []string{models.CycleFactorIllness}

	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.00),
		feverDay,
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.00), // 6th undisturbed window day
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.40),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.40),
		cyclesignalsCovBBTLog(t, "2025-01-10", 36.40),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if result.IsZero() {
		t.Fatalf("expected shift detected once the illness day is excluded from the coverline window")
	}
	// First high day = Jan08 → ovulation = Jan07.
	if got := result.Format("2006-01-02"); got != "2025-01-07" {
		t.Fatalf("expected ovulation on 2025-01-07, got %s", got)
	}
}

func TestCycleSignals_InferBBTOvulationDate_SleepDisruptedElevatedDayCannotConfirmShift(t *testing.T) {
	// The would-be second elevated day is tagged sleep_disruption → excluded.
	// The remaining elevated days are no longer calendar-consecutive, so the
	// shift is NOT confirmed: a disturbed reading must never confirm ovulation.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	disturbed := cyclesignalsCovBBTLog(t, "2025-01-08", 36.45)
	disturbed.CycleFactorKeys = []string{models.CycleFactorSleepDisruption}

	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.45),
		disturbed,
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.45),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no shift when a disturbed day breaks the elevated streak, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferBBTOvulationDate_NonConsecutiveElevatedDaysReturnZero(t *testing.T) {
	// Three elevated recorded points with a calendar gap (Jan08, Jan09, Jan11)
	// must not count as a 3-day shift.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.00),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.45),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.45),
		cyclesignalsCovBBTLog(t, "2025-01-11", 36.45),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no shift for non-consecutive elevated days, got %s", result.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Regression: the stats chart marker and luteal inference must agree on the
// ovulation day — both route through detectBBTShiftFirstHighDay.
// ---------------------------------------------------------------------------

func TestCycleSignals_BBTChartMarkerAndInferenceAgreeOnOvulationDay(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	today := cyclesignalsCovDay(t, "2025-01-20")

	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.25),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.30),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.22),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.24),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.21),
		cyclesignalsCovBBTLog(t, "2025-01-10", 36.55),
		cyclesignalsCovBBTLog(t, "2025-01-11", 36.60),
		cyclesignalsCovBBTLog(t, "2025-01-12", 36.65),
	}

	// Inference side: ovulation via the shared detector.
	ovulation := inferBBTOvulationDate(logs, cycleStart, today.AddDate(0, 0, 1), time.UTC)
	if ovulation.IsZero() {
		t.Fatalf("expected inference to detect ovulation")
	}

	// Chart side: the marker built from the same logs.
	stats := CycleStats{LastPeriodStart: cycleStart}
	chart := buildCurrentCycleBBTChart(stats, logs, today, time.UTC)
	if !chart.HasMarker {
		t.Fatalf("expected chart marker for the same logs")
	}

	// MarkerIndex is 0-based over cycle days: marker day = MarkerIndex+1.
	markerCycleDay := chart.MarkerIndex + 1
	ovulationCycleDay := CalendarDaysBetween(cycleStart, ovulation) + 1
	if markerCycleDay != ovulationCycleDay {
		t.Fatalf("marker day %d disagrees with inferred ovulation day %d", markerCycleDay, ovulationCycleDay)
	}
}

// ---------------------------------------------------------------------------
// Line 86 — BBT=0 must be excluded: a log with BBT=0 is not a valid reading.
// ---------------------------------------------------------------------------

func TestCycleSignals_CollectCycleBBTPoints_ZeroBBTIsExcluded(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		{Date: cyclesignalsCovDay(t, "2025-01-02"), BBT: models.NewBBT(0.0)},  // must be excluded
		{Date: cyclesignalsCovDay(t, "2025-01-03"), BBT: models.NewBBT(36.5)}, // valid
	}

	points := collectCycleBBTPoints(logs, cycleStart, nextStart, time.UTC)
	if len(points) != 1 {
		t.Fatalf("expected 1 BBT point (zero excluded), got %d", len(points))
	}
	if points[0].Value != 36.5 {
		t.Fatalf("expected BBT value 36.5, got %f", points[0].Value)
	}
}

// ---------------------------------------------------------------------------
// Line 97 — CycleDay must be one-based: day == cycleStart must give CycleDay=1.
// ---------------------------------------------------------------------------

func TestCycleSignals_CollectCycleBBTPoints_CycleDayIsOneBased(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	// A log on the same day as cycleStart must have CycleDay == 1.
	logs := []models.DailyLog{
		{Date: cyclesignalsCovDay(t, "2025-01-01"), BBT: models.NewBBT(36.2)},
	}

	points := collectCycleBBTPoints(logs, cycleStart, nextStart, time.UTC)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	if points[0].CycleDay != 1 {
		t.Fatalf("expected cycle day 1 for log on cycleStart, got %d", points[0].CycleDay)
	}

	// Also verify the second day is cycle day 2.
	logs2 := []models.DailyLog{
		{Date: cyclesignalsCovDay(t, "2025-01-01"), BBT: models.NewBBT(36.2)},
		{Date: cyclesignalsCovDay(t, "2025-01-02"), BBT: models.NewBBT(36.2)},
	}
	points2 := collectCycleBBTPoints(logs2, cycleStart, nextStart, time.UTC)
	if len(points2) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points2))
	}
	if points2[1].CycleDay != 2 {
		t.Fatalf("expected cycle day 2 for day after cycleStart, got %d", points2[1].CycleDay)
	}
}

// ---------------------------------------------------------------------------
// Line 115 — egg-white filter: only CervicalMucusEggWhite observations must
// update the ovulation date; other mucus types must be ignored.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferEggWhiteOvulationDate_OnlyEggWhiteIsAccepted(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		// creamy on Jan10 must NOT count
		cyclesignalsCovMucusLog(t, "2025-01-10", models.CervicalMucusCreamy),
		// egg-white on Jan15 must count
		cyclesignalsCovMucusLog(t, "2025-01-15", models.CervicalMucusEggWhite),
		// moist on Jan20 must NOT count
		cyclesignalsCovMucusLog(t, "2025-01-20", models.CervicalMucusMoist),
	}

	result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if result.IsZero() {
		t.Fatalf("expected non-zero ovulation date from egg-white observation")
	}
	// Peak-day rule: last egg-white Jan15 → ovulation estimated the day after = Jan16.
	if got := result.Format("2006-01-02"); got != "2025-01-16" {
		t.Fatalf("expected ovulation on 2025-01-16 (day after last egg-white), got %s", got)
	}
}

func TestCycleSignals_InferEggWhiteOvulationDate_NoEggWhiteReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		cyclesignalsCovMucusLog(t, "2025-01-10", models.CervicalMucusCreamy),
		cyclesignalsCovMucusLog(t, "2025-01-15", models.CervicalMucusMoist),
	}

	result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected zero date with no egg-white observations, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferEggWhiteOvulationDate_ReturnsDayAfterLastEggWhiteInCycleWindow(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		// Both are egg-white; the LAST one in the window is the peak day.
		cyclesignalsCovMucusLog(t, "2025-01-12", models.CervicalMucusEggWhite),
		cyclesignalsCovMucusLog(t, "2025-01-14", models.CervicalMucusEggWhite),
		// Outside window — must be excluded.
		cyclesignalsCovMucusLog(t, "2025-01-30", models.CervicalMucusEggWhite),
	}

	result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, time.UTC)
	// Peak day = last in-window egg-white Jan14 → ovulation the day after = Jan15.
	if got := result.Format("2006-01-02"); got != "2025-01-15" {
		t.Fatalf("expected ovulation on 2025-01-15 (day after last in-window egg-white), got %s", got)
	}
}

func TestCycleSignals_InferEggWhiteOvulationDate_PeakOnLastCycleDayClampsToPeak(t *testing.T) {
	// When the peak (last egg-white) falls on the final cycle day, peak+1 would
	// reach the next cycle start; the clamp keeps the peak day itself so the
	// ovulation estimate never lands on or past the next cycle.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		// Jan28 is the last day before nextStart (Jan29); peak+1 = Jan29 = nextStart.
		cyclesignalsCovMucusLog(t, "2025-01-28", models.CervicalMucusEggWhite),
	}

	result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if got := result.Format("2006-01-02"); got != "2025-01-28" {
		t.Fatalf("expected ovulation clamped to peak day 2025-01-28, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Full integration: InferUserLutealPhase with valid BBT returns the right value.
// This exercises lines 36-37, 43, and 46.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferUserLutealPhase_CorrectValueFromBBT(t *testing.T) {
	// Two cycles with BBT-confirmed ovulation, each yielding luteal ~14 days.
	logs := cyclesignalsCovBuildLutealLogs(t)
	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// Both cycles have their BBT rise on day 16, so ovulation is day 15 of a
	// 28-day cycle (the day before the shift) → luteal = 14.
	if phase != 14 {
		t.Fatalf("expected inferred luteal phase 14, got %d", phase)
	}
}

// ---------------------------------------------------------------------------
// Day-before convention: ovulation is the day BEFORE the sustained shift.
// (Calendar adjacency, disturbance exclusion, and marker/inference agreement
// are pinned by the dedicated tests above.)
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_OvulationIsDayBeforeSustainedShift(t *testing.T) {
	// Basal temperature rises after ovulation, so the estimate is the day before
	// the first of three consecutive elevated days (Jan16 shift → Jan15 ovulation).
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-16", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-17", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-18", 36.50),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if got := result.Format("2006-01-02"); got != "2025-01-15" {
		t.Fatalf("expected ovulation Jan15 (day before the Jan16 shift), got %s", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cyclesignalsCovBuildLutealLogs builds a realistic log slice with:
//   - three period clusters (Jan1, Jan29, Feb26) → 3 observed cycle starts
//   - 6-day BBT coverline window + 3-day rise starting Jan16 in cycle 1
//     (→ ovulation Jan15 = day before first high, luteal=14)
//   - 6-day BBT coverline window + 3-day rise starting Feb13 in cycle 2
//     (→ ovulation Feb12, luteal=14)
//
// Resulting inferred luteal phase = round((14+14)/2) = 14.
func cyclesignalsCovBuildLutealLogs(t *testing.T) []models.DailyLog {
	t.Helper()
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Cluster 1: Jan 1
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		// Cluster 2: Jan 29
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		// Cluster 3: Feb 26
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// === Cycle 1 BBT: Jan1→Jan29 ===
		// 6-day coverline window (days 1-6): max = 36.20
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		// Rise streak of 3 starting Jan16 (coverline=36.20; third ≥36.40):
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-18"), BBT: models.NewBBT(36.50)},
		// ovulation = Jan16−1 = Jan15; Jan29 − Jan15 = 14 days → luteal=14 ✓

		// === Cycle 2 BBT: Jan29→Feb26 ===
		// 6-day coverline window (days 1-6): max = 36.20
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		// Rise streak of 3 starting Feb13 (coverline=36.20; third ≥36.40):
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-15"), BBT: models.NewBBT(36.50)},
		// ovulation = Feb13−1 = Feb12; Feb26 − Feb12 = 14 days → luteal=14 ✓
	}
	return logs
}
