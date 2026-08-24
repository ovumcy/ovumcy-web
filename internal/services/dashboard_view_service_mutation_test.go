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

// TestSymptomRankingGuardsAtTheirBoundaries drives the two-part ranking guard,
// `len(symptoms) >= 2 && completedCycleCountFromLogs(logs) >= 2`.
//
// Four tests stood here before, named for four different boundaries: the first
// guard's negation, the first guard's boundary at two symptoms, the completed
// cycle boundary at two, and the second guard's negation. All four built the
// same two symptoms, the same three cycle starts and the same assertion; only
// the user ID and the doc comment differed, so they exercised ONE program state
// four times and no test in the package ever stood below either guard.
//
// The rows below are that one state plus the boundary states the names claimed:
// one completed cycle, which must leave the input order alone, and the two
// above-boundary states. Ranking is observable through the order of
// viewData.Symptoms — the logs use symptom 2 twice and symptom 1 never, so
// ranked is [2,1] against an input order of [1,2].
func TestSymptomRankingGuardsAtTheirBoundaries(t *testing.T) {
	now := mustParseDashboardServiceDay(t, "2026-04-01")
	cramps := models.SymptomType{ID: 1, Name: "Cramps"}
	headache := models.SymptomType{ID: 2, Name: "Headache"}
	bloating := models.SymptomType{ID: 3, Name: "Bloating"}

	// cycleStartLogs returns `count` cycle starts a month apart; the first two
	// carry symptom 2, so usage ranking has something to reorder.
	cycleStartLogs := func(count int) []models.DailyLog {
		dates := []string{"2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"}
		logs := make([]models.DailyLog, 0, count)
		for index := range count {
			entry := models.DailyLog{
				Date:       mustParseDashboardServiceDay(t, dates[index]),
				IsPeriod:   true,
				CycleStart: true,
			}
			if index < 2 {
				entry.SymptomIDs = []uint{2}
			}
			logs = append(logs, entry)
		}
		return logs
	}

	tests := []struct {
		name      string
		userID    uint
		symptoms  []models.SymptomType
		starts    int
		wantOrder []uint
	}{
		{
			name:      "at both boundaries: two symptoms, two completed cycles",
			userID:    73,
			symptoms:  []models.SymptomType{cramps, headache},
			starts:    3,
			wantOrder: []uint{2, 1},
		},
		{
			name:      "one completed cycle is below the cycle guard",
			userID:    74,
			symptoms:  []models.SymptomType{cramps, headache},
			starts:    2,
			wantOrder: []uint{1, 2},
		},
		{
			name:      "three completed cycles is above the cycle guard",
			userID:    75,
			symptoms:  []models.SymptomType{cramps, headache},
			starts:    4,
			wantOrder: []uint{2, 1},
		},
		{
			name:      "three symptoms is above the symptom guard",
			userID:    76,
			symptoms:  []models.SymptomType{cramps, headache, bloating},
			starts:    3,
			wantOrder: []uint{2, 1, 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user := &models.User{ID: tc.userID, Role: models.RoleOwner}
			svc := NewDashboardViewService(
				&stubDashboardStatsProvider{},
				&stubDashboardViewerProvider{symptoms: tc.symptoms},
				&stubDashboardDayStateProvider{logs: cycleStartLogs(tc.starts)},
			)

			vd, err := svc.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC)
			if err != nil {
				t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
			}
			if len(vd.Symptoms) != len(tc.wantOrder) {
				t.Fatalf("expected %d symptoms, got %d", len(tc.wantOrder), len(vd.Symptoms))
			}
			for index, want := range tc.wantOrder {
				if vd.Symptoms[index].ID != want {
					got := make([]uint, 0, len(vd.Symptoms))
					for _, symptom := range vd.Symptoms {
						got = append(got, symptom.ID)
					}
					t.Fatalf("expected symptom order %v, got %v", tc.wantOrder, got)
				}
			}
		})
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
