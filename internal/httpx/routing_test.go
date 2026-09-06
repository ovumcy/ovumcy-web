package httpx

import (
	"strings"
	"testing"
)

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

// TestAsciiLowerPathSkipsTheCopyWhenNothingFolds pins the read-only scan
// asciiLowerPath now runs before it ever allocates: a path carrying no
// 'A'-'Z' byte must return the original string with zero allocations, not a
// []byte copy that only happens to compare equal to it.
func TestAsciiLowerPathSkipsTheCopyWhenNothingFolds(t *testing.T) {
	const path = "/calendar/feed/already-lower-token.ics"

	var got string
	allocs := testing.AllocsPerRun(1000, func() {
		got = asciiLowerPath(path)
	})
	if got != path {
		t.Fatalf("asciiLowerPath(%q) = %q, want the input unchanged", path, got)
	}
	if allocs != 0 {
		t.Errorf("asciiLowerPath allocated %.0f times per call for a path with no uppercase letter, want 0", allocs)
	}
}

// TestHasRoutingPrefix pins the byte-exact ASCII-only prefix check
// isCalendarFeedRequestPath's cheap pre-check compares against: only 'A'-'Z'
// fold, a shorter path never matches, and an empty prefix matches anything.
func TestHasRoutingPrefix(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		prefix string
		want   bool
	}{
		{"identical", "/calendar/feed/", "/calendar/feed/", true},
		{"path continues past the prefix", "/calendar/feed/token.ics", "/calendar/feed/", true},
		{"path shorter than the prefix", "/calendar/fee", "/calendar/feed/", false},
		{"case-folded match, ASCII letters only", "/CALENDAR/Feed/", "/calendar/feed/", true},
		{"a byte differs outside the folded range", "/calendar/feet/", "/calendar/feed/", false},
		{"empty prefix matches anything, including an empty path", "", "", true},
		{"empty path against a non-empty prefix", "", "/calendar/feed/", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := HasRoutingPrefix(testCase.path, testCase.prefix); got != testCase.want {
				t.Errorf("HasRoutingPrefix(%q, %q) = %t, want %t", testCase.path, testCase.prefix, got, testCase.want)
			}
		})
	}
}

// TestHasRoutingPrefixRejectsAUnicodeConfusableStringsEqualFoldWouldAccept
// pins the exact defect HasRoutingPrefix replaces strings.EqualFold to avoid:
// Unicode simple case folding equates U+212A KELVIN SIGN with ASCII 'k' and
// U+017F LATIN SMALL LETTER LONG S with ASCII 's', a fold fiber's router
// never performs. Each case asserts the strings.EqualFold side first, so it
// fails loudly — rather than silently proving nothing — if a future Unicode
// table update ever stopped folding the pair together.
func TestHasRoutingPrefixRejectsAUnicodeConfusableStringsEqualFoldWouldAccept(t *testing.T) {
	cases := []struct {
		name   string
		ascii  string
		folded string
	}{
		{"U+212A KELVIN SIGN vs ASCII 'k'", "k", "\u212A"},
		{"U+017F LATIN SMALL LETTER LONG S vs ASCII 's'", "s", "\u017F"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if !strings.EqualFold(testCase.ascii, testCase.folded) {
				t.Fatalf("fixture is wrong: strings.EqualFold(%q, %q) = false, want true", testCase.ascii, testCase.folded)
			}
			path := testCase.folded + "-suffix/token.ics"
			prefix := testCase.ascii + "-suffix/"
			if HasRoutingPrefix(path, prefix) {
				t.Errorf("HasRoutingPrefix(%q, %q) = true, want false — this is the Unicode fold fiber's router never performs", path, prefix)
			}
		})
	}
}
