package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubSymptomRepo struct {
	countByIDs int64
	countErr   error
	builtinCnt int64

	createErr      error
	createBatchErr error
	findErr        error
	updateErr      error
	findResult     models.SymptomType
	created        []models.SymptomType
	updated        []models.SymptomType
	listed         []models.SymptomType
}

func (stub *stubSymptomRepo) CountBuiltinByUser(context.Context, uint) (int64, error) {
	return stub.builtinCnt, nil
}

func (stub *stubSymptomRepo) CountByUserAndIDs(context.Context, uint, []uint) (int64, error) {
	return stub.countByIDs, stub.countErr
}

func (stub *stubSymptomRepo) ListByUser(context.Context, uint) ([]models.SymptomType, error) {
	result := make([]models.SymptomType, len(stub.listed))
	copy(result, stub.listed)
	return result, nil
}

func (stub *stubSymptomRepo) Create(ctx context.Context, symptom *models.SymptomType) error {
	if stub.createErr != nil {
		return stub.createErr
	}
	if symptom.ID == 0 {
		symptom.ID = uint(len(stub.created) + 1)
	}
	stub.created = append(stub.created, *symptom)
	return nil
}

func (stub *stubSymptomRepo) CreateBatch(context.Context, []models.SymptomType) error {
	if stub.createBatchErr != nil {
		return stub.createBatchErr
	}
	stub.builtinCnt = 1
	return nil
}

func (stub *stubSymptomRepo) FindByIDForUser(context.Context, uint, uint) (models.SymptomType, error) {
	if stub.findErr != nil {
		return models.SymptomType{}, stub.findErr
	}
	return stub.findResult, nil
}

func (stub *stubSymptomRepo) Update(ctx context.Context, symptom *models.SymptomType) error {
	if stub.updateErr != nil {
		return stub.updateErr
	}
	stub.updated = append(stub.updated, *symptom)
	stub.findResult = *symptom
	return nil
}

func TestValidateSymptomIDsSortsAndDeduplicates(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{countByIDs: 3})

	ids, err := service.ValidateSymptomIDs(context.Background(), 10, []uint{3, 1, 3, 2})
	if err != nil {
		t.Fatalf("ValidateSymptomIDs() unexpected error: %v", err)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Fatalf("ValidateSymptomIDs() = %#v, want [1 2 3]", ids)
	}
}

func TestValidateSymptomIDsReturnsInvalidID(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{countByIDs: 1})

	_, err := service.ValidateSymptomIDs(context.Background(), 10, []uint{3, 1})
	if !errors.Is(err, ErrInvalidSymptomID) {
		t.Fatalf("expected ErrInvalidSymptomID, got %v", err)
	}
}

func TestCreateSymptomForUserAppliesDefaultsAndTrim(t *testing.T) {
	repo := &stubSymptomRepo{}
	service := NewSymptomService(repo)

	symptom, err := service.CreateSymptomForUser(context.Background(), 10, "  Custom  ", " ", "")
	if err != nil {
		t.Fatalf("CreateSymptomForUser() unexpected error: %v", err)
	}
	if symptom.UserID != 10 {
		t.Fatalf("expected user_id 10, got %d", symptom.UserID)
	}
	if symptom.Name != "Custom" {
		t.Fatalf("expected trimmed name Custom, got %q", symptom.Name)
	}
	if symptom.Icon != defaultSymptomIcon {
		t.Fatalf("expected default icon %q, got %q", defaultSymptomIcon, symptom.Icon)
	}
	if symptom.Color != defaultSymptomColor {
		t.Fatalf("expected default color %q, got %q", defaultSymptomColor, symptom.Color)
	}
	if !symptom.IsActive() {
		t.Fatalf("expected new custom symptom to be active")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected one create call, got %d", len(repo.created))
	}
}

