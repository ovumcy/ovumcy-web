package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// symptomserviceCovRepo is a variant of the stub that can inject
// errors from CountBuiltinByUser and ListByUser, which the shared
// stubSymptomRepo does not support.
type symptomserviceCovRepo struct {
	countBuiltinErr error
	countBuiltinCnt int64
	listErr         error
	listed          []models.SymptomType
	createBatchErr  error
	batchCreated    bool

	countByIDs    int64
	countByIDsErr error

	findResult models.SymptomType
	findErr    error
	updated    []models.SymptomType
	updateErr  error
	created    []models.SymptomType
	createErr  error
}

func (r *symptomserviceCovRepo) CountBuiltinByUser(context.Context, uint) (int64, error) {
	return r.countBuiltinCnt, r.countBuiltinErr
}

func (r *symptomserviceCovRepo) CountByUserAndIDs(context.Context, uint, []uint) (int64, error) {
	return r.countByIDs, r.countByIDsErr
}

func (r *symptomserviceCovRepo) ListByUser(context.Context, uint) ([]models.SymptomType, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	result := make([]models.SymptomType, len(r.listed))
	copy(result, r.listed)
	return result, nil
}

func (r *symptomserviceCovRepo) Create(ctx context.Context, symptom *models.SymptomType) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, *symptom)
	return nil
}

func (r *symptomserviceCovRepo) CreateBatch(ctx context.Context, symptoms []models.SymptomType) error {
	if r.createBatchErr != nil {
		return r.createBatchErr
	}
	r.batchCreated = true
	return nil
}

func (r *symptomserviceCovRepo) FindByIDForUser(context.Context, uint, uint) (models.SymptomType, error) {
	if r.findErr != nil {
		return models.SymptomType{}, r.findErr
	}
	return r.findResult, nil
}

func (r *symptomserviceCovRepo) Update(ctx context.Context, symptom *models.SymptomType) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = append(r.updated, *symptom)
	return nil
}

// --- Line 205/207: CalculateFrequencies sort order ---

// TestSymptomServiceCovFrequenciesSortsByCountDescending verifies that
// CalculateFrequencies returns the highest-count symptom first (line 207:
// result[i].Count > result[j].Count). A mutation reversing the comparator
// would place B (count=1) before A (count=3).
func TestSymptomServiceCovFrequenciesSortsByCountDescending(t *testing.T) {
	repo := &symptomserviceCovRepo{
		countBuiltinCnt: 1,
		listed: []models.SymptomType{
			{ID: 10, Name: "Alpha", Icon: "A"},
			{ID: 20, Name: "Beta", Icon: "B"},
		},
	}
	service := NewSymptomService(repo)

	// Alpha appears in 3 logs; Beta in 1.
	logs := []models.DailyLog{
		{SymptomIDs: []uint{10}},
		{SymptomIDs: []uint{10}},
		{SymptomIDs: []uint{10, 20}},
	}

	result, err := service.CalculateFrequencies(context.Background(), 99, logs)
	if err != nil {
		t.Fatalf("CalculateFrequencies() unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 frequencies, got %d", len(result))
	}
	if result[0].Name != "Alpha" {
		t.Fatalf("expected higher-count symptom Alpha first, got %q", result[0].Name)
	}
	if result[0].Count != 3 {
		t.Fatalf("expected Alpha count 3, got %d", result[0].Count)
	}
	if result[1].Name != "Beta" {
		t.Fatalf("expected lower-count symptom Beta second, got %q", result[1].Name)
	}
	if result[1].Count != 1 {
		t.Fatalf("expected Beta count 1, got %d", result[1].Count)
	}
}

// TestSymptomServiceCovFrequenciesTiebreakAlphabetical verifies that equal-count
// symptoms are sorted alphabetically (line 205: result[i].Name < result[j].Name).
// Uses three tied symptoms so that a missing tie-break cannot produce the correct
// ordering by accident.
func TestSymptomServiceCovFrequenciesTiebreakAlphabetical(t *testing.T) {
	repo := &symptomserviceCovRepo{
		countBuiltinCnt: 1,
		listed: []models.SymptomType{
			{ID: 1, Name: "Zeta", Icon: "Z"},
			{ID: 2, Name: "Alpha", Icon: "A"},
			{ID: 3, Name: "Mu", Icon: "M"},
		},
	}
	service := NewSymptomService(repo)

	// Every symptom appears exactly once — all tied at count=1.
	logs := []models.DailyLog{
		{SymptomIDs: []uint{1, 2, 3}},
	}

	result, err := service.CalculateFrequencies(context.Background(), 99, logs)
	if err != nil {
		t.Fatalf("CalculateFrequencies() unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 frequencies, got %d", len(result))
	}
	if result[0].Name != "Alpha" || result[1].Name != "Mu" || result[2].Name != "Zeta" {
		t.Fatalf("expected alphabetical order [Alpha Mu Zeta], got [%q %q %q]",
			result[0].Name, result[1].Name, result[2].Name)
	}
}

