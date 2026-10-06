package services

import (
	"strings"
	"testing"
)

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
		{name: "crlf header injection blocked", raw: "/dashboard\r\nSet-Cookie: evil=1", want: fallback},
		{name: "bare newline blocked", raw: "/ok\nLocation: https://evil", want: fallback},
		{name: "path without leading slash blocked", raw: "dashboard", want: fallback},
		{name: "tab before second slash blocked", raw: "/\t/evil.example", want: fallback},
		{name: "nul byte blocked", raw: "/dashboard\x00", want: fallback},
		{name: "path over the length bound blocked", raw: "/" + strings.Repeat("a", maxRedirectPathBytes), want: fallback},
		{name: "path at the length bound kept", raw: "/" + strings.Repeat("a", maxRedirectPathBytes-1), want: "/" + strings.Repeat("a", maxRedirectPathBytes-1)},
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
