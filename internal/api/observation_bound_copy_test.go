package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// A key outside the bound families, and a bound family the catalogue cannot
// answer, are both misses: the caller falls back rather than rendering a raw
// pattern or a machine key with verb residue.
func TestObservationBoundLookupMissesOutsideItsFamilies(t *testing.T) {
	t.Parallel()

	messages := map[string]string{"common.saved": "Saved."}
	if value, ok := lookupObservationBoundMessage(messages, i18n.LangEN, "common.saved"); ok {
		t.Fatalf("a key outside the bound families resolved to %q", value)
	}
	if value, ok := lookupObservationBoundMessage(map[string]string{}, i18n.LangEN, observationBoundCycleStartKey); ok {
		t.Fatalf("an unanswered bound family resolved to %q", value)
	}

	render := newTemplateFuncMap()["observationBoundMessage"].(func(map[string]string, string, string) string)
	if got := render(map[string]string{}, i18n.LangEN, observationBoundImportKey); got != observationBoundImportKey {
		t.Fatalf("the template helper rendered %q for an unanswered family, want the key", got)
	}
}

func TestObservationBoundTemplateHelperRendersTheConstant(t *testing.T) {
	t.Parallel()

	days := services.ManualCycleStartFutureDays()
	messages := map[string]string{
		observationBoundImportKey + ".one":   "%d day",
		observationBoundImportKey + ".other": "%d days",
	}
	render := newTemplateFuncMap()["observationBoundMessage"].(func(map[string]string, string, string) string)
	got := render(messages, i18n.LangEN, observationBoundImportKey)
	want := fmt.Sprintf(i18n.TranslatePlural(messages, i18n.LangEN, observationBoundImportKey, days), days)
	if got != want || !strings.Contains(got, fmt.Sprint(days)) {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}
