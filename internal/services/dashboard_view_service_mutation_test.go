package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestEntryContextLogsFetchedForViewerWithExactlyTwoSymptoms(t *testing.T) {
	// Viewer (non-owner): the IsOwnerUser clause of requiresLogs is false, so the
	// len(symptoms) >= 2 clause alone decides whether logs are fetched. With exactly
	// 2 symptoms the original fetches logs; the boundary mutant (> 2) would not.
	// ShowSpottingCycleWarning is only reachable when logs were fetched, so it
	// distinguishes the two behaviors on an output field (not on markup/log text).
	user := &models.User{ID: 71, Role: "viewer"}
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}

	spottingDay := models.DailyLog{Date: today, IsPeriod: true, Flow: models.FlowSpotting}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{logEntry: spottingDay, symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: []models.DailyLog{spottingDay}},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !vd.ShowSpottingCycleWarning {
		t.Fatal("expected ShowSpottingCycleWarning=true: logs must be fetched for a viewer with exactly 2 symptoms")
	}
}

func TestEntryContextLogsNotSkippedForViewerWithTwoSymptoms(t *testing.T) {
	// Negating the symptom-count guard (>= becomes <) would stop a viewer with 2
	// symptoms from loading logs. ShowSpottingCycleWarning depends on fetched logs,
	// so it flips between original (true) and the negated mutant (false).
	user := &models.User{ID: 72, Role: "viewer"}
	today := mustParseDashboardServiceDay(t, "2026-03-11")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}

	spottingDay := models.DailyLog{Date: today, IsPeriod: true, Flow: models.FlowSpotting}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{logEntry: spottingDay, symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: []models.DailyLog{spottingDay}},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !vd.ShowSpottingCycleWarning {
		t.Fatal("expected ShowSpottingCycleWarning=true: a viewer with 2 symptoms must still load logs")
	}
}

func TestSymptomRankingFirstGuardReordersSymptomsByUsage(t *testing.T) {
	// 2 symptoms + 2 completed cycles (3 cycle starts) -> ranking runs and orders
	// symptoms by descending usage. Logs use symptom ID 2 twice and ID 1 never, so
	// ranked order is [2,1] while the raw input order is [1,2]. Negating or shifting
	// the first guard (len(symptoms) >= 2) skips ranking, leaving the input order.
	user := &models.User{ID: 73, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-04-01")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: logs},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 symptoms, got %d", len(vd.Symptoms))
	}
	if vd.Symptoms[0].ID != 2 || vd.Symptoms[1].ID != 1 {
		t.Fatalf("expected ranking by usage [2,1], got [%d,%d]", vd.Symptoms[0].ID, vd.Symptoms[1].ID)
	}
}

func TestSymptomRankingFirstGuardBoundaryAtTwoSymptoms(t *testing.T) {
	// Exactly 2 symptoms is the boundary for the first ranking guard. With 2
	// completed cycles, the original ranks symptoms by usage -> [2,1]; the boundary
	// mutant (len(symptoms) > 2) skips ranking and preserves the input order [1,2].
	user := &models.User{ID: 74, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-04-01")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: logs},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 symptoms, got %d", len(vd.Symptoms))
	}
	if vd.Symptoms[0].ID != 2 || vd.Symptoms[1].ID != 1 {
		t.Fatalf("expected usage ranking [2,1] at the 2-symptom boundary, got [%d,%d]", vd.Symptoms[0].ID, vd.Symptoms[1].ID)
	}
}

func TestSymptomRankingCompletedCycleBoundaryAtTwo(t *testing.T) {
	// 3 cycle starts -> exactly 2 completed cycles, the boundary for the second
	// ranking guard (completedCycleCountFromLogs(logs) >= 2). Original ranks symptoms
	// by usage -> [2,1]; the boundary mutant (> 2) skips ranking and keeps input [1,2].
	user := &models.User{ID: 75, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-04-01")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: logs},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 symptoms, got %d", len(vd.Symptoms))
	}
	if vd.Symptoms[0].ID != 2 || vd.Symptoms[1].ID != 1 {
		t.Fatalf("expected usage ranking [2,1] at exactly 2 completed cycles, got [%d,%d]", vd.Symptoms[0].ID, vd.Symptoms[1].ID)
	}
}

func TestSymptomRankingSecondGuardNegationKeepsRankingAtTwoCycles(t *testing.T) {
	// Negating the completed-cycle guard (>= becomes <) would skip ranking at 2
	// completed cycles. Original ranks by usage -> [2,1]; negated mutant keeps the
	// raw input order [1,2]. viewData.Symptoms order distinguishes them.
	user := &models.User{ID: 76, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-04-01")
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "Headache"}}
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true, SymptomIDs: []uint{2}},
		{Date: mustParseDashboardServiceDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true},
	}
	svc := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardViewerProvider{symptoms: symptoms},
		&stubDashboardDayStateProvider{logs: logs},
	)

	vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if len(vd.Symptoms) != 2 {
		t.Fatalf("expected 2 symptoms, got %d", len(vd.Symptoms))
	}
	if vd.Symptoms[0].ID != 2 || vd.Symptoms[1].ID != 1 {
		t.Fatalf("expected usage ranking [2,1] with 2 completed cycles, got [%d,%d]", vd.Symptoms[0].ID, vd.Symptoms[1].ID)
	}
}

func TestCompletedCycleCountFromLogsReturnsOneForTwoCycleStarts(t *testing.T) {
	// Exactly two observed cycle starts (two period clusters > 5 days apart) means
	// one completed cycle: len(starts) - 1 = 1. The boundary mutant (< becomes <=)
	// would instead return 0 at len(starts)==2.
	twoCycleStarts := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseDashboardServiceDay(t, "2026-01-29"), IsPeriod: true, CycleStart: true},
	}
	if got := completedCycleCountFromLogs(twoCycleStarts); got != 1 {
		t.Fatalf("expected 1 completed cycle for two cycle starts, got %d", got)
	}
}
