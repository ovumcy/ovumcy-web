package services

// Coverage tests that kill surviving mutants in stats_page_view_service.go.
// Every helper/type defined here is prefixed with statspageviewserviceCov to
// avoid collisions when merged with other agents' test files.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Line 119 – BuildOwnerPredictionExplanation third-arg boolean
// hasCycleFactorExplanation && len(HintFactorKeys) > 0
// Mutation: the len() guard is dropped or the && becomes ||.
// A regular-cycle owner who has pattern/cycle context but NO hint keys must
// produce HasPredictionExplanationSecondary=false.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovPredictionExplanationSecondaryAbsentWhenHintKeysEmpty(t *testing.T) {
	// Logs: 4 period starts, 3 completed cycles with irregular spread (>7 days)
	// but NO CycleFactorKeys on any log, so HintFactorKeys will be empty.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-20"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-05"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-05"), IsPeriod: true},
	}
	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs, logsForAll: logs},
		&stubStatsSymptomReader{},
	)
	user := &models.User{ID: 99, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-03-10")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.HasPredictionExplanationSecondary {
		t.Fatalf(
			"expected HasPredictionExplanationSecondary=false when HintFactorKeys is empty, got key=%q",
			viewData.PredictionExplanationSecondaryKey,
		)
	}
	if viewData.PredictionExplanationSecondaryKey != "" {
		t.Fatalf("expected empty secondary key, got %q", viewData.PredictionExplanationSecondaryKey)
	}
}

// ---------------------------------------------------------------------------
// Line 146 – HasLastCycleSymptoms: len(ownerInsights.lastCycleSymptoms) > 0
// Mutation: condition dropped so it becomes always true (>= 0).
// When the service has no symptom reader (nil), lastCycleSymptoms is empty;
// HasLastCycleSymptoms must be false.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovHasLastCycleSymptomsIsFalseWhenNoneLogged(t *testing.T) {
	// Owner with logs but no symptom IDs on any log entry and no symptom types
	// in the catalog: lastCycleSymptoms will be empty, so HasLastCycleSymptoms
	// must be false.  The stubStatsSymptomReader with an empty symptoms slice
	// returns no SymptomType lookups, so buildLastCycleSymptomCounts returns [].
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs, logsForAll: logs},
		&stubStatsSymptomReader{symptoms: []models.SymptomType{}},
	)
	user := &models.User{ID: 42, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-03-15")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.HasLastCycleSymptoms {
		t.Fatalf("expected HasLastCycleSymptoms=false when no symptom data available")
	}
	if len(viewData.LastCycleSymptoms) != 0 {
		t.Fatalf("expected empty LastCycleSymptoms, got %#v", viewData.LastCycleSymptoms)
	}
}

// ---------------------------------------------------------------------------
// Line 147 – HasSymptomPatterns: len(ownerInsights.symptomPatterns) > 0
// Mutation: condition dropped so it becomes always true.
// Same nil-reader scenario: symptomPatterns is empty.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovHasSymptomPatternsIsFalseWhenNoneAvailable(t *testing.T) {
	// No symptom types and no symptom IDs on logs → symptomPatterns empty.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs, logsForAll: logs},
		&stubStatsSymptomReader{symptoms: []models.SymptomType{}},
	)
	user := &models.User{ID: 43, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-03-15")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.HasSymptomPatterns {
		t.Fatalf("expected HasSymptomPatterns=false when no symptom data available")
	}
	if len(viewData.SymptomPatterns) != 0 {
		t.Fatalf("expected empty SymptomPatterns, got %#v", viewData.SymptomPatterns)
	}
}

// ---------------------------------------------------------------------------
// Line 152 – HasCurrentCycleBBTChart: len(ownerInsights.currentCycleBBTChart.Labels) > 0
// Mutation: condition dropped so it becomes always true.
// An owner without BBT data must have HasCurrentCycleBBTChart=false.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovHasCurrentCycleBBTChartIsFalseWhenNoBBTData(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	// TrackBBT=false → no BBT entries → chart labels empty
	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs},
		&stubStatsSymptomReader{},
	)
	user := &models.User{ID: 44, Role: models.RoleOwner, CycleLength: 28, TrackBBT: false}
	now := mustParseStatsServiceDay(t, "2026-03-15")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.HasCurrentCycleBBTChart {
		t.Fatalf("expected HasCurrentCycleBBTChart=false when no BBT data logged")
	}
	if len(viewData.CurrentCycleBBTChart.Labels) != 0 {
		t.Fatalf("expected empty BBT chart labels, got %#v", viewData.CurrentCycleBBTChart.Labels)
	}
}

