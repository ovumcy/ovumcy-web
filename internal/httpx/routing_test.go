package httpx

import "testing"

// TestRoutingNormalizedPath pins the exact two operations fiber's own router
// applies to pick a route under CaseSensitive=false, StrictRouting=false (the
// app's shipped fiberConfig): ASCII-only case folding, then trailing slashes
// trimmed everywhere except the bare root. Nothing else may be folded, or a
// caller comparing this normalization against fiber's own dispatch would
// claim spellings the router does not actually accept.
func TestRoutingNormalizedPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"already normalized", "/calendar/feed/token.ics", "/calendar/feed/token.ics"},
		{"uppercase letters folded", "/CALENDAR/Feed/TOKEN.ICS", "/calendar/feed/token.ics"},
		{"single trailing slash trimmed", "/calendar/feed/", "/calendar/feed"},
		{"multiple trailing slashes trimmed", "/calendar/feed//", "/calendar/feed"},
		{"uppercase with trailing slash", "/CALENDAR/FEED/", "/calendar/feed"},
		{"root stays root, not emptied", "/", "/"},
		{"empty path is left alone", "", ""},
		{"single-character path is left alone", "a", "a"},
		{
			// strings.ToLower is Unicode-aware and would fold U+212A (KELVIN
			// SIGN) onto ASCII 'k'; fiber's own byte table
			// (gofiber/utils/v2/bytes.UnsafeToLower) only ever touches 'A'-'Z',
			// so this must survive untouched or the normalization claims a
			// fold the router itself never performs.
			name: "non-ASCII code points are never folded",
			path: "/Kelvin",
			want: "/Kelvin",
		},
		{"percent-encoding stays literal", "/calendar/feed/%2E%2E.ics", "/calendar/feed/%2e%2e.ics"},
		{"interior double slash is not collapsed", "/calendar//feed", "/calendar//feed"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RoutingNormalizedPath(testCase.path); got != testCase.want {
				t.Errorf("RoutingNormalizedPath(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}

// TestAsciiLowerPath pins RoutingNormalizedPath's lowering half in isolation:
// only 'A'-'Z' fold, every other byte (including one only distinguishable by
// its high bit) passes through unchanged, and a path with no uppercase letter
// is returned without a copy.
func TestAsciiLowerPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"mixed case folds only ASCII letters", "AbC123-_/", "abc123-_/"},
		{"digits and punctuation untouched", "1-2_3.4~5", "1-2_3.4~5"},
		{"no uppercase letters", "already-lower", "already-lower"},
		{"empty string", "", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := asciiLowerPath(testCase.path); got != testCase.want {
				t.Errorf("asciiLowerPath(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}
