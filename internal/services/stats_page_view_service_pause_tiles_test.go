package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestStatsPageWithholdsTheReliabilityAndModeTilesDuringAPregnancyPause pins the
// view data behind the two tiles a pause left on /stats: the reliability card
// (graded predictions the page no longer makes) and the third stat tile (whose
// "facts only" branch speaks for the unpredictable-cycle setting). The unpaused
// twin of each case is the anchor — same history, same mode, no positive test.
// The rendered counterpart lives in internal/api.
func TestStatsPageWithholdsTheReliabilityAndModeTilesDuringAPregnancyPause(t *testing.T) {
	for index, testCase := range []struct {
		name          string
		irregularMode bool
		paused        bool
		wantReliable  bool
		wantModeCard  bool
	}{
		{name: "regular, no pause", wantReliable: true, wantModeCard: true},
		{name: "regular, paused", paused: true},
		{name: "irregular, no pause", irregularMode: true, wantReliable: true, wantModeCard: true},
		{name: "irregular, paused", irregularMode: true, paused: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			logs := statspageviewserviceCovLogsWithNCompletedCycles(t, 3)
			lastStart := logs[len(logs)-1].Date
			if testCase.paused {
				logs = append(logs, models.DailyLog{UserID: 1, Date: lastStart.AddDate(0, 0, 2), PregnancyTest: models.PregnancyTestPositive})
			}
			service := NewStatsService(&stubStatsDayReader{logsForRange: logs}, &stubStatsSymptomReader{})

			viewData, err := service.BuildStatsPageViewData(context.Background(),
				&models.User{ID: uint(500 + index), Role: models.RoleOwner, CycleLength: 28, IrregularCycle: testCase.irregularMode},
				"en", "Cycle %d", lastStart.AddDate(0, 0, 5), time.UTC, 12,
			)
			if err != nil {
				t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
			}
			if viewData.Stats.PregnancyPaused != testCase.paused {
				t.Fatalf("fixture drift: PregnancyPaused = %v, want %v", viewData.Stats.PregnancyPaused, testCase.paused)
			}
			if viewData.ShowPredictionReliability != testCase.wantReliable {
				t.Errorf("ShowPredictionReliability = %v, want %v", viewData.ShowPredictionReliability, testCase.wantReliable)
			}
			if viewData.ShowPredictionModeCard != testCase.wantModeCard {
				t.Errorf("ShowPredictionModeCard = %v, want %v", viewData.ShowPredictionModeCard, testCase.wantModeCard)
			}
		})
	}
}
