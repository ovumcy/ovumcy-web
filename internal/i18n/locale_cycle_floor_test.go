package i18n_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// shippedCatalogues reads every supported language through the manager the
// application itself boots.
func shippedCatalogues(t *testing.T) map[string]map[string]string {
	t.Helper()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	catalogues := map[string]map[string]string{}
	for _, language := range manager.SupportedLanguages() {
		catalogues[language] = manager.Messages(language)
	}
	return catalogues
}

// Every sentence that names the completed-cycle floor takes the number through
// %d, fed from the one constant the dashboard's fertility floor and the stats
// page's insights gate both read. A catalogue value that spells the number
// itself is a second copy of that constant in six files, and it goes on telling
// the owner "3" the day the constant moves.

// cycleFloorSentenceKeys are the plural bases of the sentences whose one %d is
// the floor. They are declared here because the verb-sequence half of this
// barrier needs to know which values must carry the verb; the literal half
// below enumerates every key of every catalogue and has no list to fall out of
// date.
//
// Each is a plural family in every language, never a single fixed-form value: a
// number-agreeing sentence ("нужно %d цикла") is right for one range of floors
// and wrong for the rest, so the form follows the number the page prints.
var cycleFloorSentenceKeys = []string{
	"dashboard.fertile_window_after_cycle_floor",
	"dashboard.next_period_need_cycles",
	"dashboard.ovulation_need_cycles",
	"prediction.explainer.awaiting_more_cycles",
	"prediction.explainer.irregular_sparse",
	"stats.empty_body_one",
	"stats.empty_body_zero",
	"stats.empty_progress_hint",
}

// TestNoCatalogueValueEmbedsTheCycleFloorLiteral sweeps every value of every
// catalogue for the floor's digits as a standalone number.
func TestNoCatalogueValueEmbedsTheCycleFloorLiteral(t *testing.T) {
	floor := regexp.MustCompile(fmt.Sprintf(`(^|[^\p{L}0-9%%])%d([^0-9]|$)`, services.FertilityMinimumCycles()))

	swept := 0
	var offenders []string
	for language, messages := range shippedCatalogues(t) {
		for key, value := range messages {
			swept++
			if floor.MatchString(value) {
				offenders = append(offenders, fmt.Sprintf("%s %s: %q", language, key, value))
			}
		}
	}
	if swept < 1000 {
		t.Fatalf("swept only %d catalogue values; a sweep that read no catalogue passes about nothing", swept)
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("catalogue values spell the completed-cycle floor %d instead of taking it through %%d:\n%s",
			services.FertilityMinimumCycles(), strings.Join(offenders, "\n"))
	}
}

// TestCycleFloorSentencesTakeTheFloorThroughOnePercentD checks the verb
// sequence in all six catalogues: a translator who dropped the verb ships
// "%!(EXTRA int=3)", one who doubled it ships "%!d(MISSING)".
func TestCycleFloorSentencesTakeTheFloorThroughOnePercentD(t *testing.T) {
	catalogues := shippedCatalogues(t)
	if len(catalogues) < 6 {
		t.Fatalf("read %d catalogues, want all six", len(catalogues))
	}
	for language, messages := range catalogues {
		for _, base := range cycleFloorSentenceKeys {
			if _, fixed := messages[base]; fixed {
				t.Errorf("%s carries %s as one fixed-form value; a floor sentence is a plural family", language, base)
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
			}
		}
	}
}