// ---------------------------------------------------------------------------
// Line 166 – now.AddDate(-2, 0, 0): the 2-year look-back window.
// Mutation: the year offset is changed (e.g. -1 or -3).
// We use stubStatsDayReader.gotFrom to verify the range start.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovBaseDataRangeStartsExactlyTwoYearsBack(t *testing.T) {
	dayReader := &stubStatsDayReader{}
	service := NewStatsService(dayReader, &stubStatsSymptomReader{})
	user := &models.User{ID: 50, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseStatsServiceDay(t, "2026-04-15")

	_, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}

	expectedFrom := now.AddDate(-2, 0, 0)
	if !dayReader.gotFrom.Equal(expectedFrom) {
		t.Fatalf("expected range start %s (2 years back), got %s", expectedFrom.Format("2006-01-02"), dayReader.gotFrom.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Line 238 – shouldShowStatsIrregularInsufficientDataNotice
// user.IrregularCycle && flags.CompletedCycleCount < 3
//
// Test A: exactly 3 completed cycles → notice must be ABSENT (boundary).
// Test B: non-irregular user with 1 completed cycle → notice must be ABSENT.
// ---------------------------------------------------------------------------

func statspageviewserviceCovLogsWithNCompletedCycles(t *testing.T, n int) []models.DailyLog {
	t.Helper()
	// Build n+1 period starts 28 days apart so there are n completed cycles.
	base, err := time.ParseInLocation("2006-01-02", "2026-01-01", time.UTC)
	if err != nil {
		t.Fatalf("parse base date: %v", err)
	}
	logs := make([]models.DailyLog, 0, n+1)
	for i := 0; i <= n; i++ {
		logs = append(logs, models.DailyLog{
			Date:     base.AddDate(0, 0, i*28),
			IsPeriod: true,
		})
	}
	return logs
}

func TestStatspageviewserviceCovIrregularInsufficientDataNoticeAbsentAtThreeCycles(t *testing.T) {
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	// last log date is ~84 days after start; use a date shortly after
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 60, Role: models.RoleOwner, IrregularCycle: true},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.ShowIrregularInsufficientDataNotice {
		t.Fatalf("expected ShowIrregularInsufficientDataNotice=false at exactly 3 completed cycles, got true")
	}
}

func TestStatspageviewserviceCovIrregularInsufficientDataNoticeAbsentForNonIrregularUser(t *testing.T) {
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 1)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 61, Role: models.RoleOwner, IrregularCycle: false, CycleLength: 28},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.ShowIrregularInsufficientDataNotice {
		t.Fatalf("expected ShowIrregularInsufficientDataNotice=false for non-irregular user")
	}
}

// ---------------------------------------------------------------------------
// Line 252 – isStatsPredictionDisabled: user.UnpredictableCycle
// Mutation: the field is negated or the nil check is dropped.
// PredictionDisabled in view data must be true iff UnpredictableCycle=true.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovPredictionDisabledReflectsUnpredictableCycleFlag(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	now := mustParseStatsServiceDay(t, "2026-04-10")

	unpredictable := &models.User{ID: 70, Role: models.RoleOwner, CycleLength: 28, UnpredictableCycle: true}
	viewData, err := service.BuildStatsPageViewData(context.Background(), unpredictable, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if !viewData.PredictionDisabled {
		t.Fatalf("expected PredictionDisabled=true for user with UnpredictableCycle=true")
	}

	predictable := &models.User{ID: 71, Role: models.RoleOwner, CycleLength: 28, UnpredictableCycle: false}
	viewData2, err := service.BuildStatsPageViewData(context.Background(), predictable, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData2.PredictionDisabled {
		t.Fatalf("expected PredictionDisabled=false for user with UnpredictableCycle=false")
	}
}

// ---------------------------------------------------------------------------
// Line 260 – buildStatsPredictionReliability early return:
// flags.CompletedCycleCount < statsMinimumInsightsCycles || isStatsPredictionDisabled(user)
//
// Mutation: || becomes && → an unpredictable owner with enough cycles would
// erroneously get ShowPredictionReliability=true.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovPredictionReliabilityHiddenForUnpredictableOwnerWithEnoughCycles(t *testing.T) {
	// 3 completed cycles is enough for statsMinimumInsightsCycles (2), but
	// UnpredictableCycle=true must suppress reliability display.
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 80, Role: models.RoleOwner, CycleLength: 28, UnpredictableCycle: true},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.ShowPredictionReliability {
		t.Fatalf("expected ShowPredictionReliability=false for unpredictable user (line-260 guard)")
	}
}

