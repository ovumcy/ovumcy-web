package services

import (
	"errors"
	"testing"

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
