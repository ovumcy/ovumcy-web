package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubExportDayReader struct {
	logs []models.DailyLog
	err  error
}

func (stub *stubExportDayReader) FetchLogsForOptionalRange(uint, *time.Time, *time.Time, *time.Location) ([]models.DailyLog, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.DailyLog, len(stub.logs))
	copy(result, stub.logs)
	return result, nil
}

type stubExportSymptomReader struct {
	symptoms []models.SymptomType
	err      error
}

func (stub *stubExportSymptomReader) FetchSymptoms(uint) ([]models.SymptomType, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

func TestExportBuildSummaryUsesDateBounds(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{Date: mustParseExportDay(t, "2026-02-20")},
				{Date: mustParseExportDay(t, "2026-02-07")},
				{Date: mustParseExportDay(t, "2026-02-12")},
			},
		},
		&stubExportSymptomReader{},
	)

	summary, err := service.BuildSummary(42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary() unexpected error: %v", err)
	}
	if !summary.HasData {
		t.Fatalf("expected summary.HasData=true")
	}
	if summary.TotalEntries != 3 {
		t.Fatalf("expected TotalEntries=3, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "2026-02-07" {
		t.Fatalf("expected DateFrom=2026-02-07, got %q", summary.DateFrom)
	}
	if summary.DateTo != "2026-02-20" {
		t.Fatalf("expected DateTo=2026-02-20, got %q", summary.DateTo)
	}
}

func TestExportBuildSummaryReturnsEmptyForNoLogs(t *testing.T) {
	service := NewExportService(&stubExportDayReader{logs: []models.DailyLog{}}, &stubExportSymptomReader{})
	summary, err := service.BuildSummary(42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary() unexpected error: %v", err)
	}
	if summary.HasData {
		t.Fatalf("expected summary.HasData=false")
	}
	if summary.TotalEntries != 0 {
		t.Fatalf("expected TotalEntries=0, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "" || summary.DateTo != "" {
		t.Fatalf("expected empty date range, got %q..%q", summary.DateFrom, summary.DateTo)
	}
}

func TestExportBuildJSONEntriesNormalizesFlowAndMapsSymptoms(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:            mustParseExportDay(t, "2026-02-19"),
					Flow:            "unexpected-flow",
					Mood:            4,
					SexActivity:     models.SexActivityProtected,
					BBT:             36.55,
					CervicalMucus:   models.CervicalMucusEggWhite,
					CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorTravel},
					SymptomIDs:      []uint{1, 2, 3, 3},
					Notes:           "json-note",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Mood swings"},
				{ID: 2, Name: "My Custom"},
				{ID: 3, Name: "Another Custom"},
			},
		},
	)

	entries, err := service.BuildJSONEntries(42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildJSONEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}

	entry := entries[0]
	assertExportJSONEntryCoreFields(t, entry)
	assertExportJSONEntryTrackingFields(t, entry)
	assertExportJSONEntrySymptomFields(t, entry)
}

func TestExportBuildCSVRowsBuildsExpectedColumns(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:            mustParseExportDay(t, "2026-02-18"),
					IsPeriod:        true,
					Flow:            models.FlowLight,
					Mood:            5,
					SexActivity:     models.SexActivityUnprotected,
					BBT:             36.7,
					CervicalMucus:   models.CervicalMucusCreamy,
					CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorMedicationChange},
					SymptomIDs:      []uint{1, 2},
					Notes:           "note",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Cramps"},
				{ID: 2, Name: "Custom Symptom"},
			},
		},
	)

	rows, err := service.BuildCSVRows(42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	if len(columns) != len(ExportCSVHeaders) {
		t.Fatalf("expected %d csv columns, got %d", len(ExportCSVHeaders), len(columns))
	}
	assertExportCSVFixedColumns(t, columns)
	indexByHeader := exportCSVIndexByHeader()
	assertExportCSVTrackingColumns(t, columns, indexByHeader)
	assertExportCSVSymptomColumns(t, columns, indexByHeader)
}

func TestExportBuildCSVRowsNeutralizesFormulaLikeCells(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:       mustParseExportDay(t, "2026-02-18"),
					SymptomIDs: []uint{1},
					Notes:      "  =cmd|' /C calc'!A0",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "@Doctor export"},
			},
		},
	)

	rows, err := service.BuildCSVRows(42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	indexByHeader := make(map[string]int, len(ExportCSVHeaders))
	for index, header := range ExportCSVHeaders {
		indexByHeader[header] = index
	}

	if columns[indexByHeader["Other"]] != "'@Doctor export" {
		t.Fatalf("expected sanitized other symptom cell, got %q", columns[indexByHeader["Other"]])
	}
	if columns[indexByHeader["Notes"]] != "'  =cmd|' /C calc'!A0" {
		t.Fatalf("expected sanitized notes cell, got %q", columns[indexByHeader["Notes"]])
	}
}

