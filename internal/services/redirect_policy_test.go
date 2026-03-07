package services

import "testing"

func TestSanitizeRedirectPath(t *testing.T) {
	fallback := "/login"

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty uses fallback", raw: "", want: fallback},
		{name: "absolute url blocked", raw: "https://evil.example", want: fallback},
		{name: "protocol relative blocked", raw: "//evil.example", want: fallback},
		{name: "slash backslash redirect blocked", raw: "/\\evil.example", want: fallback},
		{name: "path without leading slash blocked", raw: "dashboard", want: fallback},
		{name: "valid local path kept", raw: "/dashboard", want: "/dashboard"},
		{name: "valid local path with query kept", raw: "/calendar?month=2026-02&day=2026-02-17", want: "/calendar?month=2026-02&day=2026-02-17"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SanitizeRedirectPath(testCase.raw, fallback); got != testCase.want {
				t.Fatalf("SanitizeRedirectPath(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}
