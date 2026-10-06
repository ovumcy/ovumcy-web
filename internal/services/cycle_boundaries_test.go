package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func boundaryDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

func boundaryPeriodRun(start time.Time, length int, flow string) []models.DailyLog {
	logs := make([]models.DailyLog, 0, length)
	for offset := range length {
		logs = append(logs, models.DailyLog{Date: start.AddDate(0, 0, offset), IsPeriod: true, Flow: flow})
	}
	return logs
}

func boundaryKeys(starts []time.Time) []string {
	keys := make([]string, 0, len(starts))
	for _, start := range starts {
		keys = append(keys, start.Format("2006-01-02"))
	}
	return keys
}

func assertBoundaries(t *testing.T, logs []models.DailyLog, ctx BoundaryContext, want ...string) {
	t.Helper()
	got := boundaryKeys(CycleBoundaries(logs, ctx))
	if len(got) != len(want) {
		t.Fatalf("boundaries = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("boundaries = %v, want %v", got, want)
		}
	}
}

func TestCycleBoundariesRules(t *testing.T) {
	today := boundaryDay(time.October, 5)
	ctx := BoundaryContext{Today: today}

	t.Run("a lone bleeding day mid-cycle opens nothing", func(t *testing.T) {
		logs := append(boundaryPeriodRun(boundaryDay(time.September, 7), 5, models.FlowMedium),
			models.DailyLog{Date: boundaryDay(time.September, 25), IsPeriod: true, Flow: models.FlowLight})
		assertBoundaries(t, logs, ctx, "2026-09-07")
	})
	t.Run("a spotting-only run opens nothing, by flow or by symptom", func(t *testing.T) {
		assertBoundaries(t, boundaryPeriodRun(boundaryDay(time.September, 1), 2, models.FlowSpotting), ctx)
		bySymptom := boundaryPeriodRun(boundaryDay(time.September, 1), 2, models.FlowNone)
		for index := range bySymptom {
			bySymptom[index].HasSpottingSymptom = true
		}
		assertBoundaries(t, bySymptom, ctx)
		// A real flow beside the symptom is a bleeding day.
		bySymptom[0].Flow, bySymptom[1].Flow = models.FlowLight, models.FlowLight
		assertBoundaries(t, bySymptom, ctx, "2026-09-01")
	})
	t.Run("spotting never opens a cycle even when marked", func(t *testing.T) {
		logs := []models.DailyLog{{Date: boundaryDay(time.September, 1), IsPeriod: true, CycleStart: true, Flow: models.FlowSpotting}}
		assertBoundaries(t, logs, ctx)
	})
	t.Run("an explicit single-day start opens", func(t *testing.T) {
		logs := []models.DailyLog{{Date: boundaryDay(time.September, 1), IsPeriod: true, CycleStart: true, Flow: models.FlowLight}}
		assertBoundaries(t, logs, ctx, "2026-09-01")
	})
	t.Run("an uncertain explicit mark does not open", func(t *testing.T) {
		logs := []models.DailyLog{{Date: boundaryDay(time.September, 1), IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowLight}}
		assertBoundaries(t, logs, ctx)
	})
	t.Run("a lone day today or yesterday opens, three days ago does not", func(t *testing.T) {
		for _, testCase := range []struct {
			date time.Time
			want []string
		}{
			{today, []string{"2026-10-05"}},
			{today.AddDate(0, 0, -1), []string{"2026-10-04"}},
			{today.AddDate(0, 0, -3), nil},
		} {
			logs := []models.DailyLog{{Date: testCase.date, IsPeriod: true, Flow: models.FlowMedium}}
			assertBoundaries(t, logs, ctx, testCase.want...)
		}
	})
	t.Run("spotting before the run is not the start", func(t *testing.T) {
		logs := append([]models.DailyLog{{Date: boundaryDay(time.September, 1), IsPeriod: true, Flow: models.FlowSpotting}},
			boundaryPeriodRun(boundaryDay(time.September, 2), 3, models.FlowMedium)...)
		assertBoundaries(t, logs, ctx, "2026-09-02")
	})
	t.Run("a non-qualifying cluster does not split the cycle around it", func(t *testing.T) {
		logs := append(boundaryPeriodRun(boundaryDay(time.August, 10), 5, models.FlowMedium), boundaryPeriodRun(boundaryDay(time.September, 7), 5, models.FlowMedium)...)
		logs = append(logs, models.DailyLog{Date: boundaryDay(time.August, 25), IsPeriod: true, Flow: models.FlowLight})
		assertBoundaries(t, logs, ctx, "2026-08-10", "2026-09-07")
	})
	t.Run("the onboarding start is a boundary and merges with the cluster it adjoins", func(t *testing.T) {
		onboarding := BoundaryContext{Today: today, OnboardingStart: boundaryDay(time.September, 14)}
		assertBoundaries(t, nil, onboarding, "2026-09-14")
		logs := boundaryPeriodRun(boundaryDay(time.September, 15), 3, models.FlowMedium)
		assertBoundaries(t, logs, onboarding, "2026-09-14")
		assertBoundaries(t, nil, BoundaryContext{Today: today, OnboardingStart: boundaryDay(time.October, 9)})
	})
}

