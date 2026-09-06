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

// TestIsValidDayBBTRejectsOutsideThePhysiologicalRange is the rejecting half of
// the range check. Both endpoints were accepted by their own anchors and the
// only refusal anywhere was an above-maximum stored value, so removing
// `*value >= MinDayBBTCelsius` left every BBT test green: nothing submitted a
// reading below the range, which is what a Fahrenheit value entered on a
// Celsius form looks like.
func TestIsValidDayBBTRejectsOutsideThePhysiologicalRange(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{MinDayBBTCelsius - 0.1, 20.0, MaxDayBBTCelsius + 0.1, 200.0} {
		if IsValidDayBBT(bbtPtr(value)) {
			t.Fatalf("expected IsValidDayBBT(%.2f) = false outside [%.2f, %.2f], got true", value, MinDayBBTCelsius, MaxDayBBTCelsius)
		}
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

// TestFormatDayBBTForInputRendersALegacyZeroAsNotMeasured pins
// normalizeStoredDayBBT's `*value <= 0` guard where that function is still
// reached: a row written before BBT became nullable holds 0, and the day form
// has to render it as an empty field rather than as a measurement of zero. The
// CONDITIONALS_BOUNDARY mutant (`*value < 0`) lets the 0 through and prints
// "0.00" °C — or "32.00" °F — into the input the owner is about to save back.
//
// It used to be written against ParseDayBBTRawWithUnit, which no longer reaches
// this guard: the parse path reads the sentinel one call earlier, in the
// owner's unit (ConvertDayBBTToStorage), and that boundary is covered by
// TestConvertDayBBTToStorageJudgesTheSentinelInTheInputUnit.
func TestFormatDayBBTForInputRendersALegacyZeroAsNotMeasured(t *testing.T) {
	t.Parallel()

	for _, unit := range []string{TemperatureUnitCelsius, TemperatureUnitFahrenheit} {
		if got := FormatDayBBTForInput(bbtPtr(0), unit); got != "" {
			t.Fatalf("a legacy stored 0 in %s must render as an empty field (not measured), got %q", TemperatureUnitSymbol(unit), got)
		}
	}
}
