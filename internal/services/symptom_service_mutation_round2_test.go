package services

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSymptomServiceCovSeedBuiltinSymptomsSeedsWhenEmpty verifies line 219:
// when CountBuiltinByUser returns 0 (a user with no builtin symptoms yet),
// SeedBuiltinSymptoms must call CreateBatch to seed the catalog. Mutating the
// guard `count > 0` to `count >= 0` would skip seeding for count==0, leaving
// batchCreated false and failing this test.
func TestSymptomServiceCovSeedBuiltinSymptomsSeedsWhenEmpty(t *testing.T) {
	repo := &symptomserviceCovRepo{countBuiltinCnt: 0}
	service := NewSymptomService(repo)

	if err := service.SeedBuiltinSymptoms(context.Background(), 99); err != nil {
		t.Fatalf("SeedBuiltinSymptoms() unexpected error: %v", err)
	}
	if !repo.batchCreated {
		t.Fatal("expected CreateBatch to be called when builtin count is 0")
	}
}

// TestSortSymptomsEqualNameKeyPreservesInputOrder kills the
// CONDITIONALS_BOUNDARY mutant at line 435 of symptom_service.go
// (`normalizeSymptomNameKey(left.Name) < normalizeSymptomNameKey(right.Name)`
// -> `<=`).
//
// Two NON-builtin symptoms with the same normalized name key ("Custom") but
// distinct IDs route straight to the final name-key fallback (line 435). A
// valid strict-weak-ordering comparator (original `<`) returns false both ways
// for the tie, so sort.Slice (insertion sort at N=2: a single Less(1,0) check)
// leaves the pair in its input order [1 2]. The mutant `<=` returns true for
// the tie, swapping them to [2 1]. The assertion is on the returned slice order
// (by ID), never on logs/markup/error text, and is deterministic at N=2 (no
// pivots), so it does not depend on unstable-sort internals.
func TestSortSymptomsEqualNameKeyPreservesInputOrder(t *testing.T) {
	// Two NON-builtin symptoms with the same normalized name key ("Custom")
	// but distinct IDs. This routes straight to the final name-key fallback
	// (line 435). A valid strict-weak-ordering comparator (original `<`)
	// returns false both ways for the tie, so sort.Slice leaves the pair in
	// its input order. The mutant `<=` returns true both ways, swapping them.
	symptoms := []models.SymptomType{
		{ID: 1, UserID: 10, Name: "Custom"},
		{ID: 2, UserID: 10, Name: "Custom"},
	}
	SortSymptomsByBuiltinAndName(symptoms)

	if symptoms[0].ID != 1 || symptoms[1].ID != 2 {
		t.Fatalf("equal-key tie must preserve input order [1 2]; got [%d %d] "+
			"(mutant `<=` reverses tied pair to [2 1])", symptoms[0].ID, symptoms[1].ID)
	}
}

// TestSortSymptomsCaseInsensitiveEqualKeyPreservesInputOrder is a
// case-insensitive variant of the line-435 boundary kill. "Custom" vs "custom"
// still produce equal keys (normalizeSymptomNameKey lowercases), reaching line
// 435 identically. Same assertion: original keeps [1 2], mutant yields [2 1].
func TestSortSymptomsCaseInsensitiveEqualKeyPreservesInputOrder(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, UserID: 10, Name: "Custom"},
		{ID: 2, UserID: 10, Name: "custom"},
	}
	SortSymptomsByBuiltinAndName(symptoms)
	if symptoms[0].ID != 1 || symptoms[1].ID != 2 {
		t.Fatalf("case-insensitive equal-key tie must keep input order [1 2]; got [%d %d]",
			symptoms[0].ID, symptoms[1].ID)
	}
}
