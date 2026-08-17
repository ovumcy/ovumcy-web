package services

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeDayEntryInputAcceptsMinimumMood(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		Mood: MinDayMood,
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() with minimum mood %d: unexpected error: %v", MinDayMood, err)
	}
	if normalized.Mood != MinDayMood {
		t.Fatalf("expected mood %d to be preserved, got %d", MinDayMood, normalized.Mood)
	}
}

func TestNormalizeDayEntryInputAcceptsMaximumMood(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		Mood: MaxDayMood,
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() with maximum mood %d: unexpected error: %v", MaxDayMood, err)
	}
	if normalized.Mood != MaxDayMood {
		t.Fatalf("expected mood %d to be preserved, got %d", MaxDayMood, normalized.Mood)
	}
}

func TestNormalizeDayEntryInputRejectsNegativeMood(t *testing.T) {
	// A negative mood is below MinDayMood and must be rejected. This pins the
	// lower-bound comparison: the valid range is exactly {0} plus [MinDayMood, MaxDayMood].
	if _, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		Mood: -1,
	}); !errors.Is(err, ErrInvalidDayMood) {
		t.Fatalf("expected ErrInvalidDayMood for negative mood, got %v", err)
	}
	// In-range minimum mood must still be accepted, so the guard is not merely
	// rejecting everything below zero while also dropping the valid lower bound.
	if _, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		Mood: MinDayMood,
	}); err != nil {
		t.Fatalf("NormalizeDayEntryInput() with minimum mood %d: unexpected error: %v", MinDayMood, err)
	}
}

// TestTrimDayNotesCapsCraftedInvalidUTF8 pins the character-counting loop in
// TrimDayNotes (day_input.go) on crafted invalid input, which is reachable from
// the untrusted day-notes field and which the valid-UTF-8 cases never produce.
// A run of bare UTF-8 continuation bytes decodes as one replacement character
// per byte, so the count and the cut must agree on that reading: the
// CONDITIONALS_BOUNDARY mutant `characters == MaxDayNotesLength` ->
// `characters >= MaxDayNotesLength` never fires and the whole over-limit value
// comes back, while an off-by-one in the guard drops the cut below the cap.
// Neither may panic on the slice bound.
func TestTrimDayNotesCapsCraftedInvalidUTF8(t *testing.T) {
	// 0x80 is a UTF-8 continuation byte and never a rune start; each one counts
	// as a single character, so this value is exactly one character over the cap.
	value := strings.Repeat("\x80", MaxDayNotesLength+1)

	got := TrimDayNotes(value)

	if characters := utf8.RuneCountInString(got); characters != MaxDayNotesLength {
		t.Fatalf("expected an over-limit value to be cut to exactly %d characters, got %d", MaxDayNotesLength, characters)
	}
	if !strings.HasPrefix(value, got) {
		t.Fatalf("expected the result to be a prefix of the input, got %d bytes", len(got))
	}
}
