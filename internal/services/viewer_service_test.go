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

	// Captured userID arguments — used to prove reads are owner-scoped.
	byDateUserID  uint
	forUserUserID uint
	// Call counter — a captured id of 0 is also the zero value, so proving a
	// read never happened needs its own signal.
	calls int
}

func (stub *stubViewerDayReader) FetchLogByDate(_ context.Context, userID uint, _ time.Time, _ *time.Location) (models.DailyLog, error) {
	stub.calls++
	stub.byDateUserID = userID
	if stub.err != nil {
		return models.DailyLog{}, stub.err
	}
	return stub.entry, nil
}

func (stub *stubViewerDayReader) FetchLogsForUser(_ context.Context, userID uint, _ time.Time, _ time.Time, _ *time.Location) ([]models.DailyLog, error) {
	stub.calls++
	stub.forUserUserID = userID
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

	// Captured userID argument — used to prove reads are owner-scoped.
	symptomUserID uint
	// Call counter, for the same reason as stubViewerDayReader.calls.
	calls int
}

func (stub *stubViewerSymptomReader) FetchPickerSymptoms(_ context.Context, userID uint, selectedIDs []uint) ([]models.SymptomType, error) {
	stub.calls++
	stub.symptomUserID = userID
	if stub.err != nil {
		return nil, stub.err
	}
	stub.lastSelectedIDs = append([]uint{}, selectedIDs...)
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

// TestViewerServiceRefusesANilUser pins the precondition every method here
// carries: the acting owner is resolved before the service is reached. Each
// one used to dereference user.ID with no branch in front of it, so the
// unreachable transport path — a handler that reached a read with no resolved
// session — was a panic rather than an error, and a guard that answered with
// empty data instead would read as "this owner has nothing", which is the one
// answer a read scoped to a missing id must never give. The collaborators must
// not be touched at all: a read issued for user_id 0 is an unscoped read.
func TestViewerServiceRefusesANilUser(t *testing.T) {
	dayReader := &stubViewerDayReader{
		entry: models.DailyLog{Notes: "owner-note"},
		logs:  []models.DailyLog{{Notes: "n1"}},
	}
	symptomReader := &stubViewerSymptomReader{symptoms: []models.SymptomType{{Name: "Headache"}}}
	service := NewViewerService(dayReader, symptomReader)
	moment := time.Now().UTC()

	cases := []struct {
		name string
		call func() error
	}{
		{"FetchSymptomsForViewer", func() error {
			_, err := service.FetchSymptomsForViewer(context.Background(), nil, []uint{4})
			return err
		}},
		{"FetchLogsForViewer", func() error {
			_, err := service.FetchLogsForViewer(context.Background(), nil, moment, moment, time.UTC)
			return err
		}},
		{"FetchLogByDateForViewer", func() error {
			_, err := service.FetchLogByDateForViewer(context.Background(), nil, moment, time.UTC)
			return err
		}},
		{"FetchDayLogForViewer", func() error {
			_, _, err := service.FetchDayLogForViewer(context.Background(), nil, moment, time.UTC)
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dayReader.calls = 0
			symptomReader.calls = 0
			if err := testCase.call(); !errors.Is(err, ErrViewerUserRequired) {
				t.Fatalf("%s(nil user) error = %v, want ErrViewerUserRequired", testCase.name, err)
			}
			if dayReader.calls != 0 || symptomReader.calls != 0 {
				t.Fatalf("%s(nil user) reached its collaborators: %d day reads, %d symptom reads",
					testCase.name, dayReader.calls, symptomReader.calls)
			}
		})
	}
}

func TestViewerServiceFetchSymptomsForViewerLoadsOwnerSymptoms(t *testing.T) {
	symptomReader := &stubViewerSymptomReader{
		symptoms: []models.SymptomType{{Name: "Headache"}},
	}
	service := NewViewerService(&stubViewerDayReader{}, symptomReader)

	owner := &models.User{ID: 10, Role: models.RoleOwner}
	ownerSymptoms, err := service.FetchSymptomsForViewer(context.Background(), owner, []uint{4})
	if err != nil {
		t.Fatalf("FetchSymptomsForViewer(owner) unexpected error: %v", err)
	}
	if len(ownerSymptoms) != 1 {
		t.Fatalf("expected owner symptoms to load, got %#v", ownerSymptoms)
	}
	// The read must be scoped to the acting owner's id, never an unscoped read.
	if symptomReader.symptomUserID != owner.ID {
		t.Fatalf("expected picker read scoped to owner id %d, got %d", owner.ID, symptomReader.symptomUserID)
	}
}

func TestViewerServiceFetchLogsForViewerReturnsOwnerLogs(t *testing.T) {
	dayReader := &stubViewerDayReader{logs: []models.DailyLog{{Notes: "n1"}, {Notes: "n2"}}}
	service := NewViewerService(dayReader, &stubViewerSymptomReader{})

	owner := &models.User{ID: 10, Role: models.RoleOwner}
	logs, err := service.FetchLogsForViewer(context.Background(), owner, time.Now().UTC(), time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchLogsForViewer(owner) unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two owner logs, got %#v", logs)
	}
	// The range read must be scoped to the acting owner's id.
	if dayReader.forUserUserID != owner.ID {
		t.Fatalf("expected logs read scoped to owner id %d, got %d", owner.ID, dayReader.forUserUserID)
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
	dayReader := &stubViewerDayReader{
		entry: models.DailyLog{
			SymptomIDs: []uint{8, 3},
		},
	}
	symptomReader := &stubViewerSymptomReader{
		symptoms: []models.SymptomType{{ID: 8, Name: "Custom"}},
	}
	service := NewViewerService(dayReader, symptomReader)

	owner := &models.User{ID: 10, Role: models.RoleOwner}
	_, _, err := service.FetchDayLogForViewer(context.Background(), owner, time.Now().UTC(), time.UTC)
	if err != nil {
		t.Fatalf("FetchDayLogForViewer(owner) unexpected error: %v", err)
	}
	if len(symptomReader.lastSelectedIDs) != 2 || symptomReader.lastSelectedIDs[0] != 8 || symptomReader.lastSelectedIDs[1] != 3 {
		t.Fatalf("expected picker selected IDs [8 3], got %#v", symptomReader.lastSelectedIDs)
	}
	// Both the day read and the picker read must be scoped to the acting owner's id.
	if dayReader.byDateUserID != owner.ID {
		t.Fatalf("expected day read scoped to owner id %d, got %d", owner.ID, dayReader.byDateUserID)
	}
	if symptomReader.symptomUserID != owner.ID {
		t.Fatalf("expected picker read scoped to owner id %d, got %d", owner.ID, symptomReader.symptomUserID)
	}
}
