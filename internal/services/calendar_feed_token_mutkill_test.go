package services

import "testing"

// TestCalendarFeedTokenLengthEqualsSelectorPlusVerifier kills the
// calendar_feed_token.go:52 ARITHMETIC_BASE survivor on:
//
//	calendarFeedTokenLength = calendarFeedSelectorLength + calendarFeedVerifierLength
//
// Swapping `+` to `-` yields a negative length (16 - 32 = -16), which would make
// SplitCalendarFeedToken reject every real 48-char token (`len(token) != -16` is
// always true) and break feed auth entirely. The constant-declaration line carries
// no Go coverage counter (reported NOT_COVERED), so this pins the arithmetic
// contract directly rather than relying on incidental round-trip coverage.
func TestCalendarFeedTokenLengthEqualsSelectorPlusVerifier(t *testing.T) {
	t.Parallel()

	if got, want := calendarFeedTokenLength, calendarFeedSelectorLength+calendarFeedVerifierLength; got != want {
		t.Fatalf("calendarFeedTokenLength = %d, want selector(%d)+verifier(%d) = %d",
			got, calendarFeedSelectorLength, calendarFeedVerifierLength, want)
	}
	// Pin the concrete total width too, so a compensating change to the halves
	// cannot silently redefine the token length.
	if calendarFeedTokenLength != 48 {
		t.Fatalf("calendarFeedTokenLength = %d, want 48", calendarFeedTokenLength)
	}
}
