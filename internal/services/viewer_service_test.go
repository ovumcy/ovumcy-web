package services

import (
	"context"
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

func (stub *stubViewerDayReader) FetchLogByDate(context.Context, uint, time.Time, *time.Location) (models.DailyLog, error) {
	if stub.err != nil {
		return models.DailyLog{}, stub.err
	}
	return stub.entry, nil
}

func (stub *stubViewerDayReader) FetchLogsForUser(context.Context, uint, time.Time, time.Time, *time.Location) ([]models.DailyLog, error) {
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

func (stub *stubViewerSymptomReader) FetchPickerSymptoms(ctx context.Context, _ uint, selectedIDs []uint) ([]models.SymptomType, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	stub.lastSelectedIDs = append([]uint{}, selectedIDs...)
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

func TestViewerServiceFetchSymptomsForViewerLoadsOwnerSymptoms(t *testing.T) {
	service := NewViewerService(&stubViewerDayReader{}, &stubViewerSymptomReader{
		symptoms: []models.SymptomType{{Name: "Headache"}},
	})

	ownerSymptoms, err := service.FetchSymptomsForViewer(context.Background(), &models.User{ID: 10, Role: models.RoleOwner}, []uint{4})
	if err != nil {
		t.Fatalf("FetchSymptomsForViewer(owner) unexpected error: %v", err)
	}
	if len(ownerSymptoms) != 1 {
		t.Fatalf("expected owner symptoms to load, got %#v", ownerSymptoms)
	}
}

func TestViewerServiceFetchLogsForViewerReturnsOwnerLogs(t *testing.T) {
	service := NewViewerService(
		&stubViewerDayReader{logs: []models.DailyLog{{Notes: "n1"}, {Notes: "n2"}}},
		&stubViewerSymptomReader{},
	)

	logs, err := service.FetchLogsForViewer(context.Background(), &models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchLogsForViewer(owner) unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two owner logs, got %#v", logs)
	}
}

func TestViewerServiceFetchDayLogForViewer_PropagatesErrors(t *testing.T) {
	dayErr := errors.New("day fetch failed")
	service := NewViewerService(&stubViewerDayReader{err: dayErr}, &stubViewerSymptomReader{})

	_, _, err := service.FetchDayLogForViewer(context.Background(), &models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
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

	_, _, err = service.FetchDayLogForViewer(context.Background(), &models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
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

	_, _, err := service.FetchDayLogForViewer(context.Background(), &models.User{ID: 10, Role: models.RoleOwner}, time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchDayLogForViewer(owner) unexpected error: %v", err)
	}
	if len(symptomReader.lastSelectedIDs) != 2 || symptomReader.lastSelectedIDs[0] != 8 || symptomReader.lastSelectedIDs[1] != 3 {
		t.Fatalf("expected picker selected IDs [8 3], got %#v", symptomReader.lastSelectedIDs)
	}
}