// --- Line 305: ValidateSymptomIDs sort order ---

// TestSymptomServiceCovValidateSymptomIDsSortedAscending verifies that
// ValidateSymptomIDs returns IDs in ascending order (line 305).
// The existing test covers deduplication + ascending order, but this test
// uses a larger and deliberately reversed input to make a wrong comparator
// observable regardless of map-iteration order.
func TestSymptomServiceCovValidateSymptomIDsSortedAscending(t *testing.T) {
	// 5 unique IDs, repo says all 5 match.
	repo := &symptomserviceCovRepo{countByIDs: 5}
	service := NewSymptomService(repo)

	ids, err := service.ValidateSymptomIDs(context.Background(), 10, []uint{50, 10, 40, 20, 30})
	if err != nil {
		t.Fatalf("ValidateSymptomIDs() unexpected error: %v", err)
	}
	if len(ids) != 5 {
		t.Fatalf("expected 5 IDs, got %d: %v", len(ids), ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("IDs not strictly ascending at position %d: %v", i, ids)
		}
	}
	// Exact expected order.
	expected := []uint{10, 20, 30, 40, 50}
	for i, want := range expected {
		if ids[i] != want {
			t.Fatalf("ids[%d] = %d, want %d (full: %v)", i, ids[i], want, ids)
		}
	}
}

// --- Line 345: ensureSymptomNameAvailable excludeID guard ---

// TestSymptomServiceCovUpdateDoesNotConflictWithSelf verifies that a symptom
// being renamed to its own *current* name succeeds (line 345: excludeID != 0
// guard skips the symptom being updated from the duplicate check).
// If the guard is removed or inverted, the symptom's own existing name entry
// would be treated as a conflict and the update would fail with
// ErrSymptomNameAlreadyExists.
// NOTE: the name must not collide with any default builtin (which are blocked
// by the reserved-name check before line 345 is reached).
func TestSymptomServiceCovUpdateDoesNotConflictWithSelf(t *testing.T) {
	repo := &symptomserviceCovRepo{
		findResult: models.SymptomType{
			ID:     42,
			UserID: 5,
			Name:   "Joint stiffness",
			Icon:   "A",
			Color:  "#123456",
		},
		// The only existing symptom in the list is the symptom itself (ID=42).
		// A correct implementation skips this entry when excludeID == 42.
		listed: []models.SymptomType{
			{ID: 42, UserID: 5, Name: "Joint stiffness"},
		},
	}
	service := NewSymptomService(repo)

	symptom, err := service.UpdateSymptomForUser(context.Background(), 5, 42, "Joint stiffness", "A", "#123456")
	if err != nil {
		t.Fatalf("UpdateSymptomForUser() should succeed when renaming to same name, got: %v", err)
	}
	if symptom.Name != "Joint stiffness" {
		t.Fatalf("expected updated name 'Joint stiffness', got %q", symptom.Name)
	}
}

// TestSymptomServiceCovExcludeIDZeroDoesNotSkipAnySymptom verifies that when
// excludeID == 0 (e.g., during CreateSymptomForUser), every existing symptom is
// considered for duplicate detection. This is the complementary case to the one
// above: the guard `excludeID != 0` must not skip entries when excludeID is 0.
func TestSymptomServiceCovExcludeIDZeroDoesNotSkipAnySymptom(t *testing.T) {
	repo := &symptomserviceCovRepo{
		// ID=0 in the listed entry to confirm that even a symptom with ID=0
		// is NOT skipped when excludeID is also 0.
		listed: []models.SymptomType{
			{ID: 0, UserID: 7, Name: "Migraine"},
		},
	}
	service := NewSymptomService(repo)

	_, err := service.CreateSymptomForUser(context.Background(), 7, "Migraine", "A", "#aabbcc")
	if !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected ErrSymptomNameAlreadyExists for duplicate name with ID=0 entry, got %v", err)
	}
}

// --- Lines 425/426/427: SortSymptomsByBuiltinAndName builtin-order switch ---

