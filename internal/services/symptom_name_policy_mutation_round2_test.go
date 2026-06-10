package services

import (
	"strings"
	"testing"
)

// TestNormalizeSymptomNameInputAcceptsExactMaxLength kills the
// CONDITIONALS_BOUNDARY mutant at symptom_name_policy.go:25, where the guard is
// `utf8.RuneCountInString(name) > maxSymptomNameLength`. A name of exactly
// maxSymptomNameLength runes must be accepted; mutating `>` to `>=` would
// reject the boundary-length name with ErrSymptomNameTooLong.
func TestNormalizeSymptomNameInputAcceptsExactMaxLength(t *testing.T) {
	name := strings.Repeat("a", maxSymptomNameLength)
	got, err := normalizeSymptomNameInput(name)
	if err != nil {
		t.Fatalf("name of exactly maxSymptomNameLength runes must be accepted, got error: %v", err)
	}
	if got != name {
		t.Fatalf("got %q, want %q", got, name)
	}
}
