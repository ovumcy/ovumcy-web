package services

import "testing"

func TestParseBoolLike(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		value bool
	}{
		{name: "numeric true", raw: "1", value: true},
		{name: "word true", raw: "true", value: true},
		{name: "case-insensitive true", raw: "YeS", value: true},
		{name: "html on", raw: "on", value: true},
		{name: "trimmed true", raw: "  true  ", value: true},
		{name: "numeric false", raw: "0", value: false},
		{name: "word false", raw: "false", value: false},
		{name: "empty false", raw: "", value: false},
		{name: "unknown false", raw: "maybe", value: false},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseBoolLike(testCase.raw); got != testCase.value {
				t.Fatalf("ParseBoolLike(%q) = %v, want %v", testCase.raw, got, testCase.value)
			}
		})
	}
}
