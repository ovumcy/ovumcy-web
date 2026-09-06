package services

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
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

// TestConvertDayBBTToStorageJudgesTheSentinelInTheInputUnit pins WHERE the
// "not measured" sentinel is read; ConvertDayBBTToStorage's own doc says why it
// is read there. Below 0 °F both placements agree, so a negative-only table
// stays green over the defect — the Fahrenheit rows between 0 and 32, which
// convert to a non-positive Celsius value, are the ones that separate them.
//
// The stored value is compared EXACTLY, against expectations written as the
// grid values storage keeps, so the table states that grid rather than a
// neighbourhood around it. The row that carries the rounding is 20 °F: without
// roundStoredTemperatureValue it converts to -6.666666666666667 instead of
// -6.6667. 97.7 °F is no witness for it — measured, it converts to exactly
// 36.5 either way.
func TestConvertDayBBTToStorageJudgesTheSentinelInTheInputUnit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		unit  string
		input *float64
		want  *float64
	}{
		{name: "no value at all is not measured", unit: TemperatureUnitFahrenheit},
		{name: "fahrenheit zero is not measured", unit: TemperatureUnitFahrenheit, input: bbtPtr(0)},
		{name: "fahrenheit negative is not measured", unit: TemperatureUnitFahrenheit, input: bbtPtr(-1)},
		{name: "fahrenheit impossible positive stays a reading", unit: TemperatureUnitFahrenheit, input: bbtPtr(20), want: bbtPtr(-6.6667)},
		{name: "fahrenheit freezing point stays a reading", unit: TemperatureUnitFahrenheit, input: bbtPtr(32), want: bbtPtr(0)},
		{name: "fahrenheit ordinary reading", unit: TemperatureUnitFahrenheit, input: bbtPtr(97.7), want: bbtPtr(36.5)},
		{name: "celsius zero is not measured", unit: TemperatureUnitCelsius, input: bbtPtr(0)},
		{name: "celsius negative is not measured", unit: TemperatureUnitCelsius, input: bbtPtr(-1)},
		{name: "celsius impossible positive stays a reading", unit: TemperatureUnitCelsius, input: bbtPtr(0.5), want: bbtPtr(0.5)},
		{name: "celsius ordinary reading", unit: TemperatureUnitCelsius, input: bbtPtr(36.5), want: bbtPtr(36.5)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := ConvertDayBBTToStorage(testCase.input, testCase.unit)
			if !sameDayBBT(got, testCase.want) {
				t.Fatalf("%s %s: expected %s stored, got %s",
					describeDayBBT(testCase.input), TemperatureUnitSymbol(testCase.unit), describeDayBBT(testCase.want), describeDayBBT(got))
			}
		})
	}
}

// TestNormalizeDayEntryInputRefusesAReadingTheRangeCannotAccept is the other
// half of that placement: once a value survives the conversion as a reading,
// IsValidDayBBT is what refuses it, so the owner is told which field lost the
// save instead of receiving a day filed with an empty temperature.
//
// 20 in either unit is the ordinary case. The three non-finite spellings are
// there because strconv.ParseFloat accepts all of them, so the form can deliver
// one, and because only -Inf separates the two placements: it is non-positive,
// so without the non-finite branch in ConvertDayBBTToStorage it would BE the
// "not measured" sentinel and answer 200 with an empty field. NaN and +Inf
// reach the same refusal by the longer road; they are listed so the rule holds
// over the class, not over the one member that discriminates today.
func TestNormalizeDayEntryInputRefusesAReadingTheRangeCannotAccept(t *testing.T) {
	t.Parallel()

	for _, unit := range []string{TemperatureUnitCelsius, TemperatureUnitFahrenheit} {
		for _, raw := range []string{"20", "NaN", "+Inf", "-Inf"} {
			t.Run(raw+"/"+unit, func(t *testing.T) {
				t.Parallel()

				symbol := TemperatureUnitSymbol(unit)
				parsed, err := ParseDayBBTRawWithUnit(raw, unit)
				if err != nil {
					t.Fatalf("%q is a number the form parser accepts, so the range is what must judge it; got a parse error %v", raw, err)
				}
				if parsed == nil {
					t.Fatalf("%q in %s is not the owner's not-measured answer; it must reach the range as a reading", raw, symbol)
				}
				if _, err := NormalizeDayEntryInput(DayEntryInput{Flow: models.FlowNone, BBT: parsed}); !errors.Is(err, ErrInvalidDayBBT) {
					t.Fatalf("%q in %s must answer %v, got %v", raw, symbol, ErrInvalidDayBBT, err)
				}
			})
		}
	}
}

// TestIsValidDayBBTRefusesNaN pins the SHAPE of the range comparison rather
// than its bounds. `*value >= Min && *value <= Max` and the De Morgan rewrite
// that looks equivalent to it, `!(*value < Min || *value > Max)`, agree on
// every ordinary reading and differ on exactly one input: NaN compares false to
// both bounds, so the rewrite calls it valid and a NaN reaches storage, where
// it poisons every average and shift comparison the day takes part in.
func TestIsValidDayBBTRefusesNaN(t *testing.T) {
	t.Parallel()

	notANumber := math.NaN()
	if IsValidDayBBT(&notANumber) {
		t.Fatalf("NaN is no reading inside the physiological range; the comparison must refuse it")
	}

	// The anchor: the same call still accepts an ordinary reading, so the
	// refusal above is about NaN and not about a range that refuses everything.
	inRange := 36.5
	if !IsValidDayBBT(&inRange) {
		t.Fatalf("%.2f °C is inside the range and must be accepted", inRange)
	}
}

// TestParseDayBBTRawWithUnitReadsTheNotMeasuredAnswers covers the spellings the
// day form actually posts for "I did not measure today": the field left empty,
// and the non-positive values the sentinel accepts, in both units. Text that is
// not a number at all stays an error — read as "not measured" instead, a typo
// would erase a reading without a word.
func TestParseDayBBTRawWithUnitReadsTheNotMeasuredAnswers(t *testing.T) {
	t.Parallel()

	for _, unit := range []string{TemperatureUnitCelsius, TemperatureUnitFahrenheit} {
		symbol := TemperatureUnitSymbol(unit)
		for _, raw := range []string{"", "0", "-0", "-1"} {
			got, err := ParseDayBBTRawWithUnit(raw, unit)
			if err != nil {
				t.Fatalf("ParseDayBBTRawWithUnit(%q, %s): unexpected error %v", raw, symbol, err)
			}
			if got != nil {
				t.Fatalf("%q in %s means not measured, got %s stored", raw, symbol, describeDayBBT(got))
			}
		}
		if _, err := ParseDayBBTRawWithUnit("abc", unit); err == nil {
			t.Fatalf(`ParseDayBBTRawWithUnit("abc", %s): text that is not a number must be an error, got none`, symbol)
		}
	}
}

// sameDayBBT compares two stored temperatures exactly: either both are "not
// measured", or both are the same value on the stored grid. Exactness is the
// point — see the table above on what a tolerance hides.
func sameDayBBT(got *float64, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func describeDayBBT(value *float64) string {
	if value == nil {
		return "nil (not measured)"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
