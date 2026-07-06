package services

// stats_cycle_factor_context_mutation_test.go — pins a surviving mutant in
// buildStatsCycleFactorHintKeys that the baseline gremlins run reported as
// LIVED and that no existing test discriminated. Proven by per-mutant faulting
// (apply the mutation, confirm this test fails, revert):
//
//   - L219 `if len(items) < limit`  (CONDITIONALS_NEGATION) — hint-key cap
//
// buildStatsCycleFactorHintKeys returns at most statsCycleFactorHintLimit keys
// (the top-ranked factor keys). The cap is enforced by taking the smaller of
// statsCycleFactorHintLimit and len(items). Negating `<` to `>=` makes the
// method grow `limit` to len(items) whenever there are at least as many items
// as the hint limit, so all items leak into the hint list instead of just the
// top two.

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestStatsCycleFactorMut_HintKeysCappedAtHintLimit pins the hint-key cap.
// With more items than statsCycleFactorHintLimit, exactly statsCycleFactorHintLimit
// keys must be returned, and they must be the leading (highest-ranked) items.
// Under the `>=` mutation the whole item list is returned.
func TestStatsCycleFactorMut_HintKeysCappedAtHintLimit(t *testing.T) {
	if statsCycleFactorHintLimit >= 3 {
		t.Fatalf("test assumes statsCycleFactorHintLimit < 3, got %d", statsCycleFactorHintLimit)
	}

	// Three distinct ranked items (order preserved by the slice) — more than
	// the hint limit of 2.
	items := []StatsCycleFactorContextItem{
		{Key: models.CycleFactorStress, Count: 5},
		{Key: models.CycleFactorTravel, Count: 3},
		{Key: models.CycleFactorIllness, Count: 1},
	}

	keys := buildStatsCycleFactorHintKeys(items)

	if len(keys) != statsCycleFactorHintLimit {
		t.Fatalf("expected hint keys capped at %d, got %d (%v)", statsCycleFactorHintLimit, len(keys), keys)
	}
	// The cap must keep the leading items, not an arbitrary subset.
	for i := range statsCycleFactorHintLimit {
		if keys[i] != items[i].Key {
			t.Fatalf("hint key[%d] = %q, want %q (leading items must be kept)", i, keys[i], items[i].Key)
		}
	}
}

// TestStatsCycleFactorMut_HintKeysFewerThanLimitReturnsAll guards the other
// side of the cap: with fewer items than the hint limit, every item's key is
// returned (no out-of-range read, no padding).
func TestStatsCycleFactorMut_HintKeysFewerThanLimitReturnsAll(t *testing.T) {
	items := []StatsCycleFactorContextItem{
		{Key: models.CycleFactorStress, Count: 5},
	}

	keys := buildStatsCycleFactorHintKeys(items)

	if len(keys) != 1 {
		t.Fatalf("expected 1 hint key for a single item, got %d (%v)", len(keys), keys)
	}
	if keys[0] != models.CycleFactorStress {
		t.Fatalf("hint key[0] = %q, want %q", keys[0], models.CycleFactorStress)
	}
}