func TestCreateSymptomForUserRejectsInvalidColor(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "Custom", "A", "not-color")
	if !errors.Is(err, ErrInvalidSymptomColor) {
		t.Fatalf("expected ErrInvalidSymptomColor, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsDuplicateName(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{
		listed: []models.SymptomType{
			{ID: 7, UserID: 10, Name: "Custom"},
		},
	})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "Custom", "A", "#123456")
	if !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected ErrSymptomNameAlreadyExists, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsCaseInsensitiveDuplicateName(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{
		listed: []models.SymptomType{
			{ID: 7, UserID: 10, Name: "Головокружение 2.0"},
		},
	})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "головокружение 2.0", "A", "#123456")
	if !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected ErrSymptomNameAlreadyExists, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsBuiltinDisplayNameCollision(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{}, "Усталость")

	_, err := service.CreateSymptomForUser(context.Background(), 10, "Усталость", "A", "#123456")
	if !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected built-in display name collision to return ErrSymptomNameAlreadyExists, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsMarkupLikeName(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "<script>alert('xss')</script>", "A", "#123456")
	if !errors.Is(err, ErrSymptomNameInvalidCharacters) {
		t.Fatalf("expected ErrSymptomNameInvalidCharacters, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsMarkupLikeIcon(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "Custom", "<b>", "#123456")
	if !errors.Is(err, ErrSymptomNameInvalidCharacters) {
		t.Fatalf("expected ErrSymptomNameInvalidCharacters for markup icon, got %v", err)
	}
}

func TestCreateSymptomForUserRejectsBlankAndTooLongNames(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{})

	if _, err := service.CreateSymptomForUser(context.Background(), 10, "   ", "A", "#123456"); !errors.Is(err, ErrSymptomNameRequired) {
		t.Fatalf("expected ErrSymptomNameRequired, got %v", err)
	}

	if _, err := service.CreateSymptomForUser(context.Background(), 10, "12345678901234567890123456789012345678901", "A", "#123456"); !errors.Is(err, ErrSymptomNameTooLong) {
		t.Fatalf("expected ErrSymptomNameTooLong, got %v", err)
	}
}

func TestCreateSymptomForUserReturnsTypedCreateError(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{createErr: errors.New("insert failed")})

	_, err := service.CreateSymptomForUser(context.Background(), 10, "Custom", "A", "#123456")
	if !errors.Is(err, ErrCreateSymptomFailed) {
		t.Fatalf("expected ErrCreateSymptomFailed, got %v", err)
	}
}

func TestUpdateSymptomForUserRejectsBuiltin(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{
		findResult: models.SymptomType{ID: 7, UserID: 10, IsBuiltin: true},
	})

	_, err := service.UpdateSymptomForUser(context.Background(), 10, 7, "Custom", "A", "#123456")
	if !errors.Is(err, ErrBuiltinSymptomEditForbidden) {
		t.Fatalf("expected ErrBuiltinSymptomEditForbidden, got %v", err)
	}
}

func TestUpdateSymptomForUserReturnsTypedNotFound(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{findErr: errors.New("not found")})

	_, err := service.UpdateSymptomForUser(context.Background(), 10, 7, "Custom", "A", "#123456")
	if !errors.Is(err, ErrSymptomNotFound) {
		t.Fatalf("expected ErrSymptomNotFound, got %v", err)
	}
}

func TestUpdateSymptomForUserPersistsTrimmedValues(t *testing.T) {
	repo := &stubSymptomRepo{
		findResult: models.SymptomType{
			ID:     7,
			UserID: 10,
			Name:   "Old",
			Icon:   "A",
			Color:  "#111111",
		},
	}
	service := NewSymptomService(repo)

	symptom, err := service.UpdateSymptomForUser(context.Background(), 10, 7, "  Updated  ", " ", "")
	if err != nil {
		t.Fatalf("UpdateSymptomForUser() unexpected error: %v", err)
	}
	if symptom.Name != "Updated" {
		t.Fatalf("expected updated name, got %q", symptom.Name)
	}
	if symptom.Icon != defaultSymptomIcon {
		t.Fatalf("expected default icon on blank update, got %q", symptom.Icon)
	}
	if symptom.Color != "#111111" {
		t.Fatalf("expected existing color to be preserved, got %q", symptom.Color)
	}
	if len(repo.updated) != 1 || repo.updated[0].ID != 7 {
		t.Fatalf("expected one update for symptom 7, got %#v", repo.updated)
	}
}