func boundaryOwner(lmp time.Time) *models.User {
	return &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LastPeriodStart: &lmp}
}

func TestLoneMidCycleBleedingDayKeepsCountAndAnchorTogether(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0)
	for _, start := range []time.Time{boundaryDay(time.June, 15), boundaryDay(time.July, 13), boundaryDay(time.August, 10), boundaryDay(time.September, 7)} {
		logs = append(logs, boundaryPeriodRun(start, 5, models.FlowMedium)...)
	}
	logs = append(logs, models.DailyLog{Date: boundaryDay(time.September, 25), IsPeriod: true, Flow: models.FlowLight})

	stats := BuildCycleStatsFromLogs(boundaryOwner(boundaryDay(time.September, 7)), logs, now, time.UTC)
	if stats.CompletedCycleCount != 3 || stats.LastPeriodStart.Format("2006-01-02") != "2026-09-07" || stats.CurrentCycleDay != 29 || stats.NextPeriodStart.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("count=%d last=%s day=%d next=%s", stats.CompletedCycleCount, stats.LastPeriodStart.Format("2006-01-02"), stats.CurrentCycleDay, stats.NextPeriodStart.Format("2006-01-02"))
	}
}

func TestOnboardingStartCountsAsACycleBoundary(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	mark := func(day time.Time) models.DailyLog {
		return models.DailyLog{Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}
	}

	e1 := []models.DailyLog{mark(boundaryDay(time.July, 20)), mark(boundaryDay(time.August, 17))}
	if got := BuildCycleStatsFromLogs(boundaryOwner(boundaryDay(time.September, 14)), e1, now, time.UTC).CompletedCycleCount; got != 2 {
		t.Fatalf("E1 completed cycles = %d, want 2 (the onboarding start closes the August cycle)", got)
	}
	e3 := []models.DailyLog{mark(boundaryDay(time.October, 2))}
	if got := BuildCycleStatsFromLogs(boundaryOwner(boundaryDay(time.September, 14)), e3, now, time.UTC).CompletedCycleCount; got != 1 {
		t.Fatalf("E3 completed cycles = %d, want 1", got)
	}
}

func TestCalendarPaintsTheOnboardingStartAsRecorded(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	user := boundaryOwner(boundaryDay(time.September, 14))
	stats := BuildCycleStatsFromLogs(user, nil, now, time.UTC)
	for _, state := range BuildCalendarDayStates(user, boundaryDay(time.September, 1), nil, stats, now, time.UTC) {
		if state.DateString == "2026-09-14" && (!state.IsPeriod || state.HasData) {
			t.Fatalf("onboarding day: IsPeriod=%v HasData=%v, want a recorded cell with no logged data", state.IsPeriod, state.HasData)
		}
		if state.DateString == "2026-09-15" && state.IsPeriod {
			t.Fatal("only the start day is recorded, not the days after it")
		}
	}
}

type onboardingImportUsers struct {
	user    models.User
	updates []map[string]any
}

func (users *onboardingImportUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return users.user, nil
}

func (users *onboardingImportUsers) UpdateByID(_ context.Context, _ uint, updates map[string]any) error {
	users.updates = append(users.updates, updates)
	return nil
}

func TestImportRestoresTheOnboardingStartOnlyWhereTheAccountHasNone(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	raw := func(value any) json.RawMessage {
		encoded, _ := json.Marshal(value)
		return encoded
	}
	existing := boundaryDay(time.August, 1)

	for _, testCase := range []struct {
		name      string
		raw       json.RawMessage
		user      models.User
		wantWrite bool
	}{
		{"restores into a wiped account", raw("2026-09-14"), models.User{}, true},
		{"old file without the field", nil, models.User{}, false},
		{"null value", raw(nil), models.User{}, false},
		{"malformed value", raw(42), models.User{}, false},
		{"bad date", raw("not-a-date"), models.User{}, false},
		{"future date", raw("2026-12-01"), models.User{}, false},
		{"an existing start is kept", raw("2026-09-14"), models.User{LastPeriodStart: &existing}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			users := &onboardingImportUsers{user: testCase.user}
			service := &ImportService{users: users}
			if err := service.restoreOnboardingStart(context.Background(), 1, testCase.raw, now, time.UTC); err != nil {
				t.Fatal(err)
			}
			if wrote := len(users.updates) == 1; wrote != testCase.wantWrite {
				t.Fatalf("writes = %v, want write=%v", users.updates, testCase.wantWrite)
			}
			if testCase.wantWrite {
				if got := users.updates[0]["last_period_start"].(time.Time).Format("2006-01-02"); got != "2026-09-14" {
					t.Fatalf("restored %s", got)
				}
			}
		})
	}
}
