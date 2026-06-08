package services

import (
	"errors"
	"strings"
	"testing"

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
