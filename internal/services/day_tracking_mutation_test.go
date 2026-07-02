package services

import (
	"testing"
)

func TestIsValidDayBBTAcceptsLowerBoundCelsius(t *testing.T) {
	t.Parallel()

	if !IsValidDayBBT(bbtPtr(MinDayBBTCelsius)) {
		t.Fatalf("expected IsValidDayBBT(%.2f) = true at the lower bound, got false", MinDayBBTCelsius)
	}
}

func TestIsValidDayBBTAcceptsUpperBoundCelsius(t *testing.T) {
	t.Parallel()

	if !IsValidDayBBT(bbtPtr(MaxDayBBTCelsius)) {
		t.Fatalf("expected IsValidDayBBT(%.2f) = true at the upper bound, got false", MaxDayBBTCelsius)
	}
}

func TestIsValidDayBBTNilIsUnmeasured(t *testing.T) {
	t.Parallel()

	// A nil pointer means "not measured" and is always a valid stored state.
	if !IsValidDayBBT(nil) {
		t.Fatalf("expected IsValidDayBBT(nil) = true (not measured), got false")
	}
}

func TestFormatDayBBTForInputReturnsEmptyForUnsetValue(t *testing.T) {
	t.Parallel()

	// The canonical unset value is nil (empty form field) — it renders empty.
	if got := FormatDayBBTForInput(nil, TemperatureUnitCelsius); got != "" {
		t.Fatalf("expected empty string for unset BBT, got %q", got)
	}
}

func TestParseDayBBTRawWithUnitNormalizesNonPositiveToNil(t *testing.T) {
	t.Parallel()

	// A non-positive parsed value (including "-0" and "0") is not a physiological
	// reading; normalizeStoredDayBBT's `*value <= 0` guard must collapse it to nil
	// (not measured). The CONDITIONALS_BOUNDARY mutant (`*value < 0`) would let a
	// parsed 0 through as a pointer to 0.0 instead of nil.
	for _, raw := range []string{"-0", "0"} {
		got, err := ParseDayBBTRawWithUnit(raw, TemperatureUnitCelsius)
		if err != nil {
			t.Fatalf("ParseDayBBTRawWithUnit(%q): unexpected error %v", raw, err)
		}
		if got != nil {
			t.Fatalf("expected nil (not measured) for %q, got %v", raw, *got)
		}
	}
}
