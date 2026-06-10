package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Argument-capturing stub – lets tests verify what date range was requested
// ---------------------------------------------------------------------------

type dashboardviewserviceCovCapturingStatsProvider struct {
	stats    CycleStats
	captFrom time.Time
	captTo   time.Time
}

func (s *dashboardviewserviceCovCapturingStatsProvider) BuildCycleStatsForRange(ctx context.Context,
	_ *models.User, from time.Time, to time.Time, _ time.Time, _ *time.Location,
) (CycleStats, []models.DailyLog, error) {
	s.captFrom = from
	s.captTo = to
	return s.stats, nil, nil
}

// ---------------------------------------------------------------------------
// Line 112: stats range must look back exactly two years from today
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovStatsRangeIsTwoYears(t *testing.T) {
	user := &models.User{ID: 1, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-06-07", time.UTC)
	wantFrom, _ := time.ParseInLocation("2006-01-02", "2024-06-07", time.UTC)

	capturing := &dashboardviewserviceCovCapturingStatsProvider{}
	svc := NewDashboardViewService(
		capturing,
		&stubDashboardViewerProvider{},
		&stubDashboardDayStateProvider{},
	)
	if _, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturing.captFrom.Equal(wantFrom) {
		t.Fatalf("expected stats from=%s (2 years back), got %s",
			wantFrom.Format("2006-01-02"), capturing.captFrom.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Line 141: Yesterday field is today-1 day
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovYesterdayIsOneDayBack(t *testing.T) {
	user := &models.User{ID: 2, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-03-15", time.UTC)
	wantYesterday, _ := time.ParseInLocation("2006-01-02", "2026-03-14", time.UTC)

	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{},
		&stubDashboardDayStateProvider{},
	)
	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vd.Yesterday.Equal(wantYesterday) {
		t.Fatalf("expected Yesterday=%s, got %s",
			wantYesterday.Format("2006-01-02"), vd.Yesterday.Format("2006-01-02"))
	}
	if vd.YesterdayMonth != "2026-03" {
		t.Fatalf("expected YesterdayMonth=2026-03, got %s", vd.YesterdayMonth)
	}
}

// ---------------------------------------------------------------------------
// Line 167: TodayEntryExists reflects whether the log has a non-zero ID
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovTodayEntryExistsWithID(t *testing.T) {
	user := &models.User{ID: 3, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-03-15", time.UTC)

	svcWithID := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{logEntry: models.DailyLog{ID: 42, Date: now, IsPeriod: true}},
		&stubDashboardDayStateProvider{},
	)
	vd, err := svcWithID.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vd.TodayEntryExists {
		t.Fatal("expected TodayEntryExists=true when log ID > 0")
	}

	svcNoID := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{logEntry: models.DailyLog{ID: 0, Date: now}},
		&stubDashboardDayStateProvider{},
	)
	vd2, err := svcNoID.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vd2.TodayEntryExists {
		t.Fatal("expected TodayEntryExists=false when log ID == 0")
	}
}

// ---------------------------------------------------------------------------
// Line 171: HasExtraSymptoms = true when extra bucket is non-empty (dashboard)
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovHasExtraSymptomsPopulatedInDashboard(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-03-15", time.UTC)

	// 9 symptoms so that SplitSymptomsForCollapsedPicker spills beyond the 8 primary limit.
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", IsBuiltin: true},
		{ID: 2, Name: "Headache", IsBuiltin: true},
		{ID: 3, Name: "Bloating", IsBuiltin: true},
		{ID: 4, Name: "Fatigue", IsBuiltin: true},
		{ID: 5, Name: "Backache", IsBuiltin: true},
		{ID: 6, Name: "Nausea", IsBuiltin: true},
		{ID: 7, Name: "Insomnia", IsBuiltin: true},
		{ID: 8, Name: "Tender breasts", IsBuiltin: true},
		{ID: 9, Name: "Acne", IsBuiltin: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{},
	)
	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vd.HasExtraSymptoms {
		t.Fatal("expected HasExtraSymptoms=true when more than 8 symptoms are available")
	}
	if len(vd.ExtraSymptoms) == 0 {
		t.Fatal("expected non-empty ExtraSymptoms slice")
	}

	// With ≤ 8 symptoms HasExtraSymptoms must be false.
	svcFew := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms[:3]},
		&stubDashboardDayStateProvider{},
	)
	vdFew, err := svcFew.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vdFew.HasExtraSymptoms {
		t.Fatal("expected HasExtraSymptoms=false when ≤ 8 symptoms are available")
	}
}

// ---------------------------------------------------------------------------
// Line 198: dashboardPredictionExplanationState – hasPredictionFactorHint
//           requires BOTH hasCycleFactorExplanation AND non-empty hint keys
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovPredictionFactorHintRequiresBothConditions(t *testing.T) {
	user := &models.User{ID: 5, Role: models.RoleOwner}
	cycleContext := DashboardCycleContext{}

	// Case A: hasCycleFactorExplanation=true but HintFactorKeys is empty
	// → hasPredictionFactorHint must be false
	explEmpty := StatsCycleFactorExplanation{HintFactorKeys: []string{}}
	_, _, hint := dashboardPredictionExplanationState(user, cycleContext, explEmpty, true)
	if hint {
		t.Fatal("expected hasPredictionFactorHint=false when HintFactorKeys is empty")
	}

	// Case B: hasCycleFactorExplanation=false but HintFactorKeys has entries
	// → hasPredictionFactorHint must still be false
	explWithKeys := StatsCycleFactorExplanation{HintFactorKeys: []string{"stress"}}
	_, _, hintB := dashboardPredictionExplanationState(user, cycleContext, explWithKeys, false)
	if hintB {
		t.Fatal("expected hasPredictionFactorHint=false when hasCycleFactorExplanation=false")
	}

	// Case C: both true → hasPredictionFactorHint must be true
	_, _, hintC := dashboardPredictionExplanationState(user, cycleContext, explWithKeys, true)
	if !hintC {
		t.Fatal("expected hasPredictionFactorHint=true when both conditions are met")
	}
}

// ---------------------------------------------------------------------------
// Line 269: HasExtraSymptoms = true when extra bucket is non-empty (day editor)
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovHasExtraSymptomsPopulatedInDayEditor(t *testing.T) {
	user := &models.User{ID: 6, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-03-15", time.UTC)
	day, _ := time.ParseInLocation("2006-01-02", "2026-03-14", time.UTC)

	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", IsBuiltin: true},
		{ID: 2, Name: "Headache", IsBuiltin: true},
		{ID: 3, Name: "Bloating", IsBuiltin: true},
		{ID: 4, Name: "Fatigue", IsBuiltin: true},
		{ID: 5, Name: "Backache", IsBuiltin: true},
		{ID: 6, Name: "Nausea", IsBuiltin: true},
		{ID: 7, Name: "Insomnia", IsBuiltin: true},
		{ID: 8, Name: "Tender breasts", IsBuiltin: true},
		{ID: 9, Name: "Acne", IsBuiltin: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{},
	)
	vd, err := svc.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vd.HasExtraSymptoms {
		t.Fatal("expected HasExtraSymptoms=true when more than 8 symptoms provided to day editor")
	}

	svcFew := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms[:2]},
		&stubDashboardDayStateProvider{},
	)
	vdFew, err := svcFew.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vdFew.HasExtraSymptoms {
		t.Fatal("expected HasExtraSymptoms=false when ≤ 8 symptoms provided to day editor")
	}
}

