package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// --- in-memory stubs with injectable errors, for the branches an integration
// DB never exercises (repository failures, best-effort refresh, defaults). ---

type importStubLogs struct {
	findErr     error
	createErr   error
	listErr     error
	dayRangeErr error
}

func (s *importStubLogs) FindByUserAndDayRange(context.Context, uint, time.Time, time.Time) (models.DailyLog, bool, error) {
	if s.findErr != nil {
		return models.DailyLog{}, false, s.findErr
	}
	return models.DailyLog{}, false, nil
}
func (s *importStubLogs) Create(context.Context, *models.DailyLog) error { return s.createErr }
func (s *importStubLogs) CreateBatch(context.Context, []models.DailyLog) error {
	return s.createErr
}
func (s *importStubLogs) ListByUser(context.Context, uint) ([]models.DailyLog, error) {
	return nil, s.listErr
}
func (s *importStubLogs) ListByUserRange(context.Context, uint, *time.Time, *time.Time) ([]models.DailyLog, error) {
	return nil, nil
}
func (s *importStubLogs) ListByUserDayRange(context.Context, uint, time.Time, time.Time) ([]models.DailyLog, error) {
	return nil, s.dayRangeErr
}
func (s *importStubLogs) Save(context.Context, *models.DailyLog) error { return nil }
func (s *importStubLogs) DeleteByUserAndDayRange(context.Context, uint, time.Time, time.Time) error {
	return nil
}

type importStubUsers struct{}

func (importStubUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}
func (importStubUsers) UpdateByID(context.Context, uint, map[string]any) error { return nil }

type importStubReconciler struct {
	fetchErr  error
	createErr error
	catalog   []models.SymptomType
}

func (s *importStubReconciler) EnsureBuiltinSymptoms(context.Context, uint) error { return nil }
func (s *importStubReconciler) FetchSymptoms(context.Context, uint) ([]models.SymptomType, error) {
	return s.catalog, s.fetchErr
}
func (s *importStubReconciler) CreateSymptomForUser(_ context.Context, userID uint, name, _, _ string) (models.SymptomType, error) {
	if s.createErr != nil {
		return models.SymptomType{}, s.createErr
	}
	return models.SymptomType{ID: 99, UserID: userID, Name: name}, nil
}

func importOneDayPayload(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-05-01", Period: true, Flow: "medium", CycleFactors: []string{}},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestImportServiceReconcileSymptomsErrorPropagates: a symptom-catalog read
// failure aborts the import and surfaces the error.
func TestImportServiceReconcileSymptomsErrorPropagates(t *testing.T) {
	svc := NewImportService(&importStubLogs{}, importStubUsers{}, &importStubReconciler{fetchErr: errors.New("catalog down")}, nil)
	if _, err := svc.ImportJSON(context.Background(), 1, importOneDayPayload(t), time.UTC); err == nil {
		t.Fatal("expected reconcile error to propagate")
	}
}

// TestImportServiceWriteFailurePropagates: both an existing-day lookup failure
// (the single range read) and an insert failure (the batch create) roll the
// write up as ErrImportWriteFailed.
func TestImportServiceWriteFailurePropagates(t *testing.T) {
	svcRead := NewImportService(&importStubLogs{dayRangeErr: errors.New("db")}, importStubUsers{}, &importStubReconciler{}, nil)
	if _, err := svcRead.ImportJSON(context.Background(), 1, importOneDayPayload(t), time.UTC); err != ErrImportWriteFailed {
		t.Fatalf("expected ErrImportWriteFailed on range-read error, got %v", err)
	}

	svcCreate := NewImportService(&importStubLogs{createErr: errors.New("db")}, importStubUsers{}, &importStubReconciler{}, nil)
	if _, err := svcCreate.ImportJSON(context.Background(), 1, importOneDayPayload(t), time.UTC); err != ErrImportWriteFailed {
		t.Fatalf("expected ErrImportWriteFailed on batch-create error, got %v", err)
	}
}

// TestImportServiceBestEffortRefreshWithDefaults exercises the non-transactional
// path (nil runner), the nil-location default, and the best-effort luteal
// refresh swallowing a repository error — the import must still succeed.
func TestImportServiceBestEffortRefreshWithDefaults(t *testing.T) {
	svc := NewImportService(&importStubLogs{listErr: errors.New("refresh down")}, importStubUsers{}, &importStubReconciler{}, nil)
	result, err := svc.ImportJSON(context.Background(), 1, importOneDayPayload(t), nil)
	if err != nil {
		t.Fatalf("best-effort refresh must not fail the import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 added, got %+v", result)
	}
}

// TestImportServiceEmptySymptomNamesIgnored: blank/whitespace custom-symptom
// names normalize to an empty key and are skipped in both the plan and the
// symptom-id resolution, without blocking the day.
func TestImportServiceEmptySymptomNamesIgnored(t *testing.T) {
	raw, _ := json.Marshal(importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-05-02", Period: false, CycleFactors: []string{}, OtherSymptoms: []string{"   ", ""}},
	}})
	svc := NewImportService(&importStubLogs{}, importStubUsers{}, &importStubReconciler{}, nil)
	result, err := svc.ImportJSON(context.Background(), 1, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 added, got %+v", result)
	}
}

// TestImportServiceRefreshNilGuards: the defensive nil guards make the
// best-effort refresh a no-op on a zero-value service (no panic).
func TestImportServiceRefreshNilGuards(t *testing.T) {
	(&ImportService{}).refreshDerivedCycleSettings(context.Background(), 1, time.UTC)
}

// importRecordingUsers records every UpdateByID call so a test can assert the
// best-effort refresh actually wrote (importStubUsers swallows the call).
type importRecordingUsers struct {
	updates []map[string]any
}

func (importRecordingUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}
func (r *importRecordingUsers) UpdateByID(_ context.Context, _ uint, updates map[string]any) error {
	r.updates = append(r.updates, updates)
	return nil
}

// TestImportServiceRefreshUpdatesOnSuccess: when the post-restore log read
// succeeds, the best-effort refresh must persist the derived luteal phase.
// Existing coverage only drives the error path (listErr), so the success path
// went unasserted — this kills the import_service.go:388 `err != nil` guard
// mutant, which returns early on success and never writes the update.
func TestImportServiceRefreshUpdatesOnSuccess(t *testing.T) {
	users := &importRecordingUsers{}
	// listErr nil => ListByUser succeeds; the refresh must proceed to UpdateByID.
	svc := &ImportService{logs: &importStubLogs{}, users: users}
	svc.refreshDerivedCycleSettings(context.Background(), 7, time.UTC)

	if len(users.updates) != 1 {
		t.Fatalf("expected one luteal_phase update on the success path, got %d", len(users.updates))
	}
	if _, ok := users.updates[0]["luteal_phase"]; !ok {
		t.Fatalf("expected the refresh to set luteal_phase, got %v", users.updates[0])
	}
}