// ---------------------------------------------------------------------------
// Line 266 – sampleCount > cyclePredictionWindow (= 6)
// Mutation: > becomes >= → exactly-6 cycles would wrongly cap sampleCount.
//
// With exactly 6 completed cycles: sampleCount must stay 6,
// PredictionSampleUsesRecentWindow must be false.
// With 7 completed cycles: sampleCount must be capped to 6,
// PredictionSampleUsesRecentWindow must be true.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovPredictionWindowCapAtSixCycles(t *testing.T) {
	// 6 completed cycles – should NOT be capped.
	logs6 := statspageviewserviceCovLogsWithNCompletedCycles(t, 6)
	now6 := logs6[len(logs6)-1].Date.AddDate(0, 0, 5)
	service6 := NewStatsService(&stubStatsDayReader{logsForRange: logs6}, &stubStatsSymptomReader{})
	vd6, err := service6.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 90, Role: models.RoleOwner, CycleLength: 28},
		"en", "Cycle %d", now6, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData(6 cycles) unexpected error: %v", err)
	}
	if vd6.PredictionSampleUsesRecentWindow {
		t.Fatalf("expected PredictionSampleUsesRecentWindow=false at exactly cyclePredictionWindow (6) cycles")
	}
	if vd6.PredictionSampleCount != 6 {
		t.Fatalf("expected PredictionSampleCount=6, got %d", vd6.PredictionSampleCount)
	}

	// 7 completed cycles – should be capped to cyclePredictionWindow (6).
	logs7 := statspageviewserviceCovLogsWithNCompletedCycles(t, 7)
	now7 := logs7[len(logs7)-1].Date.AddDate(0, 0, 5)
	service7 := NewStatsService(&stubStatsDayReader{logsForRange: logs7}, &stubStatsSymptomReader{})
	vd7, err := service7.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 91, Role: models.RoleOwner, CycleLength: 28},
		"en", "Cycle %d", now7, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData(7 cycles) unexpected error: %v", err)
	}
	if !vd7.PredictionSampleUsesRecentWindow {
		t.Fatalf("expected PredictionSampleUsesRecentWindow=true when sampleCount > cyclePredictionWindow")
	}
	if vd7.PredictionSampleCount != 6 {
		t.Fatalf("expected PredictionSampleCount capped to 6, got %d", vd7.PredictionSampleCount)
	}
}

// ---------------------------------------------------------------------------
// Line 271 – variablePattern computation:
// user.IrregularCycle || (flags.CompletedCycleCount >= minimumPhaseInsightCycles && IsIrregularCycleSpread(stats))
//
// Test A: IrregularCycle=true with 3 cycles (= minimumPhaseInsightCycles) and regular spread
//         → variablePattern=true via left branch only → "variable" label.
// Test B: IrregularCycle=false, ≥3 cycles with irregular spread
//         → variablePattern=true via right branch → "variable" label.
// Test C: IrregularCycle=false, ≥3 cycles WITHOUT irregular spread
//         → variablePattern=false → NOT "variable" label.
// ---------------------------------------------------------------------------

// statspageviewserviceCovIrregularSpreadLogs builds logs with a wide spread:
// cycle 1 = 14 days, cycle 2 = 35 days (spread > 7 days → irregular).
func statspageviewserviceCovIrregularSpreadLogs(t *testing.T, nExtraCycles int) []models.DailyLog {
	t.Helper()
	base, err := time.ParseInLocation("2006-01-02", "2025-01-01", time.UTC)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	// Start: 2025-01-01, next: +14, next: +35, then 28-day cadence for extras
	offsets := []int{0, 14, 49}
	cur := 49
	for i := 0; i < nExtraCycles; i++ {
		cur += 28
		offsets = append(offsets, cur)
	}
	logs := make([]models.DailyLog, 0, len(offsets))
	for _, d := range offsets {
		logs = append(logs, models.DailyLog{
			Date:     base.AddDate(0, 0, d),
			IsPeriod: true,
		})
	}
	return logs
}

