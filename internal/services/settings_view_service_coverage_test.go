package services

// settings_view_service_coverage_test.go
// Targets surviving mutants at lines 100, 276, 280, 348, 349 and
// the uncovered compareISODate branches at lines 314, 316.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Lines 276, 280 – resolveOwnerExportDateBounds comparison conditions
// ---------------------------------------------------------------------------

// settingsviewserviceCovExportDateBoundsEarlierFrom verifies that when
// availableSummary.DateFrom is strictly earlier than today, selectableMin is set
// to DateFrom (line 276 condition `< 0` must be satisfied).
func TestSettingsviewserviceCovExportDateBoundsEarlierFrom(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	// First call: full-range summary with DateFrom before today.
	// Second call: default-range summary.
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 3, HasData: true, DateFrom: "2026-01-15", DateTo: "2026-06-01"},
			{TotalEntries: 3, HasData: true, DateFrom: "2026-01-15", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 10, Role: models.RoleOwner}
	today := mustParseSettingsViewDay(t, "2026-06-01")

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DateFrom "2026-01-15" < today "2026-06-01" → selectableMin must become DateFrom.
	if viewData.Export.SelectableDateMin != "2026-01-15" {
		t.Fatalf("line 276: expected SelectableDateMin=2026-01-15 (DateFrom earlier than today), got %q", viewData.Export.SelectableDateMin)
	}
	// defaultFrom must also be "2026-01-15" (same as selectableMin per resolveOwnerExportDateBounds).
	if viewData.Export.DefaultDateFrom != "2026-01-15" {
		t.Fatalf("expected DefaultDateFrom=2026-01-15, got %q", viewData.Export.DefaultDateFrom)
	}
}

// settingsviewserviceCovExportDateBoundsFromNotEarlierThanToday verifies that when
// availableSummary.DateFrom equals today, selectableMin stays at today (the < 0
// condition on line 276 is NOT satisfied and the override is skipped).
func TestSettingsviewserviceCovExportDateBoundsFromEqualsToday(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	today := mustParseSettingsViewDay(t, "2026-06-01")
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 11, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DateFrom == today → compareISODate returns 0 → condition < 0 is false → selectableMin stays today.
	if viewData.Export.SelectableDateMin != "2026-06-01" {
		t.Fatalf("line 276: expected SelectableDateMin=2026-06-01 (DateFrom equals today), got %q", viewData.Export.SelectableDateMin)
	}
}

// settingsviewserviceCovExportDateBoundsLaterTo verifies that when
// availableSummary.DateTo is strictly later than today, selectableMax is set to
// DateTo (line 280 condition `> 0` must be satisfied).
func TestSettingsviewserviceCovExportDateBoundsLaterTo(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	today := mustParseSettingsViewDay(t, "2026-06-01")
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 2, HasData: true, DateFrom: "2026-05-01", DateTo: "2026-06-10"},
			{TotalEntries: 2, HasData: true, DateFrom: "2026-05-01", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 12, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DateTo "2026-06-10" > today "2026-06-01" → selectableMax must become DateTo.
	if viewData.Export.SelectableDateMax != "2026-06-10" {
		t.Fatalf("line 280: expected SelectableDateMax=2026-06-10 (DateTo later than today), got %q", viewData.Export.SelectableDateMax)
	}
}

// settingsviewserviceCovExportDateBoundsToEqualsToday verifies that when
// availableSummary.DateTo equals today, selectableMax stays at today (the > 0
// condition on line 280 is NOT satisfied).
func TestSettingsviewserviceCovExportDateBoundsToEqualsToday(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	today := mustParseSettingsViewDay(t, "2026-06-01")
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 13, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DateTo == today → compareISODate returns 0 → condition > 0 is false → selectableMax stays today.
	if viewData.Export.SelectableDateMax != "2026-06-01" {
		t.Fatalf("line 280: expected SelectableDateMax=2026-06-01 (DateTo equals today), got %q", viewData.Export.SelectableDateMax)
	}
}

// ---------------------------------------------------------------------------
// Lines 314, 316 – compareISODate branches (not covered at all)
// ---------------------------------------------------------------------------

// settingsviewserviceCovCompareISODateEqual exercises the equal branch (line 314,
// returns 0) by passing two identical trimmed strings.
func TestSettingsviewserviceCovCompareISODateEqual(t *testing.T) {
	result := compareISODate("2026-06-01", "2026-06-01")
	if result != 0 {
		t.Fatalf("compareISODate equal strings: expected 0, got %d", result)
	}
}

