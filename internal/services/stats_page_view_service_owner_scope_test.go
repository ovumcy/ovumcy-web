package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The stats page is the richest health-data read in the product — cycle
// history, BBT, symptoms and every insight inferred from them — and it reaches
// the repositories through four owner-scoped seams: the ranged logs, all logs,
// the frequency calculation and the symptom catalogue. An instance may host
// more than one independent owner, so each of those four must carry the
// authenticated owner's id and no other. Two owners are rendered here with
// deliberately different cycle lengths and symptom names, so a seam that
// stopped forwarding the id fails on what the page reports, not only on what
// the double recorded.

type statsOwnerScopeCase struct {
	name         string
	userID       uint
	cycleLength  int
	symptomID    uint
	symptomName  string
	wantMedian   int
	wantSymptoms string
}

func statsOwnerScopeCases() []statsOwnerScopeCase {
	return []statsOwnerScopeCase{
		{name: "first owner", userID: 11, cycleLength: 28, symptomID: 101, symptomName: "owner-a-cramps", wantMedian: 28, wantSymptoms: "owner-a-cramps"},
		{name: "second owner", userID: 22, cycleLength: 35, symptomID: 202, symptomName: "owner-b-headache", wantMedian: 35, wantSymptoms: "owner-b-headache"},
	}
}

// statsOwnerScopeLogs seeds three cycle starts of the owner's own length, the
// last of them still running, plus one symptom day inside the last completed
// cycle so the symptom-derived sections have something owner-specific to show.
func statsOwnerScopeLogs(t *testing.T, testCase statsOwnerScopeCase, anchor time.Time) []models.DailyLog {
	t.Helper()

	firstStart := anchor.AddDate(0, 0, -2*testCase.cycleLength)
	secondStart := anchor.AddDate(0, 0, -testCase.cycleLength)
	return []models.DailyLog{
		{UserID: testCase.userID, Date: firstStart, IsPeriod: true, CycleStart: true},
		{UserID: testCase.userID, Date: secondStart, IsPeriod: true, CycleStart: true},
		{UserID: testCase.userID, Date: secondStart.AddDate(0, 0, 8), SymptomIDs: []uint{testCase.symptomID}},
		{UserID: testCase.userID, Date: anchor, IsPeriod: true, CycleStart: true},
	}
}

func TestBuildStatsPageViewDataScopesEveryReadToTheSessionOwner(t *testing.T) {
	now := mustParseStatsServiceDay(t, "2026-06-01")
	anchor := now.AddDate(0, 0, -5)

	logsByOwner := make(map[uint][]models.DailyLog, len(statsOwnerScopeCases()))
	symptomsByOwner := make(map[uint][]models.SymptomType, len(statsOwnerScopeCases()))
	frequenciesByOwner := make(map[uint][]SymptomFrequency, len(statsOwnerScopeCases()))
	for _, testCase := range statsOwnerScopeCases() {
		logsByOwner[testCase.userID] = statsOwnerScopeLogs(t, testCase, anchor)
		symptomsByOwner[testCase.userID] = []models.SymptomType{{ID: testCase.symptomID, UserID: testCase.userID, Name: testCase.symptomName, Icon: "S"}}
		frequenciesByOwner[testCase.userID] = []SymptomFrequency{{Name: testCase.symptomName, Icon: "S", Count: 1, TotalDays: 1}}
	}

	for _, testCase := range statsOwnerScopeCases() {
		t.Run(testCase.name, func(t *testing.T) {
			dayReader := &stubStatsDayReader{logsByOwner: logsByOwner}
			symptomReader := &stubStatsSymptomReader{symptomsByOwner: symptomsByOwner, frequenciesByOwner: frequenciesByOwner}
			service := NewStatsService(dayReader, symptomReader)
			user := &models.User{ID: testCase.userID, Role: models.RoleOwner, CycleLength: 30, PeriodLength: 5}

			viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
			if err != nil {
				t.Fatalf("BuildStatsPageViewData: %v", err)
			}

			assertStatsReadOwner(t, "ranged logs", dayReader.gotRangeOwner, testCase.userID)
			assertStatsReadOwner(t, "all logs", dayReader.gotAllOwner, testCase.userID)
			assertStatsReadOwner(t, "frequency calculation", symptomReader.gotFrequencyOwner, testCase.userID)
			assertStatsReadOwner(t, "symptom catalogue", symptomReader.gotSymptomOwner, testCase.userID)

			if viewData.Stats.MedianCycleLength != testCase.wantMedian {
				t.Errorf("median cycle length = %d, want %d — the page reports another owner's cycle history", viewData.Stats.MedianCycleLength, testCase.wantMedian)
			}
			assertOnlySymptomName(t, "symptom counts", statsSymptomCountNames(viewData.SymptomCounts), testCase.wantSymptoms)
			assertOnlySymptomName(t, "last cycle symptoms", statsSymptomCountNames(viewData.LastCycleSymptoms), testCase.wantSymptoms)
		})
	}
}

func assertStatsReadOwner(t *testing.T, seam string, got uint, want uint) {
	t.Helper()
	if got != want {
		t.Errorf("%s read owner id = %d, want %d", seam, got, want)
	}
}

func statsSymptomCountNames(items []StatsSymptomCountViewData) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func assertOnlySymptomName(t *testing.T, section string, names []string, want string) {
	t.Helper()
	if len(names) != 1 || names[0] != want {
		t.Errorf("%s = %v, want exactly [%s]", section, names, want)
	}
}
