package services

import (
	"math"
	"testing"
)

func TestIsValidDayBBTAcceptsLowerBoundCelsius(t *testing.T) {
	t.Parallel()

	if !IsValidDayBBT(MinDayBBTCelsius) {
		t.Fatalf("expected IsValidDayBBT(%.2f) = true at the lower bound, got false", MinDayBBTCelsius)
	}
}

func TestIsValidDayBBTAcceptsUpperBoundCelsius(t *testing.T) {
	t.Parallel()

	if !IsValidDayBBT(MaxDayBBTCelsius) {
		t.Fatalf("expected IsValidDayBBT(%.2f) = true at the upper bound, got false", MaxDayBBTCelsius)
	}
}

func TestFormatDayBBTForInputReturnsEmptyForUnsetValue(t *testing.T) {
	t.Parallel()

	if got := FormatDayBBTForInput(0, TemperatureUnitCelsius); got != "" {
		t.Fatalf("expected empty string for unset BBT, got %q", got)
	}
}

func TestParseDayBBTRawWithUnitNormalizesNegativeZeroToCleanZero(t *testing.T) {
	t.Parallel()

	// "-0" parses to IEEE-754 negative zero. normalizeStoredDayBBT's `value <= 0`
	// guard must canonicalize it to clean +0.0. The CONDITIONALS_BOUNDARY mutant
	// (`value < 0`) lets -0.0 slip through to roundTemperatureValue, returning -0.0.
	got, err := ParseDayBBTRawWithUnit("-0", TemperatureUnitCelsius)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != 0 {
		t.Fatalf("expected magnitude 0, got %v", got)
	}
	// math.Signbit is the only way to distinguish the two zeros, since -0.0 == +0.0.
	// Original: Signbit(got)==false (clean +0.0). Mutant: Signbit(got)==true (-0.0).
	if math.Signbit(got) {
		t.Fatalf("expected canonical +0.0 (clean zero), got negative zero -0.0; " +
			"normalizeStoredDayBBT must use `value <= 0` to normalize negative zero")
	}
}