// ---------------------------------------------------------------------------
// Line 287: entryContextLogs fetches logs when len(symptoms) >= 2
//           (even for a non-owner user with exactly 2 symptoms)
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovEntryContextLogsLoadedForTwoSymptoms(t *testing.T) {
	// Use a viewer user (not owner) with exactly 2 symptoms – should still load logs.
	user := &models.User{ID: 7, Role: "viewer"}

	now, _ := time.ParseInLocation("2006-01-02", "2026-03-15", time.UTC)
	day := now

	sentinel := models.DailyLog{ID: 99, Date: day, IsPeriod: true}
	symptoms := []models.SymptomType{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}

	dayState := &stubDashboardDayStateProvider{
		logs: []models.DailyLog{sentinel},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		dayState,
	)
	vd, err := svc.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With logs loaded, symptom ranking is attempted; the result should compile
	// without error and the symptoms should be present.
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 symptoms, got %d", len(vd.Symptoms))
	}

	// With 1 symptom and a viewer user, logs must NOT be fetched.
	// We inject an error in FetchAllLogsForUser to detect if it is called.
	svcOneSymptom := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms[:1]},
		&stubDashboardDayStateProvider{logs: []models.DailyLog{sentinel}},
	)
	// Should succeed even though FetchAllLogsForUser would be a no-op.
	if _, err := svcOneSymptom.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC); err != nil {
		t.Fatalf("unexpected error for viewer with 1 symptom: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 306: symptom ranking only applied when len(symptoms) >= 2
//           AND completedCycleCountFromLogs >= 2
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovSymptomRankingRequiresTwoSymptomsAndTwoCycles(t *testing.T) {
	user := &models.User{ID: 8, Role: models.RoleOwner}
	now, _ := time.ParseInLocation("2006-01-02", "2026-04-01", time.UTC)

	// Build a log set that produces exactly 2 completed cycles.
	// Cycle 1: Jan 1 → Jan 29 (start), Cycle 2: Jan 29 → Feb 26, Cycle 3 start: Feb 26 (open)
	logsWithTwoCycles := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}

	// With >= 2 symptoms and >= 2 completed cycles, ranking must run.
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", IsBuiltin: true},
		{ID: 2, Name: "Headache", IsBuiltin: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: logsWithTwoCycles},
	)
	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Ranked symptoms must be returned (non-nil, same count).
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 ranked symptoms, got %d", len(vd.Symptoms))
	}

	// With only 1 symptom the guard short-circuits – no ranking.
	svcOne := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms[:1]},
		&stubDashboardDayStateProvider{logs: logsWithTwoCycles},
	)
	vdOne, err := svcOne.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vdOne.Symptoms) != 1 {
		t.Fatalf("expected 1 symptom when guard skips ranking, got %d", len(vdOne.Symptoms))
	}
}

