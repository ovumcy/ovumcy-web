package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// -- RankSymptomsForEntryPicker: uncovered lines 10, 19, 26 --

// TestSymptomPickerViewRankEarlyReturnSingleSymptom exercises line 10
// (len(symptoms) < 2) and verifies the early-return copy path.
func TestSymptomPickerViewRankEarlyReturnSingleSymptom(t *testing.T) {
	symptoms := []models.SymptomType{{ID: 1, Name: "Cramps"}}
	logs := []models.DailyLog{{ID: 1, SymptomIDs: []uint{1}}}

	result := RankSymptomsForEntryPicker(symptoms, logs)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].ID != 1 {
		t.Fatalf("expected ID 1, got %d", result[0].ID)
	}
}

// TestSymptomPickerViewRankEarlyReturnEmptyLogs exercises line 10
// (len(logs) == 0) and verifies the early-return copy path preserves all
// symptoms.
func TestSymptomPickerViewRankEarlyReturnEmptyLogs(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps"},
		{ID: 2, Name: "Fatigue"},
		{ID: 3, Name: "Headache"},
	}

	result := RankSymptomsForEntryPicker(symptoms, []models.DailyLog{})
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
}

// TestSymptomPickerViewRankByFrequency exercises lines 19 and 26.
// The symptom that appears in more logs must sort to the front.
func TestSymptomPickerViewRankByFrequency(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps"},
		{ID: 2, Name: "Fatigue"},
		{ID: 3, Name: "Headache"},
	}
	// ID 2 appears in 2 logs, ID 1 and ID 3 appear in 1 log each.
	logs := []models.DailyLog{
		{ID: 1, SymptomIDs: []uint{1, 2}},
		{ID: 2, SymptomIDs: []uint{2, 3}},
	}

	result := RankSymptomsForEntryPicker(symptoms, logs)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != 2 {
		t.Fatalf("expected highest-frequency symptom (ID 2) first, got ID %d; full order: %v",
			result[0].ID, result)
	}
}

// TestSymptomPickerViewRankStable ensures the sort is stable:
// two symptoms with the same frequency keep their original relative order.
func TestSymptomPickerViewRankStable(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "A"},
		{ID: 2, Name: "B"},
		{ID: 3, Name: "C"},
	}
	// All three appear exactly once — relative order must be preserved.
	logs := []models.DailyLog{
		{ID: 1, SymptomIDs: []uint{1, 2, 3}},
	}

	result := RankSymptomsForEntryPicker(symptoms, logs)
	if result[0].ID != 1 || result[1].ID != 2 || result[2].ID != 3 {
		t.Fatalf("expected stable order [1 2 3], got %v", result)
	}
}

// TestSymptomPickerViewRankDoesNotMutateInput verifies that the original
// slice passed in is not reordered by the ranking function.
func TestSymptomPickerViewRankDoesNotMutateInput(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps"},
		{ID: 2, Name: "Fatigue"},
		{ID: 3, Name: "Headache"},
	}
	// ID 3 is highest frequency, so the result would be reordered — but the
	// input must remain untouched.
	logs := []models.DailyLog{
		{ID: 1, SymptomIDs: []uint{3}},
		{ID: 2, SymptomIDs: []uint{3}},
	}

	_ = RankSymptomsForEntryPicker(symptoms, logs)

	if symptoms[0].ID != 1 || symptoms[1].ID != 2 || symptoms[2].ID != 3 {
		t.Fatalf("RankSymptomsForEntryPicker must not modify input slice, got %v", symptoms)
	}
}

// -- SplitSymptomsForCollapsedPicker: surviving mutant on line 32 --

// TestSymptomPickerViewSplitZeroPrimaryLimit exercises the primaryLimit == 0
// boundary that a mutation of `<= 0` to `< 0` would bypass.  When limit is
// exactly 0 the early-return path must fire and all symptoms are in primary.
func TestSymptomPickerViewSplitZeroPrimaryLimit(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", IsBuiltin: true},
		{ID: 2, Name: "Fatigue", IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{}, 0)
	if len(primary) != 2 {
		t.Fatalf("expected all 2 symptoms in primary when limit=0, got %d", len(primary))
	}
	if len(extra) != 0 {
		t.Fatalf("expected empty extra when limit=0, got %d", len(extra))
	}
}

// TestSymptomPickerViewSplitNegativePrimaryLimit ensures negative limit is also
// treated as the early-return path (boundary of `<= 0`).
func TestSymptomPickerViewSplitNegativePrimaryLimit(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{}, -1)
	if len(primary) != 1 {
		t.Fatalf("expected 1 symptom in primary when limit=-1, got %d", len(primary))
	}
	if len(extra) != 0 {
		t.Fatalf("expected empty extra when limit=-1, got %d", len(extra))
	}
}

// -- SplitSymptomsForCollapsedPicker: surviving mutant on line 60 --

// TestSymptomPickerViewSplitCustomFillsExactLimit exercises line 60 exactly at
// the boundary: a custom symptom fills the last available primary slot.
// A mutation that changes `<` to `<=` would leave that last slot unfilled —
// len(primary) would be one short and len(extra) one too long.
func TestSymptomPickerViewSplitCustomFillsExactLimit(t *testing.T) {
	// primaryLimit = 2; one selected symptom already pins ID 1.
	// ID 2 is custom (not builtin, active).  It must fill the remaining slot.
	// ID 3 is a plain builtin and should overflow to extra.
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Selected", IsBuiltin: true},
		{ID: 2, Name: "MyCustom", IsBuiltin: false},
		{ID: 3, Name: "Headache", IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{1: true}, 2)
	if len(primary) != 2 {
		t.Fatalf("expected 2 primary symptoms, got %d (primary=%v extra=%v)",
			len(primary), primary, extra)
	}
	if primary[1].ID != 2 {
		t.Fatalf("expected custom symptom (ID 2) to fill last primary slot, got ID %d",
			primary[1].ID)
	}
	if len(extra) != 1 || extra[0].ID != 3 {
		t.Fatalf("expected Headache (ID 3) in extra, got %v", extra)
	}
}

// TestSymptomPickerViewSplitCustomOverflowsWhenLimitFull verifies that custom
// symptoms overflow to extra once primary is already full (the other side of the
// line-60 branch).
func TestSymptomPickerViewSplitCustomOverflowsWhenLimitFull(t *testing.T) {
	// primaryLimit = 1; one selected symptom already fills it.
	// ID 2 is custom but the limit is already reached — it must go to extra.
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Selected", IsBuiltin: true},
		{ID: 2, Name: "MyCustom", IsBuiltin: false},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{1: true}, 1)
	if len(primary) != 1 {
		t.Fatalf("expected 1 primary symptom, got %d", len(primary))
	}
	if primary[0].ID != 1 {
		t.Fatalf("expected selected symptom (ID 1) in primary, got ID %d", primary[0].ID)
	}
	if len(extra) != 1 || extra[0].ID != 2 {
		t.Fatalf("expected custom symptom (ID 2) in extra, got %v", extra)
	}
}
