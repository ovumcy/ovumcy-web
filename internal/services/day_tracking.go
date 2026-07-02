package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	TemperatureUnitCelsius    = "c"
	TemperatureUnitFahrenheit = "f"
	DefaultTemperatureUnit    = TemperatureUnitCelsius

	MinDayBBTCelsius = 34.0
	MaxDayBBTCelsius = 43.0
)

func NormalizeDaySexActivity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.SexActivityProtected:
		return models.SexActivityProtected
	case models.SexActivityUnprotected:
		return models.SexActivityUnprotected
	default:
		return models.SexActivityNone
	}
}

func IsValidDaySexActivity(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", models.SexActivityNone, models.SexActivityProtected, models.SexActivityUnprotected:
		return true
	default:
		return false
	}
}

func NormalizeDayCervicalMucus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.CervicalMucusDry:
		return models.CervicalMucusDry
	case models.CervicalMucusMoist:
		return models.CervicalMucusMoist
	case models.CervicalMucusCreamy:
		return models.CervicalMucusCreamy
	case models.CervicalMucusEggWhite:
		return models.CervicalMucusEggWhite
	default:
		return models.CervicalMucusNone
	}
}

func IsValidDayCervicalMucus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", models.CervicalMucusNone, models.CervicalMucusDry, models.CervicalMucusMoist, models.CervicalMucusCreamy, models.CervicalMucusEggWhite:
		return true
	default:
		return false
	}
}

func NormalizeDayPregnancyTest(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case models.PregnancyTestNegative:
		return models.PregnancyTestNegative
	case models.PregnancyTestPositive:
		return models.PregnancyTestPositive
	default:
		return models.PregnancyTestNone
	}
}

func IsValidDayPregnancyTest(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", models.PregnancyTestNone, models.PregnancyTestNegative, models.PregnancyTestPositive:
		return true
	default:
		return false
	}
}

// IsValidDayBBT reports whether a basal body temperature value is acceptable
// for storage. A nil pointer means "not measured" and is always valid; a
// measured value is valid only inside the physiological range. There is no
// sentinel: an unmeasured reading is nil, not 0.
func IsValidDayBBT(value *float64) bool {
	if value == nil {
		return true
	}
	return *value >= MinDayBBTCelsius && *value <= MaxDayBBTCelsius
}

func NormalizeTemperatureUnit(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TemperatureUnitFahrenheit:
		return TemperatureUnitFahrenheit
	default:
		return TemperatureUnitCelsius
	}
}

func TemperatureUnitSymbol(unit string) string {
	switch NormalizeTemperatureUnit(unit) {
	case TemperatureUnitFahrenheit:
		return "°F"
	default:
		return "°C"
	}
}

func TemperatureUnitRange(unit string) (float64, float64) {
	switch NormalizeTemperatureUnit(unit) {
	case TemperatureUnitFahrenheit:
		return roundTemperatureValue(celsiusToFahrenheit(MinDayBBTCelsius)), roundTemperatureValue(celsiusToFahrenheit(MaxDayBBTCelsius))
	default:
		return MinDayBBTCelsius, MaxDayBBTCelsius
	}
}

func FormatDayBBTForInput(value *float64, unit string) string {
	normalized := normalizeStoredDayBBT(value)
	if normalized == nil {
		return ""
	}
	if NormalizeTemperatureUnit(unit) == TemperatureUnitFahrenheit {
		return fmt.Sprintf("%.2f", roundTemperatureValue(celsiusToFahrenheit(*normalized)))
	}
	return fmt.Sprintf("%.2f", *normalized)
}

// ParseDayBBTRawWithUnit parses a raw temperature form field into a nullable
// stored value. An empty field means "not measured" and yields nil; a parsed
// value is normalized (and unit-converted) and returned as a pointer.
func ParseDayBBTRawWithUnit(raw string, unit string) (*float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	normalized := strings.ReplaceAll(trimmed, ",", ".")
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid day bbt: %w", err)
	}
	if NormalizeTemperatureUnit(unit) == TemperatureUnitFahrenheit {
		value = fahrenheitToCelsius(value)
	}
	return normalizeStoredDayBBT(&value), nil
}

// normalizeStoredDayBBT collapses any non-measurement (nil or a non-positive
// value, the old sentinel range) to nil, and rounds a genuine reading. The
// result is the canonical stored form: nil for unmeasured, a rounded pointer
// otherwise.
func normalizeStoredDayBBT(value *float64) *float64 {
	if value == nil || *value <= 0 {
		return nil
	}
	rounded := roundTemperatureValue(*value)
	return &rounded
}

func roundTemperatureValue(value float64) float64 {
	return math.Round(value*100) / 100
}

func celsiusToFahrenheit(value float64) float64 {
	return value*9/5 + 32
}

func fahrenheitToCelsius(value float64) float64 {
	return (value - 32) * 5 / 9
}
