package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func mr3dashDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

// Kills dashboard_view_service.go BOUNDARY mutant on the
// `len(cycleFactorExplanation.HintFactorKeys) > 0` predicate feeding
// BuildOwnerPredictionExplanation. We construct a user whose factor
// explanation exists (RecentCycles / PatternSummaries from completed cycles)
// but whose recent 90-day window has NO factors, so HintFactorKeys is EMPTY.
//
// With `> 0` the secondary prediction hint must NOT be set. Under the
// `>= 0` mutant the predicate is always true, so the secondary key would be
// wrongly populated. Asserted through the page-data map, not the helper.
func TestMR3Dash_FactorHintSuppressedWhenHintKeysEmpty(t *testing.T) {
	user := &models.User{ID: 71, Role: models.RoleOwner, CycleLength: 32}
	// today sits far past every cycle-factor log, so the recent 90-day
	// factor window is empty (HintFactorKeys == nil) while the historical
	// completed cycles still carry factor keys (explanation still exists).
	today := mr3dashDay(t, "2026-10-01")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: CycleStats{
			CompletedCycleCount: 3,
			MedianCycleLength:   32,
			MinCycleLength:      24,
			MaxCycleLength:      44,
			LastPeriodStart:     mr3dashDay(t, "2026-04-20"),
			NextPeriodStart:     mr3dashDay(t, "2026-05-21"),
		}},
		&stubDashboardViewerProvider{
			logEntry: models.DailyLog{Date: today},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{
			logs: []models.DailyLog{
				{Date: mr3dashDay(t, "2026-01-01"), IsPeriod: true},
				{Date: mr3dashDay(t, "2026-01-03"), CycleFactorKeys: []string{models.CycleFactorStress}},
				{Date: mr3dashDay(t, "2026-01-25"), IsPeriod: true},
				{Date: mr3dashDay(t, "2026-01-28"), CycleFactorKeys: []string{models.CycleFactorTravel}},
				{Date: mr3dashDay(t, "2026-03-10"), IsPeriod: true},
				{Date: mr3dashDay(t, "2026-03-12"), CycleFactorKeys: []string{models.CycleFactorStress}},
				{Date: mr3dashDay(t, "2026-04-20"), IsPeriod: true},
			},
		},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}

	// Guard the fixture: the factor explanation must still EXIST (otherwise
	// the boundary is never exercised — the predicate's left operand would be
	// false). HintFactorKeys must be empty so only the `> 0` boundary decides.
	if len(viewData.PredictionFactorHintKeys) != 0 {
		t.Fatalf("fixture invalid: expected empty HintFactorKeys, got %#v", viewData.PredictionFactorHintKeys)
	}
	if viewData.HasPredictionFactorHint {
		t.Fatalf("expected HasPredictionFactorHint=false with empty hint keys")
	}
	// The mutant target: secondary prediction hint must NOT be set when the
	// hint-key count is zero. Under `>= 0` this would be wrongly populated.
	if viewData.HasPredictionExplanationSecondary {
		t.Fatalf("expected no secondary prediction explanation when HintFactorKeys is empty, got key %q", viewData.PredictionExplanationSecondaryKey)
	}
	if viewData.PredictionExplanationSecondaryKey != "" {
		t.Fatalf("expected empty secondary key, got %q", viewData.PredictionExplanationSecondaryKey)
	}
}
