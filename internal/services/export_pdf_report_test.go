package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestBuildPDFReportIncludesAdvancedTrackingAndCycleSummary(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{Date: mustParseExportDay(t, "2026-01-01"), IsPeriod: true, Flow: models.FlowLight, Mood: 2, SymptomIDs: []uint{3}},
				{Date: mustParseExportDay(t, "2026-01-10"), Mood: 4, SymptomIDs: []uint{2}},
				{Date: mustParseExportDay(t, "2026-01-14"), Mood: 5, SexActivity: models.SexActivityProtected, BBT: 36.55, CervicalMucus: models.CervicalMucusEggWhite, SymptomIDs: []uint{1, 2}, Notes: "ovulation note"},
				{Date: mustParseExportDay(t, "2026-01-29"), IsPeriod: true, Flow: models.FlowMedium, Mood: 3, SymptomIDs: []uint{3}},
				{Date: mustParseExportDay(t, "2026-02-11"), Mood: 5, SymptomIDs: []uint{1}},
				{Date: mustParseExportDay(t, "2026-02-26"), IsPeriod: true, Flow: models.FlowHeavy, Mood: 1, SymptomIDs: []uint{3}},
				{Date: mustParseExportDay(t, "2026-03-11"), Mood: 4, SymptomIDs: []uint{1}},
				{Date: mustParseExportDay(t, "2026-03-26"), IsPeriod: true, Flow: models.FlowLight, Mood: 2, SymptomIDs: []uint{3}},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Acne"},
				{ID: 2, Name: "Cramps"},
				{ID: 3, Name: "Spotting"},
			},
		},
	)

	now := time.Date(2026, time.April, 1, 9, 30, 0, 0, time.UTC)
	report, err := service.BuildPDFReport(42, nil, nil, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildPDFReport() unexpected error: %v", err)
	}
	if report.GeneratedAt != now.Format(time.RFC3339) {
		t.Fatalf("expected RFC3339 generated time, got %q", report.GeneratedAt)
	}
	if report.Summary.CompletedCycles != 3 {
		t.Fatalf("expected 3 completed cycles, got %d", report.Summary.CompletedCycles)
	}
	if report.Summary.LoggedDays != 7 {
		t.Fatalf("expected 7 logged days in completed cycles, got %d", report.Summary.LoggedDays)
	}
	if !report.Summary.HasAverageMood || report.Summary.AverageMood <= 0 {
		t.Fatalf("expected average mood summary, got %#v", report.Summary)
	}
	if len(report.Cycles) != 3 {
		t.Fatalf("expected 3 exported cycles, got %d", len(report.Cycles))
	}

	firstCycle := report.Cycles[0]
	if firstCycle.StartDate != "2026-01-01" || firstCycle.EndDate != "2026-01-28" {
		t.Fatalf("unexpected first cycle window: %#v", firstCycle)
	}

	foundAdvancedTracking := false
	for _, entry := range firstCycle.Entries {
		if entry.Date != "2026-01-14" {
			continue
		}
		foundAdvancedTracking = true
		if entry.SexActivity != models.SexActivityProtected {
			t.Fatalf("expected protected sex activity, got %q", entry.SexActivity)
		}
		if entry.BBT != 36.55 {
			t.Fatalf("expected BBT 36.55, got %.2f", entry.BBT)
		}
		if entry.CervicalMucus != models.CervicalMucusEggWhite {
			t.Fatalf("expected eggwhite cervical mucus, got %q", entry.CervicalMucus)
		}
		if len(entry.Symptoms) != 2 || entry.Symptoms[0] != "Acne" || entry.Symptoms[1] != "Cramps" {
			t.Fatalf("expected sorted symptom names, got %#v", entry.Symptoms)
		}
		if entry.Notes != "ovulation note" {
			t.Fatalf("expected note to be preserved, got %q", entry.Notes)
		}
	}
	if !foundAdvancedTracking {
		t.Fatal("expected advanced tracking day in first exported cycle")
	}
}