func TestUpdateSymptomForUserRejectsDuplicateName(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{
		findResult: models.SymptomType{
			ID:     7,
			UserID: 10,
			Name:   "Joint stiffness",
			Icon:   "A",
			Color:  "#111111",
		},
		listed: []models.SymptomType{
			{ID: 7, UserID: 10, Name: "Joint stiffness"},
			{ID: 9, UserID: 10, Name: "Joint support"},
		},
	})

	_, err := service.UpdateSymptomForUser(context.Background(), 10, 7, "JOINT SUPPORT", "A", "#123456")
	if !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected ErrSymptomNameAlreadyExists, got %v", err)
	}
}

func TestArchiveAndRestoreSymptomForUserUpdateArchivedAt(t *testing.T) {
	repo := &stubSymptomRepo{
		findResult: models.SymptomType{
			ID:     7,
			UserID: 10,
			Name:   "Custom",
			Icon:   "A",
			Color:  "#111111",
		},
	}
	service := NewSymptomService(repo)

	archivedAt := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	if err := service.ArchiveSymptomForUser(context.Background(), 10, 7, archivedAt); err != nil {
		t.Fatalf("ArchiveSymptomForUser() unexpected error: %v", err)
	}
	if len(repo.updated) != 1 || repo.updated[0].ArchivedAt == nil {
		t.Fatalf("expected archived symptom update, got %#v", repo.updated)
	}
	if repo.updated[0].ArchivedAt.Location() != time.UTC {
		t.Fatalf("expected archived_at to be normalized to UTC")
	}

	repo.findResult = repo.updated[0]
	if err := service.RestoreSymptomForUser(context.Background(), 10, 7); err != nil {
		t.Fatalf("RestoreSymptomForUser() unexpected error: %v", err)
	}
	if len(repo.updated) != 2 || repo.updated[1].ArchivedAt != nil {
		t.Fatalf("expected restored symptom to clear archived_at, got %#v", repo.updated)
	}
}

func TestArchiveAndRestoreSymptomForUserReturnTypedErrors(t *testing.T) {
	builtinService := NewSymptomService(&stubSymptomRepo{
		findResult: models.SymptomType{ID: 7, UserID: 10, IsBuiltin: true},
	})
	if err := builtinService.ArchiveSymptomForUser(context.Background(), 10, 7, time.Now().UTC()); !errors.Is(err, ErrBuiltinSymptomHideForbidden) {
		t.Fatalf("expected ErrBuiltinSymptomHideForbidden, got %v", err)
	}
	if err := builtinService.RestoreSymptomForUser(context.Background(), 10, 7); !errors.Is(err, ErrBuiltinSymptomShowForbidden) {
		t.Fatalf("expected ErrBuiltinSymptomShowForbidden, got %v", err)
	}

	notFoundService := NewSymptomService(&stubSymptomRepo{findErr: errors.New("not found")})
	if err := notFoundService.ArchiveSymptomForUser(context.Background(), 10, 7, time.Now().UTC()); !errors.Is(err, ErrSymptomNotFound) {
		t.Fatalf("expected ErrSymptomNotFound on archive, got %v", err)
	}
	if err := notFoundService.RestoreSymptomForUser(context.Background(), 10, 7); !errors.Is(err, ErrSymptomNotFound) {
		t.Fatalf("expected ErrSymptomNotFound on restore, got %v", err)
	}
}

