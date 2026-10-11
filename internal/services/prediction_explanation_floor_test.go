package services

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// explainerBase strips the plural category the English catalogue appends, so a
// plural family and a fixed-form sentence are both named by the key a service
// returns. The categories come from the catalogue's own plural table.
func explainerBase(key string) string {
	for _, category := range i18n.PluralCategories(i18n.LangEN) {
		if base, found := strings.CutSuffix(key, "."+category); found {
			return base
		}
	}
	return key
}

// A prediction-explainer sentence is handed the completed-cycle floor exactly
// when its English catalogue text carries a %d. The page prints a counted
// sentence through printf with its plural form, so a sentence that gains a %d
// without a count renders the bare "%d" (or, for a plural family, the raw key),
// and one handed a count it has no verb for renders "%!(EXTRA int=...)".
func TestEveryExplainerSentenceThatTakesANumberIsGivenTheFloor(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}

	texts := map[string]string{}
	for key, value := range manager.Messages(i18n.LangEN) {
		if !strings.HasPrefix(key, "prediction.explainer.") {
			continue
		}
		texts[explainerBase(key)] += value
	}

	judged := map[string]bool{}
	for base, text := range texts {
		takesNumber := strings.Contains(text, "%d")
		got := predictionExplanationPrimaryCount(base)
		if takesNumber && got != FertilityMinimumCycles() {
			t.Errorf("%s names a number but is given count %d, want the floor %d", base, got, FertilityMinimumCycles())
		}
		if !takesNumber && got != 0 {
			t.Errorf("%s names no number but is given count %d", base, got)
		}
		judged[base] = takesNumber
	}

	// The two sentences that name the floor, by name: a sweep that stopped
	// reading them, or read them without a %d, must not pass on a population.
	for _, key := range []string{predictionExplainerAwaitingMoreCyclesKey, predictionExplainerIrregularSparseKey} {
		takesNumber, seen := judged[key]
		if !seen {
			t.Fatalf("%s was not among the explainer sentences read from the English catalogue", key)
		}
		if !takesNumber {
			t.Errorf("%s carries no %%d in English, yet it is a sentence that names the completed-cycle floor", key)
		}
		if got := predictionExplanationPrimaryCount(key); got != FertilityMinimumCycles() {
			t.Errorf("%s is given count %d, want the floor %d", key, got, FertilityMinimumCycles())
		}
	}
}

// Every primary explainer key BuildOwnerPredictionExplanation can return renders
// from the count it is returned with, in every language. The page prints a
// counted sentence through its plural form and any other through the flat key,
// so a plural family handed count 0 would show the owner the raw key: the
// family has no flat value to fall back on. The set of keys is read off the real
// function over every combination of the inputs it branches on.
func TestEveryExplainerKeyTheServiceReturnsRendersForItsCount(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}

	type returned struct {
		key   string
		count int
	}
	reached := map[returned]bool{}
	secondaries := map[string]bool{}
	const inputs = 10
	for mask := range 1 << inputs {
		bit := func(index int) bool { return mask&(1<<index) != 0 }
		user := &models.User{Role: models.RoleOwner, IrregularCycle: bit(0)}
		context := DashboardCycleContext{
			PregnancyPaused:            bit(1),
			PredictionDisabled:         bit(2),
			DisplayNextPeriodNeedsData: bit(3),
			DisplayOvulationNeedsData:  bit(4),
			DisplayNextPeriodUseRange:  bit(5),
			DisplayOvulationUseRange:   bit(6),
			AwaitingFirstCycle:         bit(7),
			AwaitingMoreCycles:         bit(8),
		}
		explanation := BuildOwnerPredictionExplanation(user, context, bit(9))
		if explanation.PrimaryKey != "" {
			reached[returned{key: explanation.PrimaryKey, count: explanation.PrimaryCount}] = true
		}
		if explanation.SecondaryKey != "" {
			secondaries[explanation.SecondaryKey] = true
		}
	}

	for _, key := range []string{predictionExplainerAwaitingMoreCyclesKey, predictionExplainerIrregularSparseKey} {
		if !reached[returned{key: key, count: FertilityMinimumCycles()}] {
			t.Errorf("%s is never returned with the floor %d; the floor sentences are unreachable or lost their count", key, FertilityMinimumCycles())
		}
	}

	for _, language := range manager.SupportedLanguages() {
		messages := manager.Messages(language)
		for hit := range reached {
			if hit.count > 0 {
				for _, category := range i18n.PluralCategories(language) {
					if strings.TrimSpace(messages[hit.key+"."+category]) == "" {
						t.Errorf("%s: %s is returned with count %d but has no %q form", language, hit.key, hit.count, category)
					}
				}
				continue
			}
			if strings.TrimSpace(messages[hit.key]) == "" {
				t.Errorf("%s: %s is returned with count 0 and has no flat catalogue value, so the owner would be shown the raw key", language, hit.key)
			}
		}
		for key := range secondaries {
			if strings.TrimSpace(messages[key]) == "" {
				t.Errorf("%s: the secondary explainer %s has no flat catalogue value", language, key)
			}
		}
	}
}
