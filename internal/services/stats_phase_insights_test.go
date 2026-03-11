package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestBuildPhaseMoodInsightsRequiresThreeCompletedCycles(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	owner := &models.User{ID: 7, Role: models.RoleOwner}

	logs := buildPhaseInsightLogs(t)
	logs = logs[:len(logs)-1]

	insights, ok := service.BuildPhaseMoodInsights(owner, logs, time.UTC)
	if ok {
		t.Fatal("expected phase mood insights to stay gated with fewer than three completed cycles")
	}
	if len(insights) != 0 {
		t.Fatalf("expected no phase mood insights, got %#v", insights)
	}
}

func TestBuildPhaseMoodInsightsBuildsOrderedPhaseAverages(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	owner := &models.User{ID: 7, Role: models.RoleOwner}

	insights, ok := service.BuildPhaseMoodInsights(owner, buildPhaseInsightLogs(t), time.UTC)
	if !ok {
		t.Fatal("expected phase mood insights to be available")
	}
	if len(insights) != 4 {
		t.Fatalf("expected four phase mood insights, got %d", len(insights))
	}

	if insights[0].Phase != "menstrual" || !insights[0].HasData || insights[0].AverageMood != 2 || insights[0].Percentage != 40 {
		t.Fatalf("unexpected menstrual mood insight: %#v", insights[0])
	}
	if insights[2].Phase != "ovulation" || !insights[2].HasData || insights[2].AverageMood != 5 || insights[2].Percentage != 100 {
		t.Fatalf("unexpected ovulation mood insight: %#v", insights[2])
	}
}

func TestBuildPhaseSymptomInsightsSortsAndTruncatesTopThree(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{
		symptoms: []models.SymptomType{
			{ID: 1, Name: "Acne", Icon: "A"},
			{ID: 2, Name: "Bloating", Icon: "B"},
			{ID: 3, Name: "Cramps", Icon: "C"},
			{ID: 4, Name: "Headache", Icon: "H"},
		},
	})
	owner := &models.User{ID: 7, Role: models.RoleOwner}

	insights, ok, err := service.BuildPhaseSymptomInsights(owner, buildPhaseInsightLogs(t), time.UTC)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !ok {
		t.Fatal("expected phase symptom insights to be available")
	}
	if len(insights) != 4 {
		t.Fatalf("expected four phase symptom insights, got %d", len(insights))
	}

	luteal := insights[3]
	if luteal.Phase != "luteal" || !luteal.HasData || luteal.TotalDays != 3 {
		t.Fatalf("unexpected luteal insight header: %#v", luteal)
	}
	if len(luteal.Items) != 3 {
		t.Fatalf("expected top three luteal symptoms, got %#v", luteal.Items)
	}
	if luteal.Items[0].Name != "Acne" || luteal.Items[0].Count != 3 {
		t.Fatalf("expected Acne to lead luteal insight, got %#v", luteal.Items[0])
	}
	if luteal.Items[1].Name != "Cramps" || luteal.Items[1].Count != 3 {
		t.Fatalf("expected Cramps to be second luteal insight, got %#v", luteal.Items[1])
	}
	if luteal.Items[2].Name != "Bloating" || luteal.Items[2].Count != 2 {
		t.Fatalf("expected Bloating to be third luteal insight, got %#v", luteal.Items[2])
	}
}

func buildPhaseInsightLogs(t *testing.T) []models.DailyLog {
	t.Helper()

	return []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, Mood: 1, SymptomIDs: []uint{3}},
		{Date: mustParseStatsServiceDay(t, "2026-01-10"), Mood: 4, SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-01-14"), Mood: 5, SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-01-20"), Mood: 3, SymptomIDs: []uint{3, 2, 1, 4}},
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true, Mood: 2, SymptomIDs: []uint{3}},
		{Date: mustParseStatsServiceDay(t, "2026-02-07"), Mood: 4, SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-02-11"), Mood: 5, SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-02-18"), Mood: 3, SymptomIDs: []uint{3, 2, 1}},
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true, Mood: 3, SymptomIDs: []uint{3}},
		{Date: mustParseStatsServiceDay(t, "2026-03-07"), Mood: 4, SymptomIDs: []uint{2}},
		{Date: mustParseStatsServiceDay(t, "2026-03-11"), Mood: 5, SymptomIDs: []uint{1}},
		{Date: mustParseStatsServiceDay(t, "2026-03-18"), Mood: 3, SymptomIDs: []uint{3, 1}},
		{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true, Mood: 2, SymptomIDs: []uint{3}},
	}
}
