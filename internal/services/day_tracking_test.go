package services

import "testing"

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

func TestNormalizeTemperatureUnitDefaultsToCelsius(t *testing.T) {
	t.Parallel()

	if got := NormalizeTemperatureUnit("invalid"); got != TemperatureUnitCelsius {
		t.Fatalf("expected default %q, got %q", TemperatureUnitCelsius, got)
	}
}
