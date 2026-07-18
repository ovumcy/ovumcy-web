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
	// Cycle 1 (Jan1→Jan29): rise Jan16-18 → ovulation Jan15 → luteal = 14 days (valid).
	// Cycle 2 (Jan29→Feb26): rise Feb23-25 → ovulation Feb22 → luteal = 4 days
	//   (< minLutealPhaseDays → skipped by the range filter).
	//
	// With only 1 valid luteal length (< 2), must return default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Period clusters.
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — 5 baseline days then rise on Jan16 → ovulation Jan15.
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-18"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — 5 baseline days then rise on Feb23 → ovulation Feb22.
		// luteal = Feb26 − Feb22 = 4 days → below minLutealPhaseDays → skipped.
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
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
	// Cycle 1: rise Jan8-10 → ovulation Jan7 → luteal = Jan29−Jan7 = 22 days (> 20 → skipped).
	// Cycle 2: rise Feb13-15 → ovulation Feb12 → luteal = 14 days (valid).
	// Only 1 valid → default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — baseline Jan1-5 then rise Jan8-10 → ovulation Jan7 (luteal = Jan29−Jan7 = 22 > 20).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-08"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-09"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-10"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — valid: rise Feb13-15 → ovulation Feb12 → luteal = Feb26−Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
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
// Line 59 — BBT minimum points: fewer than 5 BBT readings in a cycle window
// returns no ovulation date.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_FewerThanFivePointsReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	// Only 4 BBT readings in the window.
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.2),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected zero date with only 4 BBT points, got %s", result.Format("2006-01-02"))
	}
}

func TestCycleSignals_InferBBTOvulationDate_ExactlyFivePointsButNoStreakReturnsZero(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	// 5 points all at the same temperature → no rise streak.
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.2),
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.2),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected zero date when no rise streak exists, got %s", result.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Line 71 — threshold comparison: a value exactly equal to threshold must
// count toward the streak (>= not >).
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_ExactlyAtThresholdCountsAsStreak(t *testing.T) {
	// Use an integer-valued baseline (36.00) so that baseline+0.2 = 36.20 is
	// representable exactly in float64, allowing us to test the >= boundary
	// without float64 rounding artefacts.
	//
	// Baseline: 5 days at 36.00 → avg=36.00, threshold=36.20.
	// Rise streak of 3 at exactly 36.20 starting Jan07 → ovulation Jan06 (the day
	// before the shift). A strict > would require >36.20, missing the equality case.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	baseline := 36.00
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", baseline),
		cyclesignalsCovBBTLog(t, "2025-01-02", baseline),
		cyclesignalsCovBBTLog(t, "2025-01-03", baseline),
		cyclesignalsCovBBTLog(t, "2025-01-04", baseline),
		cyclesignalsCovBBTLog(t, "2025-01-05", baseline),
		// threshold = 36.00 + 0.2 = 36.20; these are exactly at threshold.
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-09", 36.20),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if result.IsZero() {
		t.Fatalf("expected non-zero ovulation date when streak values are exactly at threshold (>= must include equality)")
	}
	// Shift begins Jan07 (first high day) → ovulation is the day before = Jan06.
	if got := result.Format("2006-01-02"); got != "2025-01-06" {
		t.Fatalf("expected ovulation on 2025-01-06, got %s", got)
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
// Unified detector regressions — the two defects the shared detector fixes:
// (1) a shift requires three CONSECUTIVE calendar days, and (2) ovulation is the
// day BEFORE the shift, so luteal inference and the chart marker agree.
// ---------------------------------------------------------------------------

func TestCycleSignals_InferBBTOvulationDate_NonConsecutiveElevatedDaysAreNotAShift(t *testing.T) {
	// Sparse elevated readings on non-consecutive calendar days (cycle days 12,
	// 16, 20) are noise, not a sustained thermal shift. The detector requires
	// three consecutive calendar days, so no ovulation is inferred. The previous
	// implementation counted any three elevated readings and wrongly reported a
	// shift here.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-12", 36.60),
		cyclesignalsCovBBTLog(t, "2025-01-16", 36.60),
		cyclesignalsCovBBTLog(t, "2025-01-20", 36.60),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if !result.IsZero() {
		t.Fatalf("expected no ovulation from non-consecutive elevated days, got %s", result.Format("2006-01-02"))
	}
}

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
		cyclesignalsCovBBTLog(t, "2025-01-16", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-17", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-18", 36.50),
	}
	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if got := result.Format("2006-01-02"); got != "2025-01-15" {
		t.Fatalf("expected ovulation Jan15 (day before the Jan16 shift), got %s", got)
	}
}

func TestCycleSignals_BBTChartMarkerAndInferenceAgreeOnOvulationDay(t *testing.T) {
	// The luteal-phase inference and the BBT chart marker share one detector, so
	// for identical readings the inferred ovulation day and the chart marker day
	// must reference the same cycle day.
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")
	logs := []models.DailyLog{
		cyclesignalsCovBBTLog(t, "2025-01-01", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-02", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-03", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-04", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-05", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-16", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-17", 36.50),
		cyclesignalsCovBBTLog(t, "2025-01-18", 36.50),
	}

	ovDate := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if ovDate.IsZero() {
		t.Fatal("expected inference to detect ovulation")
	}
	inferenceCycleDay := CalendarDaysBetween(cycleStart, ovDate) + 1

	points := collectCycleBBTPoints(logs, cycleStart, nextStart, time.UTC)
	recordedDays := make([]int, len(points))
	dayValues := make(map[int]float64, len(points))
	var baselineTotal float64
	for index, point := range points {
		recordedDays[index] = point.CycleDay
		dayValues[point.CycleDay] = point.Value
		if index < bbtBaselineWindow {
			baselineTotal += point.Value
		}
	}
	baseline := baselineTotal / float64(bbtBaselineWindow)

	markerIndex, ok := detectProbableOvulationMarker(recordedDays, dayValues, baseline)
	if !ok {
		t.Fatal("expected chart marker to detect ovulation")
	}
	// The marker index is zero-based for markerDay, so markerDay = markerIndex + 1.
	chartMarkerCycleDay := markerIndex + 1

	if inferenceCycleDay != chartMarkerCycleDay {
		t.Fatalf("inference ovulation cycle-day %d must equal chart marker cycle-day %d", inferenceCycleDay, chartMarkerCycleDay)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cyclesignalsCovBuildLutealLogs builds a realistic log slice with:
//   - three period clusters (Jan1, Jan29, Feb26) → 3 observed cycle starts
//   - BBT baseline + 3-day rise starting Jan16 in cycle 1 (→ ovulation Jan15, the
//     day before the shift, luteal=14)
//   - BBT baseline + 3-day rise starting Feb13 in cycle 2 (→ ovulation Feb12, the
//     day before the shift, luteal=14)
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
		// 5-day baseline (days 1-5): 36.20
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		// Rise streak of 3 starting Jan16 (threshold=36.40; values=36.5):
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-18"), BBT: models.NewBBT(36.50)},
		// ovulationDate = day before the shift = Jan15; nextStart Jan29 − Jan15 = 14 → luteal=14 ✓

		// === Cycle 2 BBT: Jan29→Feb26 ===
		// 5-day baseline (days 1-5): 36.20
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		// Rise streak of 3 starting Feb13 (threshold=36.40; values=36.5):
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-15"), BBT: models.NewBBT(36.50)},
		// ovulationDate = day before the shift = Feb12; nextStart Feb26 − Feb12 = 14 → luteal=14 ✓
	}
	return logs
}
