package services

import "testing"

func TestParseDayBBTRawWithUnitCelsius(t *testing.T) {
	t.Parallel()

	got, err := ParseDayBBTRawWithUnit("36.58", TemperatureUnitCelsius)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != 36.58 {
		t.Fatalf("expected 36.58, got %.2f", got)
	}
}

func TestParseDayBBTRawWithUnitFahrenheitConvertsToCelsius(t *testing.T) {
	t.Parallel()

	got, err := ParseDayBBTRawWithUnit("98.60", TemperatureUnitFahrenheit)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != 37.00 {
		t.Fatalf("expected 37.00, got %.2f", got)
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

	got := FormatDayBBTForInput(37.0, TemperatureUnitFahrenheit)
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
