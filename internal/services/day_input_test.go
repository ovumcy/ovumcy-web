package services

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeDayEntryInputRejectsInvalidFlow(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod: true,
		Flow:     "bad-flow",
	})
	if !errors.Is(err, ErrInvalidDayFlow) {
		t.Fatalf("expected ErrInvalidDayFlow, got %v", err)
	}
}

func TestNormalizeDayEntryInputNormalizesNonPeriodDay(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod:   false,
		Flow:       models.FlowHeavy,
		SymptomIDs: []uint{10, 11},
		Notes:      "note",
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if normalized.Flow != models.FlowNone {
		t.Fatalf("expected flow %q, got %q", models.FlowNone, normalized.Flow)
	}
	if len(normalized.SymptomIDs) != 2 || normalized.SymptomIDs[0] != 10 || normalized.SymptomIDs[1] != 11 {
		t.Fatalf("expected symptom IDs to be preserved, got %#v", normalized.SymptomIDs)
	}
}

func TestNormalizeDayEntryInputTrimsNotes(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod: true,
		Flow:     models.FlowNone,
		Notes:    strings.Repeat("x", MaxDayNotesLength+13),
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if len(normalized.Notes) != MaxDayNotesLength {
		t.Fatalf("expected notes length %d, got %d", MaxDayNotesLength, len(normalized.Notes))
	}
}

func TestTrimDayNotes(t *testing.T) {
	asciiAtLimit := strings.Repeat("a", MaxDayNotesLength)

	cyrillicTail := strings.Repeat("a", MaxDayNotesLength-1) + "б" // "б" is 2 bytes; 2nd byte lands at index 2000
	emojiTail := strings.Repeat("a", MaxDayNotesLength-3) + "😀"    // emoji is 4 bytes; last 3 bytes land inside the limit

	tests := []struct {
		name  string
		value string
	}{
		{"ascii at limit unchanged", asciiAtLimit},
		{"cyrillic rune split at byte boundary", cyrillicTail},
		{"emoji rune split at byte boundary", emojiTail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimDayNotes(tt.value)
			if len(got) > MaxDayNotesLength {
				t.Fatalf("expected trimmed length <= %d, got %d", MaxDayNotesLength, len(got))
			}
			if !utf8.ValidString(got) {
				t.Fatalf("expected valid UTF-8, got invalid string of length %d", len(got))
			}
			if tt.value == asciiAtLimit && len(got) != MaxDayNotesLength {
				t.Fatalf("expected ascii-at-limit to remain unchanged at %d bytes, got %d", MaxDayNotesLength, len(got))
			}
		})
	}
}

func TestNormalizeDayEntryInputNormalizesCycleFactors(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		CycleFactorKeys: []string{
			models.CycleFactorTravel,
			"  STRESS ",
			models.CycleFactorTravel,
			"",
		},
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}

	if len(normalized.CycleFactorKeys) != 2 {
		t.Fatalf("expected two normalized cycle factors, got %#v", normalized.CycleFactorKeys)
	}
	if normalized.CycleFactorKeys[0] != models.CycleFactorStress || normalized.CycleFactorKeys[1] != models.CycleFactorTravel {
		t.Fatalf("expected stable factor order, got %#v", normalized.CycleFactorKeys)
	}
}

func TestNormalizeDayEntryInputRejectsInvalidCycleFactor(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:            models.FlowNone,
		CycleFactorKeys: []string{models.CycleFactorStress, "unknown"},
	})
	if !errors.Is(err, ErrInvalidDayCycleFactors) {
		t.Fatalf("expected ErrInvalidDayCycleFactors, got %v", err)
	}
}

func TestNormalizeDayEntryInputRejectsInvalidPregnancyTest(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:          models.FlowNone,
		PregnancyTest: "bad-test",
	})
	if !errors.Is(err, ErrInvalidDayPregnancyTest) {
		t.Fatalf("expected ErrInvalidDayPregnancyTest, got %v", err)
	}
}

func TestNormalizeDayEntryInputNormalizesPregnancyTest(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:          models.FlowNone,
		PregnancyTest: " POSITIVE ",
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if normalized.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected pregnancy test %q, got %q", models.PregnancyTestPositive, normalized.PregnancyTest)
	}
}
