package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The out-of-date verdict is printed by the dashboard (CycleContext), by the
// stats page (StatsFlags) and published on the JSON API (cycle_data_stale). Each
// one forces the phase and the fertility status to "unknown" on its surface, so
// two of them disagreeing is one page calling current what another withholds.
// A pregnancy pause and unpredictable-cycle mode publish no projection, and all
// three must answer false there even though the anchor is past the reference
// length.
func TestCycleDataStaleVerdictIsOneAcrossDashboardStatsPageAndAPI(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})

	for _, testCase := range []struct {
		name  string
		user  func(*models.User)
		logs  func([]models.DailyLog) []models.DailyLog
		today string
		want  bool
		// rawStale is the length check alone, pinned so a pause or an
		// unpredictable account is one the pause branch actually decides.
		rawStale bool
	}{
		{name: "current data", today: "2026-06-10", want: false, rawStale: false},
		{name: "out-of-date data", today: "2026-06-30", want: true, rawStale: true},
		{name: "overdue", today: "2026-07-10", want: true, rawStale: true},
		{name: "irregular mode out of date", user: func(user *models.User) { user.IrregularCycle = true }, today: "2026-06-30", want: true, rawStale: true},
		{
			name:  "awaiting the first completed cycle",
			logs:  func([]models.DailyLog) []models.DailyLog { return cycleStartLogs(t, "2026-05-31") },
			today: "2026-06-30", want: true, rawStale: true,
		},
		{
			name: "pregnancy pause",
			logs: func(logs []models.DailyLog) []models.DailyLog {
				return append(logs, models.DailyLog{Date: mustParseDay(t, "2026-06-20"), PregnancyTest: models.PregnancyTestPositive})
			},
			today: "2026-06-30", want: false, rawStale: true,
		},
		{name: "unpredictable cycle", user: func(user *models.User) { user.UnpredictableCycle = true }, today: "2026-06-30", want: false, rawStale: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			user, logs, _ := staleBandFixture(t)
			if testCase.user != nil {
				testCase.user(user)
			}
			if testCase.logs != nil {
				logs = testCase.logs(logs)
			}
			today := mustParseDay(t, testCase.today)
			stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)

			raw := DashboardCycleDataLooksStale(DashboardCycleStaleAnchor(user, stats, time.UTC), today, DashboardCycleReferenceLength(user, stats))
			if raw != testCase.rawStale {
				t.Fatalf("fixture: the length check alone says stale=%v, want %v", raw, testCase.rawStale)
			}
			switch testCase.name {
			case "pregnancy pause":
				if !stats.PregnancyPaused {
					t.Fatal("fixture: the positive test must pause predictions")
				}
			case "unpredictable cycle":
				if !DashboardPredictionDisabled(user) {
					t.Fatal("fixture: unpredictable-cycle mode must disable predictions")
				}
			case "overdue":
				if !DashboardCycleOverdue(user, stats) {
					t.Fatal("fixture: the cycle must be overdue")
				}
			}

			dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC).CycleDataStale
			statsPage := service.BuildFlags(user, logs, stats, today, time.UTC, 0).CycleDataStale
			published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)

			if dashboard != testCase.want || statsPage != testCase.want || published.CycleDataStale != testCase.want {
				t.Fatalf("out of date: dashboard %v, stats page %v, cycle_data_stale %v; want %v on all three",
					dashboard, statsPage, published.CycleDataStale, testCase.want)
			}
		})
	}
}
