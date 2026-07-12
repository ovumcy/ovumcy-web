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

// TestTrimDayNotesDoesNotUnderflowOnAllContinuationBytes pins the rune-rewind
// loop's lower-bound guard `end > 0` in TrimDayNotes (day_input.go). When an
// over-limit notes value is made entirely of UTF-8 continuation bytes — crafted
// invalid input that is reachable from the untrusted day-notes field, and which
// the existing valid-UTF-8 cases never produce — the rewind walks `end` down to
// index 0. The guard must stop there and yield an empty string. The
// CONDITIONALS_BOUNDARY mutant `end > 0` -> `end >= 0` instead decrements to -1
// and panics on value[:-1]; a panic fails this test.
func TestTrimDayNotesDoesNotUnderflowOnAllContinuationBytes(t *testing.T) {
	// 0x80 is a UTF-8 continuation byte and never a rune start, so a run of them
	// longer than the cap drives the rewind loop all the way to index 0.
	value := strings.Repeat("\x80", MaxDayNotesLength+1)

	got := TrimDayNotes(value)

	if got != "" {
		t.Fatalf("expected empty result when every in-range byte is a continuation byte, got %d bytes", len(got))
	}
	if len(got) > MaxDayNotesLength {
		t.Fatalf("expected trimmed length <= %d, got %d", MaxDayNotesLength, len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8 result, got invalid string of length %d", len(got))
	}
}