func TestStatspageviewserviceCovVariablePatternViaIrregularCycleFlagThreeCompletedCycles(t *testing.T) {
	// IrregularCycle=true, 3 completed cycles (= minimumPhaseInsightCycles=3).
	// Right branch of || cannot activate (no spread with uniform 28-day cycles).
	// Left branch (user.IrregularCycle) makes variablePattern=true AND
	// sampleCount(3) >= minimumPhaseInsightCycles(3) → hits line 275 → "variable".
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 100, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.variable" {
		t.Fatalf("expected variable reliability label for irregular user with 3 cycles, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

func TestStatspageviewserviceCovVariablePatternViaSpreadForNonIrregularUser(t *testing.T) {
	// IrregularCycle=false, 3 completed cycles with irregular spread.
	// Right branch activates: CompletedCycleCount(3) >= minimumPhaseInsightCycles(3) && IsIrregularCycleSpread(stats).
	// The spread logs have cycle lengths 14 and 35 (spread=21 > 7).
	logs := statspageviewserviceCovIrregularSpreadLogs(t, 1) // 4 period starts → 3 completed cycles
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 101, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: false},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.variable" {
		t.Fatalf("expected variable reliability label for non-irregular user with irregular spread and 3 cycles, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

func TestStatspageviewserviceCovVariablePatternAbsentForNonIrregularRegularSpread(t *testing.T) {
	// IrregularCycle=false, 3 completed cycles all 28 days (spread=0).
	// variablePattern=false → label must NOT be "variable".
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 102, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: false},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey == "stats.reliability.variable" {
		t.Fatalf("expected non-variable reliability label for regular spread, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

// ---------------------------------------------------------------------------
// Lines 275/277/279 (NOT COVERED) – switch cases in buildStatsPredictionReliability
//
// Line 275: variablePattern=true && sampleCount >= minimumPhaseInsightCycles(3) → "variable"
// Line 277: sampleCount >= cyclePredictionWindow(6) && !variablePattern → "stable"
// Line 279: minimumPhaseInsightCycles(3) <= sampleCount < cyclePredictionWindow(6) && !variablePattern → "building"
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovPredictionReliabilityVariableLabelAtThreeOrMoreCycles(t *testing.T) {
	// IrregularCycle=true, 4 completed cycles (>= minimumPhaseInsightCycles=3).
	// Line 275 branch: variablePattern=true && sampleCount(4) >= 3 → "variable".
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 4)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 110, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.variable" {
		t.Fatalf("expected stats.reliability.variable for 4-cycle irregular user, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

func TestStatspageviewserviceCovPredictionReliabilityStableLabelAtSixOrMoreRegularCycles(t *testing.T) {
	// IrregularCycle=false, 6 completed cycles, regular spread (all 28 days).
	// variablePattern=false, sampleCount(6) >= cyclePredictionWindow(6) → "stable".
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 6)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 111, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: false},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.stable" {
		t.Fatalf("expected stats.reliability.stable for 6-cycle regular user, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

func TestStatspageviewserviceCovPredictionReliabilityBuildingLabelAtThreeToFiveCycles(t *testing.T) {
	// IrregularCycle=false, exactly 4 cycles, regular spread.
	// variablePattern=false, minimumPhaseInsightCycles(3) <= 4 < cyclePredictionWindow(6) → "building".
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 4)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 112, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: false},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityLabelKey != "stats.reliability.building" {
		t.Fatalf("expected stats.reliability.building for 4-cycle regular user, got %q", viewData.PredictionReliabilityLabelKey)
	}
}

// ---------------------------------------------------------------------------
// hintKey branch in buildStatsPredictionReliability (lines 283-285):
// variablePattern → hint_variable, otherwise hint.
// ---------------------------------------------------------------------------

func TestStatspageviewserviceCovHintKeyIsVariableForIrregularPattern(t *testing.T) {
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 120, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityHintKey != "stats.reliability.hint_variable" {
		t.Fatalf("expected hint_variable for irregular user, got %q", viewData.PredictionReliabilityHintKey)
	}
}

func TestStatspageviewserviceCovHintKeyIsStandardForRegularPattern(t *testing.T) {
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 121, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: false},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.PredictionReliabilityHintKey != "stats.reliability.hint" {
		t.Fatalf("expected standard hint for regular user, got %q", viewData.PredictionReliabilityHintKey)
	}
}
