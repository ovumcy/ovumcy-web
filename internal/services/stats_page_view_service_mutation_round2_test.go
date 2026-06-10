package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestStatspageviewserviceCovPredictionExplanationSecondaryAbsentWhenHintKeysEmptyButOldCycleFactorsExist(t *testing.T) {
	// Three period starts 28 days apart => two completed cycles. A cycle factor
	// (stress) is logged on 2026-01-10, inside the first completed cycle
	// [2026-01-01, 2026-01-29). now is pushed to 2026-06-01 so the 90-day recent
	// window [2026-03-04, 2026-06-01] excludes 2026-01-10: RecentFactors and the
	// derived HintFactorKeys are empty, while PatternSummaries/RecentCycles
	// (built from the in-cycle factor) are non-empty => hasCycleFactorExplanation
	// is true. The owner is irregular (to open the factor-explanation gate) but
	// predictable (UnpredictableCycle=false) so cycleContext.PredictionDisabled
	// is false. Under the original guard the secondary explanation must stay
	// absent; the boundary mutant (>0 -> >=0) would surface it.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-10"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 130, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-06-01")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	// Guard: hasCycleFactorExplanation must be true via the in-cycle factor path,
	// otherwise the boundary on line 120 is not exercised.
	if !viewData.HasCycleFactorPatternSummaries && !viewData.HasRecentFactorCycles {
		t.Fatalf("precondition failed: expected cycle-factor explanation to be present via pattern/recent-cycle context")
	}
	if viewData.HasPredictionFactorHint {
		t.Fatalf("precondition failed: expected empty HintFactorKeys (no recent-window factors)")
	}
	if viewData.HasPredictionExplanationSecondary {
		t.Fatalf("expected HasPredictionExplanationSecondary=false when HintFactorKeys is empty, got key=%q", viewData.PredictionExplanationSecondaryKey)
	}
	if viewData.PredictionExplanationSecondaryKey != "" {
		t.Fatalf("expected empty secondary key, got %q", viewData.PredictionExplanationSecondaryKey)
	}
}

func TestStatspageviewserviceCovHasRecentCycleFactorsFalseWhenOnlyOldCycleFactorsExist(t *testing.T) {
	// Stress factor logged 2026-01-10 (inside completed cycle 1) with now far
	// enough out (2026-06-01) that the recent 90-day window excludes it. The
	// explanation is present via pattern/recent-cycle context, but RecentFactors
	// is empty so HasRecentCycleFactors must be false. The boundary mutant on
	// line 149 (>0 -> >=0) would force it true.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-10"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 131, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-06-01")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	// Guard: explanation must be present (via pattern/recent-cycle context) so
	// that the && short-circuit does not mask the mutated len comparison.
	if !viewData.HasCycleFactorPatternSummaries && !viewData.HasRecentFactorCycles {
		t.Fatalf("precondition failed: expected cycle-factor explanation present via pattern/recent-cycle context")
	}
	if viewData.HasRecentCycleFactors {
		t.Fatalf("expected HasRecentCycleFactors=false when no factors fall in the recent window")
	}
	if len(viewData.RecentCycleFactors) != 0 {
		t.Fatalf("expected empty RecentCycleFactors, got %#v", viewData.RecentCycleFactors)
	}
}

func TestStatspageviewserviceCovHasCycleFactorPatternSummariesFalseWhenFactorsOnlyInOngoingCycle(t *testing.T) {
	// Three period starts (two completed cycles ending 2026-02-26). The only
	// cycle factor is logged 2026-03-05, AFTER the last cycle start, i.e. in the
	// ongoing/incomplete cycle, and inside the recent 90-day window for
	// now=2026-03-10. RecentFactors is therefore non-empty (hasCycleFactorExplanation
	// is true) but no completed cycle span contains a factor, so snapshots ->
	// PatternSummaries are empty. HasCycleFactorPatternSummaries must be false;
	// the boundary mutant on line 150 (>0 -> >=0) would force it true.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-05"), CycleFactorKeys: []string{models.CycleFactorStress}},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 132, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-03-10")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	// Guard: explanation must be present (via recent factors) so the && does not
	// mask the mutated len comparison.
	if !viewData.HasRecentCycleFactors {
		t.Fatalf("precondition failed: expected recent cycle factors to make the explanation present")
	}
	if viewData.HasCycleFactorPatternSummaries {
		t.Fatalf("expected HasCycleFactorPatternSummaries=false when no completed cycle carries a factor")
	}
	if len(viewData.CycleFactorPatternSummaries) != 0 {
		t.Fatalf("expected empty CycleFactorPatternSummaries, got %#v", viewData.CycleFactorPatternSummaries)
	}
}

