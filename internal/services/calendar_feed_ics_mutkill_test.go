package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFoldICSLineCountsOctetsNotRunesForMultibyte kills the
// calendar_feed_ics.go:235 CONDITIONALS_NEGATION survivor. That line guards
// utf8.RuneLen's -1 (invalid-rune) sentinel:
//
//	runeOctets := utf8.RuneLen(r)
//	if runeOctets < 0 {   // negated -> `runeOctets >= 0`, which is always true
//		runeOctets = 1
//	}
//
// Negated to `runeOctets >= 0`, the clamp fires for EVERY rune, so a multi-byte
// UTF-8 rune (2-4 octets) is miscounted as a single octet. Folding then measures
// segments in RUNES instead of OCTETS, so a line of multibyte runes whose octet
// length exceeds 75 is emitted UNFOLDED (its rune count never reaches the
// 75-octet fold threshold), producing a physical line far longer than RFC 5545
// §3.1's 75-octet cap. The existing ASCII fold test cannot see this (1 rune == 1
// octet for ASCII), which is exactly why the negation survived.
//
// Each case builds a line whose byte length is > 75 but whose rune count is < 76,
// so correct octet-based folding splits it into <=75-octet segments while the
// mutant leaves it as a single oversized segment.
func TestFoldICSLineCountsOctetsNotRunesForMultibyte(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		glyph string
		count int
	}{
		// 'я' U+044F is 2 octets in UTF-8: 40 runes = 80 octets.
		{name: "two-byte runes", glyph: "я", count: 40},
		// '你' U+4F60 is 3 octets in UTF-8: 30 runes = 90 octets.
		{name: "three-byte runes", glyph: "你", count: 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := strings.Repeat(tc.glyph, tc.count)
			if len(line) <= 75 {
				t.Fatalf("test setup: line must exceed 75 octets, got %d", len(line))
			}
			if runes := utf8.RuneCountInString(line); runes >= 76 {
				t.Fatalf("test setup: rune count must stay below the 76-rune fold threshold, got %d", runes)
			}

			folded := foldICSLine(line)

			// The oversized line MUST be folded: octet accounting reaches 75 well
			// before the 76-rune count the mutant would need to trigger a fold.
			if !strings.Contains(folded, "\r\n ") {
				t.Fatalf("expected a %d-octet multibyte line to be folded, got a single unfolded line of %d octets", len(line), len(folded))
			}
			// Every physical segment must respect RFC 5545's 75-octet cap. Under the
			// rune-counting mutant the whole line stays one >75-octet segment.
			for i, segment := range strings.Split(folded, "\r\n ") {
				if len(segment) > 75 {
					t.Fatalf("fold segment %d is %d octets, exceeding the RFC 5545 §3.1 75-octet cap", i, len(segment))
				}
			}
		})
	}
}
