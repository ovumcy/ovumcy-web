package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// symptom_service.go:429:42 NEGATION `leftIndex != rightIndex`->`==`.
// Cramps (canonical index 0) and Bloating (canonical index 3): canonical order
// is the REVERSE of alphabetical (Bloating < Cramps). The `==` mutant skips the
// index-compare branch and falls through to alphabetical order -> Bloating first.
func TestMR3I18n_SortBuiltinCanonicalOverAlphabetical(t *testing.T) {
	symptoms := []models.SymptomType{
		{Name: "Bloating", IsBuiltin: true},
		{Name: "Cramps", IsBuiltin: true},
	}
	SortSymptomsByBuiltinAndName(symptoms)
	if symptoms[0].Name != "Cramps" || symptoms[1].Name != "Bloating" {
		t.Fatalf("canonical order lost: got [%q, %q], want [Cramps, Bloating]",
			symptoms[0].Name, symptoms[1].Name)
	}
}

// symptom_service.go:431:17 NEGATION `leftHas != rightHas`->`==`.
// One known builtin (Cramps) and one unknown builtin (Aardvark). Alphabetically
// Aardvark < Cramps, but known-builtins must sort before unknowns. The `==`
// mutant skips the known-first branch and falls through to alphabetical order
// -> Aardvark first.
func TestMR3I18n_SortBuiltinKnownBeforeUnknown(t *testing.T) {
	symptoms := []models.SymptomType{
		{Name: "Aardvark", IsBuiltin: true},
		{Name: "Cramps", IsBuiltin: true},
	}
	SortSymptomsByBuiltinAndName(symptoms)
	if symptoms[0].Name != "Cramps" || symptoms[1].Name != "Aardvark" {
		t.Fatalf("known-before-unknown lost: got [%q, %q], want [Cramps, Aardvark]",
			symptoms[0].Name, symptoms[1].Name)
	}
}