func TestRestoreSymptomForUserRejectsDuplicateActiveName(t *testing.T) {
	repo := &stubSymptomRepo{
		findResult: models.SymptomType{
			ID:         7,
			UserID:     10,
			Name:       "Головокружение 2.0",
			Icon:       "A",
			Color:      "#111111",
			ArchivedAt: ptrSymptomTime(mustSymptomTestDay(t, "2026-03-01")),
		},
		listed: []models.SymptomType{
			{ID: 7, UserID: 10, Name: "Головокружение 2.0", ArchivedAt: ptrSymptomTime(mustSymptomTestDay(t, "2026-03-01"))},
			{ID: 9, UserID: 10, Name: "головокружение 2.0"},
		},
	}
	service := NewSymptomService(repo)

	if err := service.RestoreSymptomForUser(context.Background(), 10, 7); !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected ErrSymptomNameAlreadyExists on restore collision, got %v", err)
	}
}

func TestFetchPickerSymptomsKeepsSelectedArchivedSymptoms(t *testing.T) {
	repo := &stubSymptomRepo{
		builtinCnt: 1,
		listed: []models.SymptomType{
			{ID: 1, Name: "Headache", IsBuiltin: true},
			{ID: 2, Name: "Joint stiffness"},
			{ID: 3, Name: "Caffeine crash", ArchivedAt: ptrSymptomTime(mustSymptomTestDay(t, "2026-03-01"))},
		},
	}
	service := NewSymptomService(repo)

	filtered, err := service.FetchPickerSymptoms(context.Background(), 10, []uint{3})
	if err != nil {
		t.Fatalf("FetchPickerSymptoms() unexpected error: %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("expected active plus selected archived symptoms, got %#v", filtered)
	}

	filtered, err = service.FetchPickerSymptoms(context.Background(), 10, nil)
	if err != nil {
		t.Fatalf("FetchPickerSymptoms() unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected only active symptoms when nothing selected, got %#v", filtered)
	}
	for _, symptom := range filtered {
		if !symptom.IsActive() {
			t.Fatalf("did not expect archived symptom without selection, got %#v", filtered)
		}
	}
}

func TestCalculateFrequenciesIncludesTotalDaysContext(t *testing.T) {
	repo := &stubSymptomRepo{
		builtinCnt: 1,
		listed: []models.SymptomType{
			{ID: 1, Name: "A", Icon: "A"},
			{ID: 2, Name: "B", Icon: "B"},
		},
	}
	service := NewSymptomService(repo)

	logs := []models.DailyLog{
		{SymptomIDs: []uint{1}},
		{SymptomIDs: []uint{1, 2}},
		{SymptomIDs: []uint{}},
	}

	result, err := service.CalculateFrequencies(context.Background(), 10, logs)
	if err != nil {
		t.Fatalf("CalculateFrequencies() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 symptom frequencies, got %d", len(result))
	}
	if result[0].Name != "A" || result[0].Count != 2 {
		t.Fatalf("expected first result A x2, got %#v", result[0])
	}
	for _, item := range result {
		if item.TotalDays != len(logs) {
			t.Fatalf("expected total days %d, got %d", len(logs), item.TotalDays)
		}
	}
}

func TestCalculateFrequenciesSortsByCountThenName(t *testing.T) {
	repo := &stubSymptomRepo{
		builtinCnt: 1,
		listed: []models.SymptomType{
			{ID: 1, Name: "Beta", Icon: "B"},
			{ID: 2, Name: "Alpha", Icon: "A"},
		},
	}
	service := NewSymptomService(repo)

	logs := []models.DailyLog{
		{SymptomIDs: []uint{1}},
		{SymptomIDs: []uint{2}},
	}

	result, err := service.CalculateFrequencies(context.Background(), 10, logs)
	if err != nil {
		t.Fatalf("CalculateFrequencies() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 symptom frequencies, got %d", len(result))
	}
	if result[0].Name != "Alpha" || result[1].Name != "Beta" {
		t.Fatalf("expected alphabetical tie-break, got %#v", result)
	}
}

func mustSymptomTestDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

func ptrSymptomTime(value time.Time) *time.Time {
	return &value
}
