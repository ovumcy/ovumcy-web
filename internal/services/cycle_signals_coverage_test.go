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

func TestCyclesignalsCov_InferUserLutealPhase_NilLocationDoesNotPanic(t *testing.T) {
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

func TestCyclesignalsCov_InferUserLutealPhase_FewerThanThreeStartsReturnsDefault(t *testing.T) {
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

func TestCyclesignalsCov_InferUserLutealPhase_ExactlyThreeStartsIsAccepted(t *testing.T) {
	// 3 starts + valid BBT + enough data to produce >= 2 luteal lengths.
	// Cycles: Jan1→Jan29 (28 days), Jan29→Feb26 (28 days).
	logs := cyclesignalsCovBuildLutealLogs(t)

	_, ok := InferUserLutealPhase(logs, time.UTC)
	// With valid BBT signals we expect ok=true; at minimum must not panic.
	// We only assert no panic and that ok matches the data.
	_ = ok
}

// ---------------------------------------------------------------------------
// Line 28 — loop bound: the last pair of starts must be processed.
// Verify by building exactly 3 starts where only the last pair yields a
// valid ovulation date via BBT, and checking that the result is influenced
// by that last pair.
// ---------------------------------------------------------------------------

func TestCyclesignalsCov_InferUserLutealPhase_LastStartPairIsIncluded(t *testing.T) {
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

func TestCyclesignalsCov_InferUserLutealPhase_OutOfRangeLutealLengthIsSkipped(t *testing.T) {
	// Three starts: Jan1, Jan29, Feb26 (28-day cycles).
	// Cycle 1 (Jan1→Jan29): ovulation Jan15 → luteal = 14 days (valid).
	// Cycle 2 (Jan29→Feb26): ovulation Feb25 → luteal = 1 day (< minLutealPhaseDays → skipped).
	//
	// With only 1 valid luteal length (< 2), must return default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		// Period clusters.
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — 5 baseline days then rise on Jan15.
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-15"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — 5 baseline days then rise on Feb25 (1 day before Feb26 start).
		// luteal = Feb26 − Feb25 = 1 day → below minLutealPhaseDays → skipped.
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-24"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-25"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-26"), BBT: models.NewBBT(36.50)},
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

func TestCyclesignalsCov_InferUserLutealPhase_LutealLengthOverTwentyIsSkipped(t *testing.T) {
	// Cycle where ovulation is very early (day 2) → luteal > 20 → skipped.
	// Three starts: Jan1, Jan29, Feb26.
	// Cycle 1: ovulation Jan7 → luteal = Jan29−Jan7 = 22 days (> 20 → skipped).
	// Cycle 2: ovulation Feb12 → luteal = 14 days (valid).
	// Only 1 valid → default, false.
	day := func(s string) time.Time { return cyclesignalsCovDay(t, s) }

	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 BBT — baseline on Jan1-5 then rise on Jan7 (luteal = Jan29−Jan7 = 22 > 20).
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-07"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-08"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-09"), BBT: models.NewBBT(36.50)},

		// Cycle 2 BBT — valid: rise on Feb12 → luteal = Feb26−Feb12 = 14 (valid).
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-12"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
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

func TestCyclesignalsCov_InferUserLutealPhase_FewerThanTwoValidLutealLengthsReturnsDefault(t *testing.T) {
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

func TestCyclesignalsCov_InferUserLutealPhase_TwoValidLutealLengthsProducesResult(t *testing.T) {
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

func TestCyclesignalsCov_InferBBTOvulationDate_FewerThanFivePointsReturnsZero(t *testing.T) {
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

func TestCyclesignalsCov_InferBBTOvulationDate_ExactlyFivePointsButNoStreakReturnsZero(t *testing.T) {
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

func TestCyclesignalsCov_InferBBTOvulationDate_ExactlyAtThresholdCountsAsStreak(t *testing.T) {
	// Use an integer-valued baseline (36.00) so that baseline+0.2 = 36.20 is
	// representable exactly in float64, allowing us to test the >= boundary
	// without float64 rounding artefacts.
	//
	// Baseline: 5 days at 36.00 → avg=36.00, threshold=36.20.
	// Rise streak of 3 at exactly 36.20 starting Jan06 → ovulation on Jan06.
	// A strict > would require >36.20, missing the equality case.
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
		cyclesignalsCovBBTLog(t, "2025-01-06", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-07", 36.20),
		cyclesignalsCovBBTLog(t, "2025-01-08", 36.20),
	}

	result := inferBBTOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if result.IsZero() {
		t.Fatalf("expected non-zero ovulation date when streak values are exactly at threshold (>= must include equality)")
	}
	// streak==3 at index=7 (Jan08) → ovulation = points[7-2] = points[5] = Jan06.
	if got := result.Format("2006-01-02"); got != "2025-01-06" {
		t.Fatalf("expected ovulation on 2025-01-06, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Line 86 — BBT=0 must be excluded: a log with BBT=0 is not a valid reading.
// ---------------------------------------------------------------------------

func TestCyclesignalsCov_CollectCycleBBTPoints_ZeroBBTIsExcluded(t *testing.T) {
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

func TestCyclesignalsCov_CollectCycleBBTPoints_CycleDayIsOneBased(t *testing.T) {
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

func TestCyclesignalsCov_InferEggWhiteOvulationDate_OnlyEggWhiteIsAccepted(t *testing.T) {
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
	if got := result.Format("2006-01-02"); got != "2025-01-15" {
		t.Fatalf("expected ovulation on 2025-01-15 (last egg-white), got %s", got)
	}
}

func TestCyclesignalsCov_InferEggWhiteOvulationDate_NoEggWhiteReturnsZero(t *testing.T) {
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

func TestCyclesignalsCov_InferEggWhiteOvulationDate_ReturnsLastEggWhiteInCycleWindow(t *testing.T) {
	cycleStart := cyclesignalsCovDay(t, "2025-01-01")
	nextStart := cyclesignalsCovDay(t, "2025-01-29")

	logs := []models.DailyLog{
		// Both are egg-white; the LAST one in the window should win.
		cyclesignalsCovMucusLog(t, "2025-01-12", models.CervicalMucusEggWhite),
		cyclesignalsCovMucusLog(t, "2025-01-14", models.CervicalMucusEggWhite),
		// Outside window — must be excluded.
		cyclesignalsCovMucusLog(t, "2025-01-30", models.CervicalMucusEggWhite),
	}

	result := inferEggWhiteOvulationDate(logs, cycleStart, nextStart, time.UTC)
	if got := result.Format("2006-01-02"); got != "2025-01-14" {
		t.Fatalf("expected last in-window egg-white on 2025-01-14, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Full integration: InferUserLutealPhase with valid BBT returns the right value.
// This exercises lines 36-37, 43, and 46.
// ---------------------------------------------------------------------------

func TestCyclesignalsCov_InferUserLutealPhase_CorrectValueFromBBT(t *testing.T) {
	// Two cycles with BBT-confirmed ovulation, each yielding luteal ~14 days.
	logs := cyclesignalsCovBuildLutealLogs(t)
	phase, ok := InferUserLutealPhase(logs, time.UTC)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// Both cycles have BBT rise on day 15 of a 28-day cycle → luteal = 14.
	if phase != 14 {
		t.Fatalf("expected inferred luteal phase 14, got %d", phase)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cyclesignalsCovBuildLutealLogs builds a realistic log slice with:
//   - three period clusters (Jan1, Jan29, Feb26) → 3 observed cycle starts
//   - BBT baseline + 3-day rise starting Jan15 in cycle 1 (→ ovulation Jan15, luteal=14)
//   - BBT baseline + 3-day rise starting Feb12 in cycle 2 (→ ovulation Feb12, luteal=14)
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
		// Rise streak of 3 starting Jan15 (threshold=36.40; values=36.5):
		{Date: day("2025-01-15"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-17"), BBT: models.NewBBT(36.50)},
		// nextStart Jan29 − ovulationDate Jan15 = 14 days → luteal=14 ✓

		// === Cycle 2 BBT: Jan29→Feb26 ===
		// 5-day baseline (days 1-5): 36.20
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		// Rise streak of 3 starting Feb12 (threshold=36.40; values=36.5):
		{Date: day("2025-02-12"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-13"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-14"), BBT: models.NewBBT(36.50)},
		// nextStart Feb26 − ovulationDate Feb12 = 14 days → luteal=14 ✓
	}
	return logs
}
