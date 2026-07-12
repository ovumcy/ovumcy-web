package api

import (
	"strings"
	"testing"
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
