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

	// bbtStoredScale is the canonical stored precision for a BBT reading:
	// ten-thousandths of a degree Celsius. bbtStoredGridValue is the one
	// rounding onto that grid; roundStoredTemperatureValue and the shift
	// detector's comparisons (cycle_signals.go) both go through it.
	bbtStoredScale = 10000
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
	return ConvertDayBBTToStorage(&value, unit), nil
}

// ConvertDayBBTToStorage takes a BBT value already expressed in the account's
// chosen unit and converts it to the canonical stored Celsius form. It is the
// unit-conversion half of ParseDayBBTRawWithUnit, factored out so the JSON
// bind path — which parses its own float via encoding/json rather than a form
// string — converts and rounds identically instead of writing whatever unit
// the caller sent straight to storage.
func ConvertDayBBTToStorage(value *float64, unit string) *float64 {
	if value == nil {
		return nil
	}
	converted := *value
	if NormalizeTemperatureUnit(unit) == TemperatureUnitFahrenheit {
		converted = fahrenheitToCelsius(converted)
	}
	return normalizeStoredDayBBT(&converted)
}

// normalizeStoredDayBBT collapses any non-measurement (nil or a non-positive
// value, the old sentinel range) to nil, and rounds a genuine reading. The
// result is the canonical stored form: nil for unmeasured, a rounded pointer
// otherwise.
func normalizeStoredDayBBT(value *float64) *float64 {
	if value == nil || *value <= 0 {
		return nil
	}
	rounded := roundStoredTemperatureValue(*value)
	return &rounded
}

// roundStoredTemperatureValue rounds a reading to its canonical STORED form.
// Storage is always Celsius, and it keeps more decimals than either unit
// displays on purpose: a reading entered in Fahrenheit is converted before it
// is stored, and one step of 0.01 °F is 0.0056 °C, so a stored value rounded to
// the two Celsius decimals the form shows cannot represent what the owner
// typed. It came back one hundredth of a degree away after every save — a
// recorded measurement altered without notice, far below the 0.1-0.5 °F shift a
// BBT chart is read for and therefore invisible to every other check. Four
// decimals keep every 0.01 °F step distinct and exactly recoverable, while
// still collapsing the float noise the conversion produces.
func roundStoredTemperatureValue(value float64) float64 {
	return bbtStoredGridValue(value) / bbtStoredScale
}

// bbtStoredGridValue is the one rounding onto the stored grid: the reading as
// a whole number of bbtStoredScale-ths, kept as float64 so whatever the form
// parser lets through (NaN, 1e300) stays a defined value for IsValidDayBBT to
// refuse rather than an out-of-range int64 conversion.
func bbtStoredGridValue(value float64) float64 {
	return math.Round(value * bbtStoredScale)
}

// bbtStoredUnits is bbtStoredGridValue as an integer, for readings that have
// already passed IsValidDayBBT. Comparisons in these units are exact where a
// float64 addition is not: 36.2 + 0.2 is 36.400000000000006, one ULP above a
// third day recorded as 36.4.
func bbtStoredUnits(value float64) int64 {
	return int64(bbtStoredGridValue(value))
}

// roundTemperatureValue rounds to the two decimals a temperature is DISPLAYED
// with, in whichever unit it is being shown. It is a presentation rounding, not
// the stored one.
func roundTemperatureValue(value float64) float64 {
	return math.Round(value*100) / 100
}

func celsiusToFahrenheit(value float64) float64 {
	return value*9/5 + 32
}

func fahrenheitToCelsius(value float64) float64 {
	return (value - 32) * 5 / 9
}
