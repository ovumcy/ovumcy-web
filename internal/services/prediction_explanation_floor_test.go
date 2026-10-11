package services

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// A prediction-explainer sentence is handed the completed-cycle floor exactly
// when its English catalogue text carries a %d. The service resolves the number
// once, so a sentence that gains a %d without a count renders "%!d(MISSING)",
// and one given a count it has no verb for renders "%!(EXTRA ...)".
func TestEveryExplainerSentenceThatTakesANumberIsGivenTheFloor(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}

	bases := map[string]string{}
	for key, value := range manager.Messages(i18n.LangEN) {
		if !strings.HasPrefix(key, "prediction.explainer.") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimSuffix(key, ".one"), ".other")
		bases[base] += value
	}
	if len(bases) < 6 {
		t.Fatalf("found only %d explainer sentences; the sweep read no catalogue", len(bases))
	}

	counted := 0
	for base, text := range bases {
		takesNumber := strings.Contains(text, "%d")
		got := predictionExplanationPrimaryCount(base)
		if takesNumber && got != FertilityMinimumCycles() {
			t.Errorf("%s names a number but is given count %d, want the floor %d", base, got, FertilityMinimumCycles())
		}
		if !takesNumber && got != 0 {
			t.Errorf("%s names no number but is given count %d", base, got)
		}
		if takesNumber {
			counted++
		}
	}
	if counted != 2 {
		t.Fatalf("%d explainer sentences take a number, want the irregular-sparse and awaiting-more-cycles pair", counted)
	}
}
