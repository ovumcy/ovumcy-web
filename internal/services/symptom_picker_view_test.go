package services

import (
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestSplitSymptomsForCollapsedPickerPrioritizesActiveCustomSymptoms(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", UserID: 9, IsBuiltin: true},
		{ID: 2, Name: "Fatigue", UserID: 9, IsBuiltin: true},
		{ID: 3, Name: "Headache", UserID: 9, IsBuiltin: true},
		{ID: 4, Name: "Joint stiffness", UserID: 9},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{}, 3)
	if len(primary) != 3 {
		t.Fatalf("expected 3 primary symptoms, got %d", len(primary))
	}
	if len(extra) != 1 {
		t.Fatalf("expected 1 extra symptom, got %d", len(extra))
	}
	if primary[0].Name != "Joint stiffness" {
		t.Fatalf("expected active custom symptom to stay in primary picker, got %#v", primary)
	}
}

func TestSplitSymptomsForCollapsedPickerKeepsSelectedSymptomsPinned(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", UserID: 9, IsBuiltin: true},
		{ID: 2, Name: "Fatigue", UserID: 9, IsBuiltin: true},
		{ID: 3, Name: "Joint stiffness", UserID: 9},
		{ID: 4, Name: "Bloating", UserID: 9, IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{2: true}, 2)
	if len(primary) != 2 {
		t.Fatalf("expected 2 primary symptoms, got %d", len(primary))
	}
	if primary[0].ID != 2 {
		t.Fatalf("expected selected symptom to stay first in primary picker, got %#v", primary)
	}
	if primary[1].ID != 3 {
		t.Fatalf("expected active custom symptom to fill remaining primary slot, got %#v", primary)
	}
	if len(extra) != 2 {
		t.Fatalf("expected 2 extra symptoms, got %d", len(extra))
	}
}

func TestSplitSymptomsForCollapsedPickerMovesMoodSwingsOutOfPrimaryPath(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", UserID: 9, IsBuiltin: true},
		{ID: 2, Name: "Mood swings", UserID: 9, IsBuiltin: true},
		{ID: 3, Name: "Headache", UserID: 9, IsBuiltin: true},
		{ID: 4, Name: "Bloating", UserID: 9, IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{}, 3)
	if len(primary) != 3 {
		t.Fatalf("expected 3 primary symptoms, got %d", len(primary))
	}
	for _, symptom := range primary {
		if symptom.Name == "Mood swings" {
			t.Fatalf("did not expect Mood swings in primary picker: %#v", primary)
		}
	}
	if len(extra) != 1 || extra[0].Name != "Mood swings" {
		t.Fatalf("expected Mood swings to stay in extra symptoms, got %#v", extra)
	}
}

func TestSplitSymptomsForCollapsedPickerKeepsSelectedMoodSwingsPinned(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps", UserID: 9, IsBuiltin: true},
		{ID: 2, Name: "Mood swings", UserID: 9, IsBuiltin: true},
		{ID: 3, Name: "Headache", UserID: 9, IsBuiltin: true},
	}

	primary, extra := SplitSymptomsForCollapsedPicker(symptoms, map[uint]bool{2: true}, 2)
	if len(primary) != 2 {
		t.Fatalf("expected 2 primary symptoms, got %d", len(primary))
	}
	if primary[0].Name != "Mood swings" {
		t.Fatalf("expected selected Mood swings to stay pinned, got %#v", primary)
	}
	if len(extra) != 1 {
		t.Fatalf("expected 1 extra symptom, got %d", len(extra))
	}
}