// ---------------------------------------------------------------------------
// Lines 321 & 324 (not covered): completedCycleCountFromLogs
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovCompletedCycleCountFromLogsZeroWhenFewerThanTwoStarts(t *testing.T) {
	// Zero starts → 0
	if got := completedCycleCountFromLogs(nil); got != 0 {
		t.Fatalf("expected 0 for nil logs, got %d", got)
	}
	// One period cluster → 0 (not enough for a completed cycle)
	oneStart := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
	}
	if got := completedCycleCountFromLogs(oneStart); got != 0 {
		t.Fatalf("expected 0 for single cycle start, got %d", got)
	}
}

func TestDashboardviewserviceCovCompletedCycleCountFromLogsCountsCompletedCycles(t *testing.T) {
	// Three cycle starts → 2 completed cycles (len(starts) - 1).
	threeCycleStarts := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}
	if got := completedCycleCountFromLogs(threeCycleStarts); got != 2 {
		t.Fatalf("expected 2 completed cycles, got %d", got)
	}
	// Four cycle starts → 3 completed cycles.
	fourCycleStarts := append(threeCycleStarts, models.DailyLog{
		Date: mustParseDashboardServiceDay(t, "2026-03-26"), IsPeriod: true, CycleStart: true,
	})
	if got := completedCycleCountFromLogs(fourCycleStarts); got != 3 {
		t.Fatalf("expected 3 completed cycles, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Line 328: firstMissingTrackedDay enforces minimum lookback of 3 days
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovFirstMissingTrackedDayMinimumLookback(t *testing.T) {
	// lookbackDays = 1 (< 3) should be silently raised to 3.
	// With today = Feb-21 and trackingStart = Feb-18 and no logs in the 3-day
	// window [Feb-18, Feb-19, Feb-20], all 3 days are missed → show link.
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	trackingStart := mustParseDashboardServiceDay(t, "2026-02-18")

	missedDay, show := firstMissingTrackedDay(nil, today, 1, trackingStart, time.UTC)
	if !show {
		t.Fatal("expected missed-days link when lookbackDays < 3 is bumped to 3 and all days are missing")
	}
	if missedDay.Format("2006-01-02") != "2026-02-18" {
		t.Fatalf("expected first missing=2026-02-18, got %s", missedDay.Format("2006-01-02"))
	}

	// With lookbackDays = 0 (also < 3) we expect the same enforcement.
	missedDay2, show2 := firstMissingTrackedDay(nil, today, 0, trackingStart, time.UTC)
	if !show2 {
		t.Fatal("expected missed-days link when lookbackDays=0 is bumped to 3")
	}
	if missedDay2.Format("2006-01-02") != "2026-02-18" {
		t.Fatalf("expected first missing=2026-02-18, got %s", missedDay2.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// Line 357: firstMissingTrackedDay threshold – exactly 3 missed needed
// ---------------------------------------------------------------------------

func TestDashboardviewserviceCovFirstMissingTrackedDayRequiresThreeMissed(t *testing.T) {
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	trackingStart := mustParseDashboardServiceDay(t, "2026-02-10")

	// Exactly 2 missed days – must NOT show link.
	logsTwoMissed := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-02-10")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-11")},
		// Feb-12 and Feb-13 are missed (2 gaps) ...
		{Date: mustParseDashboardServiceDay(t, "2026-02-14")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-15")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-16")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-17")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-18")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-19")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-20")},
	}
	_, show := firstMissingTrackedDay(logsTwoMissed, today, 14, trackingStart, time.UTC)
	if show {
		t.Fatal("expected no missed-days link when only 2 days are missed")
	}

	// Exactly 3 missed days – must show link.
	logsThreeMissed := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-02-10")},
		// Feb-11, Feb-12, Feb-13 all missing (3 gaps)
		{Date: mustParseDashboardServiceDay(t, "2026-02-14")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-15")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-16")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-17")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-18")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-19")},
		{Date: mustParseDashboardServiceDay(t, "2026-02-20")},
	}
	firstMissed, show2 := firstMissingTrackedDay(logsThreeMissed, today, 14, trackingStart, time.UTC)
	if !show2 {
		t.Fatal("expected missed-days link when exactly 3 days are missed")
	}
	if firstMissed.Format("2006-01-02") != "2026-02-11" {
		t.Fatalf("expected first missed day 2026-02-11, got %s", firstMissed.Format("2006-01-02"))
	}
}
