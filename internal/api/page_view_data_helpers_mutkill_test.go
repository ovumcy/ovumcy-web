package api

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Mutation-kill test for formatBBTLocalizedMessage (page_view_data_helpers.go
// L183). The helper uses the translated pattern, falling back to `fallback` when
// the translation is empty (`pattern == ""`) OR missing (`pattern == key`), then
// formats it with the min/max/symbol arguments. The line carries two comparison
// operators; negating either (CONDITIONALS_NEGATION) inverts which patterns fall
// back. Pure function, tested directly with distinguishable translation vs
// fallback patterns.
func TestFormatBBTLocalizedMessageMutKill(t *testing.T) {
	const key = "dashboard.bbt_range_hint"
	const fallback = "FALLBACK %s-%s %s"

	t.Run("present translation is formatted", func(t *testing.T) {
		// Translation present, non-empty, != key -> must be used. Negating
		// `pattern == ""`->`!=` (forces fallback whenever non-empty) OR
		// `pattern == key`->`!=` (forces fallback whenever != key) both discard
		// the translation here.
		got := formatBBTLocalizedMessage(map[string]string{key: "TRANSLATED %s to %s %s"}, key, fallback, "36.00", "38.00", "C")
		if !strings.HasPrefix(got, "TRANSLATED ") {
			t.Fatalf("expected the present translation to be used, got %q", got)
		}
		if got != "TRANSLATED 36.00 to 38.00 C" {
			t.Fatalf("expected formatted translation, got %q", got)
		}
	})

	t.Run("missing translation falls back", func(t *testing.T) {
		// translateMessage echoes the key -> fallback. Negating `pattern == key`
		// keeps the bare key as the format pattern, producing key + Sprintf noise
		// instead of the fallback string.
		got := formatBBTLocalizedMessage(map[string]string{}, key, fallback, "36.00", "38.00", "C")
		if got != "FALLBACK 36.00-38.00 C" {
			t.Fatalf("expected fallback formatting for missing translation, got %q", got)
		}
	})
}

// The range copy names the bounds without the padding a fixed two-decimal
// format adds ("34-43 °C", not "34.00-43.00 °C") and with the language's
// decimal separator, while the input attributes the browser validates against
// keep the two decimals and a ".". Both halves are pinned here: formatting the
// attributes too would change what `data-temperature-min` carries, and
// formatting nothing brings the lab-style copy back.
func TestBuildBBTFieldViewDataTrimsRangeCopyZeros(t *testing.T) {
	patterns := map[string]string{
		"dashboard.bbt_range_hint":  "hint %s-%s %s",
		"dashboard.bbt_range_error": "error %s-%s %s",
	}

	t.Run("celsius bounds read as whole degrees", func(t *testing.T) {
		view := buildBBTFieldViewData(patterns, "en", services.TemperatureUnitCelsius)
		if view.RangeHint != "hint 34-43 °C" {
			t.Fatalf("expected trimmed celsius hint, got %q", view.RangeHint)
		}
		if view.RangeError != "error 34-43 °C" {
			t.Fatalf("expected trimmed celsius error, got %q", view.RangeError)
		}
		if view.Min != "34.00" || view.Max != "43.00" {
			t.Fatalf("expected the input bounds to keep two decimals, got %q-%q", view.Min, view.Max)
		}
	})

	t.Run("fahrenheit bounds keep the decimal they need", func(t *testing.T) {
		view := buildBBTFieldViewData(patterns, "en", services.TemperatureUnitFahrenheit)
		if view.RangeHint != "hint 93.2-109.4 °F" {
			t.Fatalf("expected one-decimal fahrenheit hint, got %q", view.RangeHint)
		}
		if view.RangeError != "error 93.2-109.4 °F" {
			t.Fatalf("expected one-decimal fahrenheit error, got %q", view.RangeError)
		}
		if view.Min != "93.20" || view.Max != "109.40" {
			t.Fatalf("expected the input bounds to keep two decimals, got %q-%q", view.Min, view.Max)
		}
	})

	t.Run("the copy takes the language's decimal separator, the attributes do not", func(t *testing.T) {
		view := buildBBTFieldViewData(patterns, "de", services.TemperatureUnitFahrenheit)
		if view.RangeHint != "hint 93,2-109,4 °F" || view.RangeError != "error 93,2-109,4 °F" {
			t.Fatalf("expected comma bounds in the copy, got %q / %q", view.RangeHint, view.RangeError)
		}
		if view.Min != "93.20" || view.Max != "109.40" {
			t.Fatalf("the validated input bounds must keep a dot, got %q-%q", view.Min, view.Max)
		}
	})
}
