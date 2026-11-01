package i18n_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The refusals that name the day-observation bound take the number through %d,
// fed from services.ManualCycleStartFutureDays: a value that spells the number
// ("2 days", "two days") goes on stating the old bound the day the constant
// moves. Each is a plural family in every language, so the noun agrees with
// whatever number is rendered.

// observationBoundSentenceKeys are the plural bases internal/api formats with
// the bound (lookupObservationBoundMessage); the format barrier's contract
// table pins the same set to that site.
var observationBoundSentenceKeys = []string{
	"dashboard.error.invalid_cycle_start_date",
	"dashboard.error.invalid_pregnancy_test_date",
	"settings.import_future_day",
}

func TestObservationBoundSentencesTakeTheBoundThroughOnePercentD(t *testing.T) {
	catalogues := shippedCatalogues(t)
	if len(catalogues) < 6 {
		t.Fatalf("read %d catalogues, want all six", len(catalogues))
	}
	digit := regexp.MustCompile(`[0-9]`)
	for language, messages := range catalogues {
		for _, base := range observationBoundSentenceKeys {
			if _, fixed := messages[base]; fixed {
				t.Errorf("%s carries %s as one fixed-form value; a bound sentence is a plural family", language, base)
			}
			for _, category := range i18n.PluralCategories(language) {
				key := base + "." + category
				value, ok := messages[key]
				if !ok {
					t.Errorf("%s has no value for %s", language, key)
					continue
				}
				verbs, err := formatVerbsOf(value)
				if err != nil {
					t.Errorf("%s %s: %v", language, key, err)
					continue
				}
				if len(verbs) != 1 || verbs[0] != "%d" {
					t.Errorf("%s %s: format verbs %v, want exactly [%%d] (value %q)", language, key, verbs, value)
				}
				if digit.MatchString(value) {
					t.Errorf("%s %s spells a number instead of taking the bound through %%d: %q", language, key, value)
				}
			}
		}
	}
}

// TestObservationBoundSentencesRenderTheConstant pins the rendered number to
// the constant by name: whatever ManualCycleStartFutureDays returns is what
// the owner reads, in the plural form its count selects.
func TestObservationBoundSentencesRenderTheConstant(t *testing.T) {
	days := services.ManualCycleStartFutureDays()
	for language, messages := range shippedCatalogues(t) {
		for _, base := range observationBoundSentenceKeys {
			pattern := i18n.TranslatePlural(messages, language, base, days)
			if pattern == base {
				t.Errorf("%s: %s resolves to no variant for %d", language, base, days)
				continue
			}
			rendered := fmt.Sprintf(pattern, days)
			if !strings.Contains(rendered, fmt.Sprint(days)) || strings.Contains(rendered, "%!") {
				t.Errorf("%s %s rendered %q, want the bound %d", language, base, rendered, days)
			}
		}
	}
}
