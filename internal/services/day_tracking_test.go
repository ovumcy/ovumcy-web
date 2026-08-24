package services

import (
	"math"
	"strconv"
	"testing"
)

// bbtPtr builds a *float64 for BBT test inputs (nil means "not measured").
func bbtPtr(value float64) *float64 {
	return &value
}

func TestParseDayBBTRawWithUnitRejectsNonNumeric(t *testing.T) {
	t.Parallel()

	got, err := ParseDayBBTRawWithUnit("not-a-number", TemperatureUnitCelsius)
	if err == nil {
		t.Fatalf("expected an error for a non-numeric bbt, got value %v", got)
	}
	if got != nil {
		t.Fatalf("expected a nil value on parse error, got %v", *got)
	}
}

func TestParseDayBBTRawWithUnitCelsius(t *testing.T) {
	t.Parallel()

	got, err := ParseDayBBTRawWithUnit("36.58", TemperatureUnitCelsius)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got == nil || *got != 36.58 {
		t.Fatalf("expected 36.58, got %v", got)
	}
}

func TestParseDayBBTRawWithUnitFahrenheitConvertsToCelsius(t *testing.T) {
	t.Parallel()

	got, err := ParseDayBBTRawWithUnit("98.60", TemperatureUnitFahrenheit)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got == nil || *got != 37.00 {
		t.Fatalf("expected 37.00, got %v", got)
	}
}

func TestTemperatureUnitRangeFahrenheit(t *testing.T) {
	t.Parallel()

	minimum, maximum := TemperatureUnitRange(TemperatureUnitFahrenheit)
	if minimum != 93.20 {
		t.Fatalf("expected minimum 93.20, got %.2f", minimum)
	}
	if maximum != 109.40 {
		t.Fatalf("expected maximum 109.40, got %.2f", maximum)
	}
}

func TestFormatDayBBTForInputFahrenheit(t *testing.T) {
	t.Parallel()

	got := FormatDayBBTForInput(bbtPtr(37.0), TemperatureUnitFahrenheit)
	if got != "98.60" {
		t.Fatalf("expected 98.60, got %q", got)
	}
}

// TestDayBBTRoundTripsEveryStepOfTheOwnersUnit is the storage contract of a
// recorded measurement: what an owner typed is what the form shows back. The
// value is stored in Celsius whatever unit it was entered in, and the stored
// form used to be rounded to two Celsius decimals — the precision of the form
// itself. One step of 0.01 °C is 0.018 °F, so a two-decimal Fahrenheit entry
// has no exact two-decimal Celsius form: 98.61 came back as 98.62 after every
// save, silently altering a health measurement the owner recorded. The drift
// is below anything a BBT chart is read for, which is exactly why nothing else
// would ever report it.
//
// Both units are swept step by step across the form's own advertised range,
// because the range ENDS round-tripped even while the interior did not (93.20
// and 109.40 map onto 34.00 and 43.00 exactly), so a boundary-only check stays
// green over the defect.
func TestDayBBTRoundTripsEveryStepOfTheOwnersUnit(t *testing.T) {
	t.Parallel()

	for _, unit := range []string{TemperatureUnitFahrenheit, TemperatureUnitCelsius} {
		minimum, maximum := TemperatureUnitRange(unit)
		lowest := int(math.Round(minimum * 100))
		highest := int(math.Round(maximum * 100))
		if highest-lowest < 100 {
			t.Fatalf("unit %q: expected a range of at least a hundred steps to sweep, got %.2f..%.2f", unit, minimum, maximum)
		}

		for hundredths := lowest; hundredths <= highest; hundredths++ {
			typed := strconv.FormatFloat(float64(hundredths)/100, 'f', 2, 64)

			stored, err := ParseDayBBTRawWithUnit(typed, unit)
			if err != nil {
				t.Fatalf("unit %q: %q is inside the form's own range and must parse, got %v", unit, typed, err)
			}
			if stored == nil {
				t.Fatalf("unit %q: %q is a measurement, not an empty field", unit, typed)
			}
			if !IsValidDayBBT(stored) {
				t.Fatalf("unit %q: %q stored as %v, outside the storable range", unit, typed, *stored)
			}
			if got := FormatDayBBTForInput(stored, unit); got != typed {
				t.Fatalf("unit %q: an owner typed %s and the form redisplays %s (stored %.6f °C) — the reading was altered on save", unit, typed, got, *stored)
			}
		}
	}
}

func TestNormalizeTemperatureUnitDefaultsToCelsius(t *testing.T) {
	t.Parallel()

	if got := NormalizeTemperatureUnit("invalid"); got != TemperatureUnitCelsius {
		t.Fatalf("expected default %q, got %q", TemperatureUnitCelsius, got)
	}
}
