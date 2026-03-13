package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/terraincognita07/ovumcy/internal/models"
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

func IsValidDayBBT(value float64) bool {
	return value == 0 || (value >= MinDayBBTCelsius && value <= MaxDayBBTCelsius)
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

func FormatDayBBTForInput(value float64, unit string) string {
	normalized := normalizeStoredDayBBT(value)
	if normalized <= 0 {
		return ""
	}
	if NormalizeTemperatureUnit(unit) == TemperatureUnitFahrenheit {
		return fmt.Sprintf("%.2f", roundTemperatureValue(celsiusToFahrenheit(normalized)))
	}
	return fmt.Sprintf("%.2f", normalized)
}

func ParseDayBBTRaw(raw string) (float64, error) {
	return ParseDayBBTRawWithUnit(raw, TemperatureUnitCelsius)
}

func ParseDayBBTRawWithUnit(raw string, unit string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}

	normalized := strings.ReplaceAll(trimmed, ",", ".")
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid day bbt: %w", err)
	}
	if NormalizeTemperatureUnit(unit) == TemperatureUnitFahrenheit {
		value = fahrenheitToCelsius(value)
	}
	return normalizeStoredDayBBT(value), nil
}

func normalizeStoredDayBBT(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return roundTemperatureValue(value)
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
