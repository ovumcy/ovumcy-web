package services

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// unitSeparator sits between a number and its unit. It is U+00A0, a no-break
// space, so "28" and "d" never wrap onto two lines.
const unitSeparator = "\xc2\xa0"

// numberDecimals is the one precision the product prints measured values at:
// a tenth, with a whole number carrying no trailing ".0".
const numberDecimals = 1

// FormatLocalizedNumber is the one place a measured value becomes text. It
// rounds the exact value half away from zero (toFixed and Intl agree with it on
// every value the chart script can be handed, so the server's string and the
// script's never differ), drops trailing zeros, and uses the language's decimal
// separator.
func FormatLocalizedNumber(language string, value float64) string {
	return formatLocalizedNumberAt(language, value, numberDecimals)
}

// FormatLocalizedQuantity prints a number followed by its unit.
func FormatLocalizedQuantity(language string, value float64, unit string) string {
	return JoinUnit(FormatLocalizedNumber(language, value), unit)
}

// JoinUnit attaches a unit to an already formatted number.
func JoinUnit(number string, unit string) string {
	if unit == "" {
		return number
	}
	return number + unitSeparator + unit
}

// UnitSuffix is the unit with its separator in front, for a chart script that
// appends it to a number it formats itself.
func UnitSuffix(unit string) string {
	return JoinUnit("", unit)
}

func formatLocalizedNumberAt(language string, value float64, decimals int) string {
	text := formatFixedHalfAway(value, decimals)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "-0" {
		text = "0"
	}
	return localizeDecimalSeparator(language, text)
}

// formatFixedHalfAway rounds the exact binary value to decimals places, which
// is what JavaScript's toFixed does, except that a tie the binary value hits
// exactly (36.25) goes away from zero rather than to even. Scaling by a power of
// ten and rounding would instead double-round 36.55, whose binary value is just
// under the tie.
func formatFixedHalfAway(value float64, decimals int) string {
	if isExactTie(value, decimals) {
		value = math.Nextafter(value, math.Copysign(math.Inf(1), value))
	}
	return strconv.FormatFloat(value, 'f', decimals, 64)
}

func isExactTie(value float64, decimals int) bool {
	shortest := strconv.FormatFloat(value, 'f', -1, 64)
	point := strings.IndexByte(shortest, '.')
	if point < 0 || len(shortest)-point-1 != decimals+1 || shortest[len(shortest)-1] != '5' {
		return false
	}
	decimal, ok := new(big.Rat).SetString(shortest)
	return ok && decimal.Cmp(new(big.Rat).SetFloat64(value)) == 0
}

func localizeDecimalSeparator(language string, text string) string {
	if decimalSeparatorFor(language) == "." {
		return text
	}
	return strings.Replace(text, ".", decimalSeparatorFor(language), 1)
}

func decimalSeparatorFor(language string) string {
	lang := strings.ToLower(language)
	if index := strings.IndexAny(lang, "-_"); index >= 0 {
		lang = lang[:index]
	}
	switch lang {
	case "de", "es", "fr", "it", "ru":
		return ","
	default:
		return "."
	}
}
