package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestExportImportRoundTripKeepsTheOnboardingStartAndTheDayCount: the owner's
// stored start is a cycle boundary that no logged day carries, so an export that
// dropped it would restore into an account whose cycles quietly differ. Export,
// restore into a fresh account, and the anchor and the day count both survive; an
// export written before the field existed still imports, without an anchor.
func TestExportImportRoundTripKeepsTheOnboardingStartAndTheDayCount(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)
	ctx := context.Background()

	source := createDayServiceTestUser(t, database, "onboarding-roundtrip-source@example.com")
	onboardingStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", source.ID).Update("last_period_start", onboardingStart).Error; err != nil {
		t.Fatalf("store onboarding start: %v", err)
	}
	logs := []models.DailyLog{
		{UserID: source.ID, Date: time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: source.ID, Date: time.Date(2026, time.March, 21, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create source logs: %v", err)
	}

	sourceUser, err := repositories.Users.LoadSettingsByID(ctx, source.ID)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	entries, err := exportService.BuildJSONEntries(ctx, source.ID, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("export source: %v", err)
	}
	exported := ExportOnboardingStart(&sourceUser, nil, nil)
	if exported != "2026-03-01" {
		t.Fatalf("ExportOnboardingStart = %q, want 2026-03-01", exported)
	}
	withStart, err := json.Marshal(map[string]any{"entries": entries, "last_period_start": exported})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	withoutStart, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}

	importService := newImportServiceIntegration(t, database, symptomService)

	target := createDayServiceTestUser(t, database, "onboarding-roundtrip-target@example.com")
	result, err := importService.ImportJSON(ctx, target.ID, withStart, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != len(entries) {
		t.Fatalf("added %d days, want %d", result.Added, len(entries))
	}
	restored, err := repositories.Users.LoadSettingsByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	if restored.LastPeriodStart == nil || CalendarDayKey(*restored.LastPeriodStart) != "2026-03-01" {
		t.Fatalf("restored last_period_start = %v, want 2026-03-01", restored.LastPeriodStart)
	}
	targetLogs, err := repositories.DailyLogs.ListByUser(ctx, target.ID)
	if err != nil {
		t.Fatalf("list target logs: %v", err)
	}
	if starts := CycleBoundaries(targetLogs, BoundaryContextFor(&restored, time.Time{})); len(starts) != 2 {
		t.Fatalf("restored account has %d cycle starts, want 2 (the onboarding start and the logged run)", len(starts))
	}

	legacyTarget := createDayServiceTestUser(t, database, "onboarding-roundtrip-legacy@example.com")
	legacyResult, err := importService.ImportJSON(ctx, legacyTarget.ID, withoutStart, time.UTC)
	if err != nil {
		t.Fatalf("import of an export without the field: %v", err)
	}
	if legacyResult.Added != len(entries) {
		t.Fatalf("legacy import added %d days, want %d", legacyResult.Added, len(entries))
	}
	legacy, err := repositories.Users.LoadSettingsByID(ctx, legacyTarget.ID)
	if err != nil {
		t.Fatalf("load legacy target: %v", err)
	}
	if legacy.LastPeriodStart != nil {
		t.Fatalf("legacy import set last_period_start = %v, want none", legacy.LastPeriodStart)
	}
}

// failingSettingsUsers is the real users repository with its settings read
// broken, so the restore of the onboarding start is the step that fails.
type failingSettingsUsers struct {
	DayUserRepository
}

func (failingSettingsUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, errors.New("settings read failed")
}

// TestImportJSONReportsAFailedOnboardingStartRestore: a restore that cannot put
// the onboarding start back reports a write failure instead of a success whose
// account quietly lacks a cycle boundary.
func TestImportJSONReportsAFailedOnboardingStartRestore(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	working := newImportServiceIntegration(t, database, symptomService)
	service := NewImportService(working.logs, failingSettingsUsers{DayUserRepository: repositories.Users}, symptomService, working.runInTx)

	target := createDayServiceTestUser(t, database, "onboarding-restore-failure@example.com")
	payload := []byte(`{"last_period_start":"2026-03-01","entries":[{"date":"2026-03-20","period":true,"flow":"medium","cycle_factors":[]}]}`)
	if _, err := service.ImportJSON(context.Background(), target.ID, payload, time.UTC); !errors.Is(err, ErrImportWriteFailed) {
		t.Fatalf("ImportJSON err = %v, want ErrImportWriteFailed", err)
	}
}

func TestExportOnboardingStartFollowsTheRequestedRange(t *testing.T) {
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	user := &models.User{LastPeriodStart: &start}
	before := time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC)

	if got := ExportOnboardingStart(user, nil, nil); got != "2026-03-01" {
		t.Fatalf("unbounded = %q, want 2026-03-01", got)
	}
	if got := ExportOnboardingStart(user, &before, nil); got != "" {
		t.Fatalf("range starting after the start = %q, want empty", got)
	}
	if got := ExportOnboardingStart(user, nil, &after); got != "" {
		t.Fatalf("range ending before the start = %q, want empty", got)
	}
	if got := ExportOnboardingStart(&models.User{}, nil, nil); got != "" {
		t.Fatalf("no stored start = %q, want empty", got)
	}
	if got := ExportOnboardingStart(nil, nil, nil); got != "" {
		t.Fatalf("nil user = %q, want empty", got)
	}
}

func TestWithOnboardingStartRowMarksOrInsertsInDateOrder(t *testing.T) {
	rows := []ExportCSVRow{{Date: "2026-02-10"}, {Date: "2026-03-05", Period: true}}

	marked := WithOnboardingStartRow([]ExportCSVRow{{Date: "2026-03-01", Period: true}}, "2026-03-01")
	if len(marked) != 1 || !marked[0].CycleStart || !marked[0].Period {
		t.Fatalf("a logged onboarding day must be marked in place, got %+v", marked)
	}

	inserted := WithOnboardingStartRow(rows, "2026-03-01")
	if len(inserted) != 3 || inserted[1].Date != "2026-03-01" || !inserted[1].CycleStart || inserted[1].Period {
		t.Fatalf("bare row must sit between the neighbours, got %+v", inserted)
	}
	if got := WithOnboardingStartRow([]ExportCSVRow{{Date: "2026-02-10"}}, "2026-03-01"); len(got) != 2 || got[1].Date != "2026-03-01" {
		t.Fatalf("a start after every row goes last, got %+v", got)
	}
	if got := WithOnboardingStartRow(nil, ""); got != nil {
		t.Fatalf("no start leaves the rows alone, got %+v", got)
	}
}