// settingsviewserviceCovCompareISODateEqualWithSpaces exercises line 314 via the
// TrimSpace path: leading/trailing spaces should still yield 0.
func TestSettingsviewserviceCovCompareISODateEqualWithSpaces(t *testing.T) {
	result := compareISODate("  2026-06-01  ", "2026-06-01")
	if result != 0 {
		t.Fatalf("compareISODate equal after trim: expected 0, got %d", result)
	}
}

// settingsviewserviceCovCompareISODateLess exercises the less-than branch
// (line 316, returns -1).
func TestSettingsviewserviceCovCompareISODateLess(t *testing.T) {
	result := compareISODate("2026-01-01", "2026-06-01")
	if result != -1 {
		t.Fatalf("compareISODate left < right: expected -1, got %d", result)
	}
}

// settingsviewserviceCovCompareISODateGreater exercises the default branch
// (returns 1) so all three branches are covered.
func TestSettingsviewserviceCovCompareISODateGreater(t *testing.T) {
	result := compareISODate("2026-12-31", "2026-06-01")
	if result != 1 {
		t.Fatalf("compareISODate left > right: expected 1, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// Lines 348, 349 – HasCustomSymptoms / HasArchivedSymptoms flags
// ---------------------------------------------------------------------------

// settingsviewserviceCovSymptomsAllBuiltinNoFlags verifies that when the only
// symptoms are builtin, both HasCustomSymptoms and HasArchivedSymptoms remain
// false (line 348 and 349 evaluate len == 0).
func TestSettingsviewserviceCovSymptomsAllBuiltinNoFlags(t *testing.T) {
	loader := &stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5}}
	symptomProvider := &stubSettingsViewSymptomProvider{
		symptoms: []models.SymptomType{
			{ID: 1, Name: "Cramps", IsBuiltin: true},
		},
	}
	svc := NewSettingsViewService(loader, nil, symptomProvider)
	user := &models.User{ID: 20, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-06-01"), time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if viewData.Symptoms.HasCustomSymptoms {
		t.Fatal("line 348: expected HasCustomSymptoms=false when all symptoms are builtin")
	}
	if viewData.Symptoms.HasArchivedSymptoms {
		t.Fatal("line 349: expected HasArchivedSymptoms=false when all symptoms are builtin")
	}
}

// settingsviewserviceCovSymptomsOnlyActiveCustomFlag verifies that a single active
// custom symptom sets HasCustomSymptoms=true (line 348) but HasArchivedSymptoms
// remains false (line 349).
func TestSettingsviewserviceCovSymptomsOnlyActiveCustomFlag(t *testing.T) {
	loader := &stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5}}
	symptomProvider := &stubSettingsViewSymptomProvider{
		symptoms: []models.SymptomType{
			{ID: 2, Name: "Custom active"},
		},
	}
	svc := NewSettingsViewService(loader, nil, symptomProvider)
	user := &models.User{ID: 21, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay(t, "2026-06-01"), time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !viewData.Symptoms.HasCustomSymptoms {
		t.Fatal("line 348: expected HasCustomSymptoms=true for one active custom symptom")
	}
	if viewData.Symptoms.HasArchivedSymptoms {
		t.Fatal("line 349: expected HasArchivedSymptoms=false when no archived symptoms")
	}
}

// settingsviewserviceCovSymptomsOnlyArchivedCustomFlag verifies that a single archived
// custom symptom sets HasArchivedSymptoms=true (line 349) but HasCustomSymptoms
// remains false (line 348).
func TestSettingsviewserviceCovSymptomsOnlyArchivedCustomFlag(t *testing.T) {
	loader := &stubSettingsViewLoader{user: models.User{CycleLength: 28, PeriodLength: 5}}
	archived := mustParseSettingsViewDay2("2026-05-01")
	symptomProvider := &stubSettingsViewSymptomProvider{
		symptoms: []models.SymptomType{
			{ID: 3, Name: "Custom archived", ArchivedAt: &archived},
		},
	}
	svc := NewSettingsViewService(loader, nil, symptomProvider)
	user := &models.User{ID: 22, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, mustParseSettingsViewDay2("2026-06-01"), time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if viewData.Symptoms.HasCustomSymptoms {
		t.Fatal("line 348: expected HasCustomSymptoms=false when no active custom symptoms")
	}
	if !viewData.Symptoms.HasArchivedSymptoms {
		t.Fatal("line 349: expected HasArchivedSymptoms=true for one archived custom symptom")
	}
}

// settingsviewserviceCovMustParseDay2 is a package-level helper (no *testing.T) used
// inside composite literals above.
func mustParseSettingsViewDay2(raw string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		panic("mustParseSettingsViewDay2: " + err.Error())
	}
	return parsed
}
