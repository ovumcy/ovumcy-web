package apideps

import "testing"

// TestDependenciesValidateReportsMissing pins the fail-fast contract: the zero
// Dependencies has every field nil, so Validate must report the first missing
// dependency instead of returning nil (which would defer the failure to a
// nil-panic on the first request).
func TestDependenciesValidateReportsMissing(t *testing.T) {
	if err := (Dependencies{}).Validate(); err == nil {
		t.Fatal("expected Validate to report a missing dependency for the zero value")
	}
}

// TestDependencyRequirementMissing covers the nil-detection helper across the
// interface-nil, typed-nil-pointer, non-nil-pointer, and non-nilable-value
// kinds.
func TestDependencyRequirementMissing(t *testing.T) {
	var nilPtr *int
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil interface", nil, true},
		{"typed nil pointer", nilPtr, true},
		{"non-nil pointer", new(int), false},
		{"non-nilable value", 42, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (dependencyRequirement{value: tc.value}).missing(); got != tc.want {
				t.Fatalf("missing() = %v, want %v", got, tc.want)
			}
		})
	}
}