func TestExportServicePropagatesDependencyErrors(t *testing.T) {
	dayErrService := NewExportService(
		&stubExportDayReader{err: errors.New("load failed")},
		&stubExportSymptomReader{},
	)
	if _, err := dayErrService.BuildSummary(1, nil, nil, time.UTC); err == nil {
		t.Fatalf("expected summary error when day reader fails")
	}

	symptomErrService := NewExportService(
		&stubExportDayReader{logs: []models.DailyLog{{Date: mustParseExportDay(t, "2026-02-18")}}},
		&stubExportSymptomReader{err: errors.New("symptom load failed")},
	)
	if _, err := symptomErrService.BuildJSONEntries(1, nil, nil, time.UTC); err == nil {
		t.Fatalf("expected json entries error when symptom reader fails")
	}
}

func mustParseExportDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

func assertExportJSONEntryCoreFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if entry.Date != "2026-02-19" {
		t.Fatalf("expected Date=2026-02-19, got %q", entry.Date)
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected normalized flow=%q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.Notes != "json-note" {
		t.Fatalf("expected notes preserved, got %q", entry.Notes)
	}
}

func assertExportJSONEntryTrackingFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if entry.MoodRating != 4 {
		t.Fatalf("expected mood rating 4, got %d", entry.MoodRating)
	}
	if entry.SexActivity != models.SexActivityProtected {
		t.Fatalf("expected protected sex activity, got %q", entry.SexActivity)
	}
	if entry.BBT != 36.55 {
		t.Fatalf("expected BBT 36.55, got %.2f", entry.BBT)
	}
	if entry.CervicalMucus != models.CervicalMucusEggWhite {
		t.Fatalf("expected eggwhite cervical mucus, got %q", entry.CervicalMucus)
	}
	if len(entry.CycleFactors) != 2 || entry.CycleFactors[0] != models.CycleFactorStress || entry.CycleFactors[1] != models.CycleFactorTravel {
		t.Fatalf("expected normalized cycle factors, got %#v", entry.CycleFactors)
	}
}

func assertExportJSONEntrySymptomFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if !entry.Symptoms.Mood {
		t.Fatalf("expected mood flag=true")
	}
	if len(entry.OtherSymptoms) != 2 || entry.OtherSymptoms[0] != "Another Custom" || entry.OtherSymptoms[1] != "My Custom" {
		t.Fatalf("expected sorted deduped other symptoms, got %#v", entry.OtherSymptoms)
	}
}

func assertExportCSVFixedColumns(t *testing.T, columns []string) {
	t.Helper()

	if columns[0] != "2026-02-18" || columns[1] != "Yes" || columns[2] != "Light" {
		t.Fatalf("unexpected fixed csv columns: %#v", columns[:3])
	}
}

func assertExportCSVTrackingColumns(t *testing.T, columns []string, indexByHeader map[string]int) {
	t.Helper()

	if columns[indexByHeader["Mood rating"]] != "5" {
		t.Fatalf("expected mood rating column 5, got %q", columns[indexByHeader["Mood rating"]])
	}
	if columns[indexByHeader["Sex activity"]] != "Unprotected" {
		t.Fatalf("expected sex activity column Unprotected, got %q", columns[indexByHeader["Sex activity"]])
	}
	if columns[indexByHeader["BBT (C)"]] != "36.70" {
		t.Fatalf("expected BBT column 36.70, got %q", columns[indexByHeader["BBT (C)"]])
	}
	if columns[indexByHeader["Cervical mucus"]] != "Creamy" {
		t.Fatalf("expected cervical mucus column Creamy, got %q", columns[indexByHeader["Cervical mucus"]])
	}
	if columns[indexByHeader["Cycle factors"]] != "Stress; Medication change" {
		t.Fatalf("expected cycle factors column, got %q", columns[indexByHeader["Cycle factors"]])
	}
}

func assertExportCSVSymptomColumns(t *testing.T, columns []string, indexByHeader map[string]int) {
	t.Helper()

	if columns[indexByHeader["Cramps"]] != "Yes" {
		t.Fatalf("expected cramps column Yes, got %q", columns[indexByHeader["Cramps"]])
	}
	if columns[indexByHeader["Other"]] != "Custom Symptom" {
		t.Fatalf("expected other symptom column, got %q", columns[indexByHeader["Other"]])
	}
	if columns[indexByHeader["Notes"]] != "note" {
		t.Fatalf("expected notes column, got %q", columns[indexByHeader["Notes"]])
	}
}

func exportCSVIndexByHeader() map[string]int {
	indexByHeader := make(map[string]int, len(ExportCSVHeaders))
	for index, header := range ExportCSVHeaders {
		indexByHeader[header] = index
	}
	return indexByHeader
}