func TestStatspageviewserviceCovHasRecentFactorCyclesFalseWhenFactorsOnlyInOngoingCycle(t *testing.T) {
	// Same shape as the pattern-summary case: the lone cycle factor (2026-03-05)
	// sits in the ongoing cycle (after the last start 2026-02-26) and within the
	// recent 90-day window for now=2026-03-10. RecentFactors is non-empty so the
	// explanation is present, but no completed cycle contains a factor, so
	// snapshots -> RecentCycles are empty. HasRecentFactorCycles must be false;
	// the boundary mutant on line 151 (>0 -> >=0) would force it true.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-03-05"), CycleFactorKeys: []string{models.CycleFactorStress}},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 133, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-03-10")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	// Guard: explanation present via recent factors so the && does not mask the
	// mutated len comparison.
	if !viewData.HasRecentCycleFactors {
		t.Fatalf("precondition failed: expected recent cycle factors to make the explanation present")
	}
	if viewData.HasRecentFactorCycles {
		t.Fatalf("expected HasRecentFactorCycles=false when no completed cycle carries a factor")
	}
	if len(viewData.RecentFactorCycles) != 0 {
		t.Fatalf("expected empty RecentFactorCycles, got %#v", viewData.RecentFactorCycles)
	}
}

func TestStatspageviewserviceCovHasPredictionFactorHintFalseWhenOnlyOldCycleFactorsExist(t *testing.T) {
	// Stress factor logged 2026-01-10 (inside completed cycle 1) with now far out
	// (2026-06-01) so the recent 90-day window excludes it. The explanation is
	// present via pattern/recent-cycle context, but HintFactorKeys (derived from
	// recent-window factors) is empty so HasPredictionFactorHint must be false.
	// The boundary mutant on line 152 (>0 -> >=0) would force it true.
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-01-10"), CycleFactorKeys: []string{models.CycleFactorStress}},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},
	}
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	user := &models.User{ID: 134, Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	now := mustParseStatsServiceDay(t, "2026-06-01")

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	// Guard: explanation present via pattern/recent-cycle context so the && does
	// not mask the mutated len comparison.
	if !viewData.HasCycleFactorPatternSummaries && !viewData.HasRecentFactorCycles {
		t.Fatalf("precondition failed: expected cycle-factor explanation present via pattern/recent-cycle context")
	}
	if viewData.HasPredictionFactorHint {
		t.Fatalf("expected HasPredictionFactorHint=false when no factors fall in the recent window")
	}
	if len(viewData.PredictionFactorHintKeys) != 0 {
		t.Fatalf("expected empty PredictionFactorHintKeys, got %#v", viewData.PredictionFactorHintKeys)
	}
}

func TestStatspageviewserviceCovPredictionReliabilityShownAtExactlyTwoCompletedCycles(t *testing.T) {
	// Exactly two completed cycles (three period starts 28 days apart) for a
	// predictable owner. statsMinimumInsightsCycles is 2, so CompletedCycleCount
	// (2) is NOT < 2: reliability must be shown with an uncapped sample count of
	// 2. The boundary mutant on line 261 (< -> <=) would early-return, hiding it.
	logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 2)
	service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})
	now := logs[len(logs)-1].Date.AddDate(0, 0, 5)

	viewData, err := service.BuildStatsPageViewData(context.Background(),
		&models.User{ID: 135, Role: models.RoleOwner, CycleLength: 28},
		"en", "Cycle %d", now, time.UTC, 12,
	)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}
	if viewData.Flags.CompletedCycleCount != 2 {
		t.Fatalf("precondition failed: expected CompletedCycleCount=2, got %d", viewData.Flags.CompletedCycleCount)
	}
	if !viewData.ShowPredictionReliability {
		t.Fatalf("expected ShowPredictionReliability=true at exactly statsMinimumInsightsCycles (2) completed cycles")
	}
	if viewData.PredictionSampleCount != 2 {
		t.Fatalf("expected PredictionSampleCount=2 at exactly two completed cycles, got %d", viewData.PredictionSampleCount)
	}
}
