package services

import (
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Barrier for the entry-picker hide set.
//
// legacyEntryPickerHiddenSymptoms used to be keyed on a SPELLING of the builtin
// name, looked up through normalizeSymptomNameKey, which lowercases and
// collapses whitespace runs but never removes them. The entry "moodswings"
// therefore matched no symptom the product can create: the builtin is named
// "Mood swings" and normalizes to "mood swings", and a custom symptom never
// reaches the lookup at all. The declared set said four symptoms are kept out
// of the day-entry picker while three were, and nothing failed.
//
// The set is keyed on models.BuiltinSymptom.Key — the identity the catalogue
// already carries — and the two cases below hold the map to it from both sides:
// every key must name a real builtin, and every key must actually fire.

func builtinSymptomKeysForTest() map[string]struct{} {
	keys := make(map[string]struct{})
	for _, symptom := range models.DefaultBuiltinSymptoms() {
		keys[symptom.Key] = struct{}{}
	}
	return keys
}

// TestEveryEntryPickerHiddenKeyNamesABuiltinSymptom fails on a hide-set entry
// that matches no builtin, so a stale spelling is refused at the point it is
// written instead of quietly hiding nothing.
func TestEveryEntryPickerHiddenKeyNamesABuiltinSymptom(t *testing.T) {
	builtinKeys := builtinSymptomKeysForTest()

	// Anti-vacuity anchors owned by this test rather than read off the set
	// being judged: one spelling that must classify as a builtin key and one
	// that must not. If the catalogue is ever read wrong, both fail here before
	// the sweep below can report a clean verdict about nothing.
	if _, ok := builtinKeys["mood_swings"]; !ok {
		t.Fatal("anchor: expected mood_swings to be a builtin symptom key")
	}
	if _, ok := builtinKeys["moodswings"]; ok {
		t.Fatal("anchor: expected moodswings NOT to be a builtin symptom key")
	}

	unknown := make([]string, 0)
	for key := range legacyEntryPickerHiddenSymptoms {
		if _, ok := builtinKeys[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Fatalf("entry-picker hide set names %d key(s) that match no builtin symptom, so they hide nothing while the set claims they do: %s", len(unknown), strings.Join(unknown, ", "))
	}
}

// TestEveryEntryPickerHiddenKeyActuallyHidesItsSymptom closes the other half:
// a key can name a real builtin and still not fire if the lookup and the set
// disagree about what a key is. It drives the shipped predicate with the
// catalogue row each key names.
func TestEveryEntryPickerHiddenKeyActuallyHidesItsSymptom(t *testing.T) {
	byKey := make(map[string]models.BuiltinSymptom)
	for _, symptom := range models.DefaultBuiltinSymptoms() {
		byKey[symptom.Key] = symptom
	}

	// Anchor: a builtin that is not in the hide set must not be hidden, so a
	// predicate that answered true for everything could not pass this case.
	if shouldHideSymptomFromEntryPicker(models.SymptomType{Name: byKey["cramps"].Name, IsBuiltin: true}) {
		t.Fatal("anchor: expected Cramps to stay in the entry picker")
	}

	for key := range legacyEntryPickerHiddenSymptoms {
		builtin, ok := byKey[key]
		if !ok {
			continue // reported by the sweep above
		}
		if !shouldHideSymptomFromEntryPicker(models.SymptomType{Name: builtin.Name, IsBuiltin: true}) {
			t.Fatalf("hide set lists %q but the entry picker still shows %q", key, builtin.Name)
		}
	}
}
