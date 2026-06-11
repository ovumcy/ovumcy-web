package services

import (
	"strings"
	"testing"
)

// TestNormalizeSymptomIconInputAcceptsExactMaxLength kills the
// symptom_name_policy.go:53 CONDITIONALS_BOUNDARY survivor
// (`utf8.RuneCountInString(icon) > maxSymptomIconLength` -> `>=`): an icon of
// exactly maxSymptomIconLength runes is valid and must be returned unchanged.
// Under the `>=` mutant it is wrongly rejected with ErrSymptomNameTooLong.
func TestNormalizeSymptomIconInputAcceptsExactMaxLength(t *testing.T) {
	icon := strings.Repeat("a", maxSymptomIconLength)

	got, err := normalizeSymptomIconInput(icon)
	if err != nil {
		t.Fatalf("icon of exactly maxSymptomIconLength=%d runes must be accepted, got error: %v", maxSymptomIconLength, err)
	}
	if got != icon {
		t.Fatalf("expected the max-length icon returned unchanged, got %q", got)
	}
}
