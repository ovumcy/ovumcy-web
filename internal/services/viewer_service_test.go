package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubViewerDayReader struct {
	entry models.DailyLog
	logs  []models.DailyLog
	err   error
}

func (stub *stubViewerDayReader) FetchLogByDate(uint, time.Time, *time.Location) (models.DailyLog, error) {
	if stub.err != nil {
		return models.DailyLog{}, stub.err
	}
	return stub.entry, nil
}

func (stub *stubViewerDayReader) FetchLogsForUser(uint, time.Time, time.Time, *time.Location) ([]models.DailyLog, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.DailyLog, len(stub.logs))
	copy(result, stub.logs)
	return result, nil
}

type stubViewerSymptomReader struct {
	symptoms        []models.SymptomType
	lastSelectedIDs []uint
	err             error
}

func (stub *stubViewerSymptomReader) FetchPickerSymptoms(_ uint, selectedIDs []uint) ([]models.SymptomType, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	stub.lastSelectedIDs = append([]uint{}, selectedIDs...)
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

func TestViewerServiceFetchSymptomsForViewer_OnlyOwner(t *testing.T) {
	service := NewViewerService(&stubViewerDayReader{}, &stubViewerSymptomReader{
		symptoms: []models.SymptomType{{Name: "Headache"}},
	})

	ownerSymptoms, err := service.FetchSymptomsForViewer(&models.User{ID: 10, Role: models.RoleOwner}, []uint{4})
	if err != nil {
		t.Fatalf("FetchSymptomsForViewer(owner) unexpected error: %v", err)
	}
	if len(ownerSymptoms) != 1 {
		t.Fatalf("expected owner symptoms to load, got %#v", ownerSymptoms)
	}

	unsupportedSymptoms, err := service.FetchSymptomsForViewer(&models.User{ID: 11, Role: "legacy_viewer"}, []uint{4})
	if err != nil {
		t.Fatalf("FetchSymptomsForViewer(unsupported) unexpected error: %v", err)
	}
	if len(unsupportedSymptoms) != 0 {
		t.Fatalf("expected unsupported-role symptoms to be hidden, got %#v", unsupportedSymptoms)
	}
}

func TestViewerServiceFetchDayLogForViewer_SanitizesUnsupportedRoleAndSkipsSymptoms(t *testing.T) {
	service := NewViewerService(
		&stubViewerDayReader{
			entry: models.DailyLog{
				IsPeriod:   true,
				Flow:       models.FlowMedium,
				Notes:      "private",
				SymptomIDs: []uint{1, 2},
			},
		},
		&stubViewerSymptomReader{
			symptoms: []models.SymptomType{{Name: "Headache"}},
		},
	)

	logEntry, symptoms, err := service.FetchDayLogForViewer(&models.User{ID: 10, Role: "legacy_viewer"}, time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchDayLogForViewer(unsupported) unexpected error: %v", err)
	}
	if logEntry.Notes != "" || len(logEntry.SymptomIDs) != 0 {
		t.Fatalf("expected unsupported-role log to be sanitized, got %#v", logEntry)
	}
	if len(symptoms) != 0 {
		t.Fatalf("expected unsupported-role symptoms hidden, got %#v", symptoms)
	}
}

func TestViewerServiceFetchDayLogForViewer_PropagatesErrors(t *testing.T) {
	dayErr := errors.New("day fetch failed")
	service := NewViewerService(&stubViewerDayReader{err: dayErr}, &stubViewerSymptomReader{})

	_, _, err := service.FetchDayLogForViewer(&models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
	if !errors.Is(err, dayErr) {
		t.Fatalf("expected day fetch error, got %v", err)
	}

	symptomErr := errors.New("symptom fetch failed")
	service = NewViewerService(
		&stubViewerDayReader{
			entry: models.DailyLog{Notes: "owner-note"},
		},
		&stubViewerSymptomReader{err: symptomErr},
	)

	_, _, err = service.FetchDayLogForViewer(&models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
	if !errors.Is(err, symptomErr) {
		t.Fatalf("expected symptom fetch error, got %v", err)
	}
}

func TestViewerServiceFetchDayLogForViewer_PassesSelectedIDsToPicker(t *testing.T) {
	symptomReader := &stubViewerSymptomReader{
		symptoms: []models.SymptomType{{ID: 8, Name: "Custom"}},
	}
	service := NewViewerService(
		&stubViewerDayReader{
			entry: models.DailyLog{
				SymptomIDs: []uint{8, 3},
			},
		},
		symptomReader,
	)

	_, _, err := service.FetchDayLogForViewer(&models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchDayLogForViewer(owner) unexpected error: %v", err)
	}
	if len(symptomReader.lastSelectedIDs) != 2 || symptomReader.lastSelectedIDs[0] != 8 || symptomReader.lastSelectedIDs[1] != 3 {
		t.Fatalf("expected picker selected IDs [8 3], got %#v", symptomReader.lastSelectedIDs)
	}
}

func TestViewerServiceFetchLogsForViewer_SanitizesUnsupportedRoleLogs(t *testing.T) {
	service := NewViewerService(
		&stubViewerDayReader{
			logs: []models.DailyLog{
				{Notes: "private-1", SymptomIDs: []uint{1}, IsPeriod: true},
				{Notes: "private-2", SymptomIDs: []uint{2}, IsPeriod: false},
			},
		},
		&stubViewerSymptomReader{},
	)

	logs, err := service.FetchLogsForViewer(&models.User{ID: 10, Role: "legacy_viewer"}, time.Now().UTC(), time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchLogsForViewer(unsupported) unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two logs, got %#v", logs)
	}
	for _, entry := range logs {
		if entry.Notes != "" || len(entry.SymptomIDs) != 0 {
			t.Fatalf("expected unsupported-role logs sanitized, got %#v", logs)
		}
	}
}

func TestViewerServiceFetchLogByDateForViewer_SanitizesUnsupportedRoleLog(t *testing.T) {
	service := NewViewerService(
		&stubViewerDayReader{
			entry: models.DailyLog{
				Notes:      "private-note",
				SymptomIDs: []uint{1, 2},
			},
		},
		&stubViewerSymptomReader{},
	)

	logEntry, err := service.FetchLogByDateForViewer(&models.User{ID: 10, Role: "legacy_viewer"}, time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchLogByDateForViewer(unsupported) unexpected error: %v", err)
	}
	if logEntry.Notes != "" || len(logEntry.SymptomIDs) != 0 {
		t.Fatalf("expected unsupported-role log sanitized, got %#v", logEntry)
	}
}