// TestSymptomServiceCovSortBuiltinOrderPreservesCanonicalSequence verifies
// lines 425/426: when two builtin symptoms both have known canonical indices
// and their indices differ, the one with the smaller index comes first.
// Also exercises line 427: when one builtin has an unknown canonical index
// and the other does not, the known-index one comes first.
func TestSymptomServiceCovSortBuiltinOrderPreservesCanonicalSequence(t *testing.T) {
	// Use real default builtin names so they have known canonical positions.
	// "Cramps" is index 0, "Headache" is index 1 in DefaultBuiltinSymptoms.
	// Deliberately place them reversed to prove the sort moves them correctly.
	symptoms := []models.SymptomType{
		{ID: 2, Name: "Headache", IsBuiltin: true}, // canonical index 1
		{ID: 1, Name: "Cramps", IsBuiltin: true},   // canonical index 0
	}
	SortSymptomsByBuiltinAndName(symptoms)

	if symptoms[0].Name != "Cramps" {
		t.Fatalf("expected Cramps (index 0) first, got %q", symptoms[0].Name)
	}
	if symptoms[1].Name != "Headache" {
		t.Fatalf("expected Headache (index 1) second, got %q", symptoms[1].Name)
	}
}

// TestSymptomServiceCovSortBuiltinUnknownNameFallsAfterKnown verifies
// line 427: a builtin symptom whose name is not in the canonical order map
// (i.e., rightHas=false, leftHas=true) is sorted after a known builtin.
func TestSymptomServiceCovSortBuiltinUnknownNameFallsAfterKnown(t *testing.T) {
	// "Cramps" is the canonical index-0 builtin.
	// "XYZ-unknown-symptom" is not in the default catalog.
	symptoms := []models.SymptomType{
		{ID: 2, Name: "Xyz unknown symptom", IsBuiltin: true}, // not in canonical map
		{ID: 1, Name: "Cramps", IsBuiltin: true},              // canonical index 0
	}
	SortSymptomsByBuiltinAndName(symptoms)

	if symptoms[0].Name != "Cramps" {
		t.Fatalf("expected known builtin Cramps before unknown, got %q", symptoms[0].Name)
	}
	if symptoms[1].Name != "Xyz unknown symptom" {
		t.Fatalf("expected unknown builtin after known, got %q", symptoms[1].Name)
	}
}

// TestSymptomServiceCovSortBuiltinTwoUnknownNamesAlphabetically verifies the
// fallback at line 431: two builtins both lacking canonical positions are
// sorted by normalised name key.
func TestSymptomServiceCovSortBuiltinTwoUnknownNamesAlphabetical(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Zebra symptom", IsBuiltin: true},
		{ID: 2, Name: "Apple symptom", IsBuiltin: true},
	}
	SortSymptomsByBuiltinAndName(symptoms)

	if symptoms[0].Name != "Apple symptom" {
		t.Fatalf("expected Apple symptom first, got %q", symptoms[0].Name)
	}
}

// --- Lines 215/218: SeedBuiltinSymptoms error + early exit ---

// TestSymptomServiceCovSeedBuiltinSymptomsReturnsCountError verifies line 215:
// when CountBuiltinByUser returns an error, SeedBuiltinSymptoms propagates it.
func TestSymptomServiceCovSeedBuiltinSymptomsReturnsCountError(t *testing.T) {
	sentinel := errors.New("db failure")
	repo := &symptomserviceCovRepo{countBuiltinErr: sentinel}
	service := NewSymptomService(repo)

	if err := service.SeedBuiltinSymptoms(context.Background(), 99); !errors.Is(err, sentinel) {
		t.Fatalf("expected db failure error, got %v", err)
	}
}

// TestSymptomServiceCovSeedBuiltinSymptomsSkipsWhenAlreadySeeded verifies
// line 218: when CountBuiltinByUser returns count > 0, SeedBuiltinSymptoms
// returns nil without calling CreateBatch.
func TestSymptomServiceCovSeedBuiltinSymptomsSkipsWhenAlreadySeeded(t *testing.T) {
	repo := &symptomserviceCovRepo{countBuiltinCnt: 5}
	service := NewSymptomService(repo)

	if err := service.SeedBuiltinSymptoms(context.Background(), 99); err != nil {
		t.Fatalf("SeedBuiltinSymptoms() unexpected error: %v", err)
	}
	if repo.batchCreated {
		t.Fatal("expected CreateBatch to be skipped when count > 0")
	}
}
