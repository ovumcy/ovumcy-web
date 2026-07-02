package i18n

import (
	"fmt"
	"strings"
	"testing"
)

func TestPluralCategoryRussian(t *testing.T) {
	cases := map[int]string{
		0:    "many",
		1:    "one",
		2:    "few",
		4:    "few",
		5:    "many",
		11:   "many",
		12:   "many",
		14:   "many",
		15:   "many",
		21:   "one",
		22:   "few",
		25:   "many",
		31:   "one",
		90:   "many",
		111:  "many",
		112:  "many",
		121:  "one",
		122:  "few",
		-3:   "few",
		1000: "many",
	}
	for n, want := range cases {
		if got := PluralCategory(LangRU, n); got != want {
			t.Errorf("PluralCategory(ru, %d) = %q, want %q", n, got, want)
		}
	}
}

func TestPluralCategoryFrenchTreatsZeroAndOneAsSingular(t *testing.T) {
	cases := map[int]string{0: "one", 1: "one", 2: "other", 21: "other"}
	for n, want := range cases {
		if got := PluralCategory(LangFR, n); got != want {
			t.Errorf("PluralCategory(fr, %d) = %q, want %q", n, got, want)
		}
	}
}

func TestPluralCategoryDefaultOneOther(t *testing.T) {
	for _, language := range []string{LangEN, LangDE, LangES, "unknown", ""} {
		if got := PluralCategory(language, 1); got != "one" {
			t.Errorf("PluralCategory(%q, 1) = %q, want one", language, got)
		}
		for _, n := range []int{0, 2, 21} {
			if got := PluralCategory(language, n); got != "other" {
				t.Errorf("PluralCategory(%q, %d) = %q, want other", language, n, got)
			}
		}
	}
}

func TestTranslatePluralFallbackChain(t *testing.T) {
	messages := map[string]string{
		"full.one":         "one form",
		"full.few":         "few form",
		"full.many":        "many form",
		"other-only.other": "other form",
		"bare":             "bare form",
	}

	if got := TranslatePlural(messages, LangRU, "full", 2); got != "few form" {
		t.Errorf("exact category lookup = %q, want %q", got, "few form")
	}
	// A locale whose category set lacks the selected category falls back to
	// ".other" (the default-language overlay in merged Messages maps).
	if got := TranslatePlural(messages, LangRU, "other-only", 2); got != "other form" {
		t.Errorf("other fallback = %q, want %q", got, "other form")
	}
	// Not-yet-pluralized keys keep rendering during transitions.
	if got := TranslatePlural(messages, LangRU, "bare", 2); got != "bare form" {
		t.Errorf("bare-key fallback = %q, want %q", got, "bare form")
	}
	if got := TranslatePlural(messages, LangRU, "missing", 2); got != "missing" {
		t.Errorf("missing key = %q, want the key itself", got)
	}
}

// TestRussianCountStringsAgainstRealLocales pins audit task #25 end to end:
// the shipped Russian locale must produce grammatically correct numeral
// agreement for the count-bearing stats strings across the one/few/many
// forms, loaded through the same Manager path production uses.
func TestRussianCountStringsAgainstRealLocales(t *testing.T) {
	manager := mustNewTestManager(t)
	messages := manager.Messages(LangRU)

	cases := []struct {
		key  string
		n    int
		want string
	}{
		{"stats.phase_mood_count", 1, "1 день с настроением"},
		{"stats.phase_mood_count", 3, "3 дня с настроением"},
		{"stats.phase_mood_count", 21, "21 день с настроением"},
		{"stats.phase_mood_count", 12, "12 дней с настроением"},
		{"stats.factor_cycle_length", 21, "Цикл длиной 21 день"},
		{"stats.factor_cycle_length", 24, "Цикл длиной 24 дня"},
		{"stats.factor_cycle_length", 30, "Цикл длиной 30 дней"},
		{"stats.reliability.sample", 1, "Основано на 1 завершённом цикле."},
		{"stats.reliability.sample", 2, "Основано на 2 завершённых циклах."},
		{"stats.reliability.sample", 6, "Основано на 6 завершённых циклах."},
		{"stats.phase_symptoms_days", 1, "1 записанный день в этой фазе"},
		{"stats.phase_symptoms_days", 2, "2 записанных дня в этой фазе"},
		{"stats.phase_symptoms_days", 5, "5 записанных дней в этой фазе"},
	}

	for _, testCase := range cases {
		pattern := TranslatePlural(messages, LangRU, testCase.key, testCase.n)
		got := fmt.Sprintf(pattern, testCase.n)
		if got != testCase.want {
			t.Errorf("%s with n=%d: got %q, want %q", testCase.key, testCase.n, got, testCase.want)
		}
	}

	// Range strings select their form by the upper bound.
	rangePattern := TranslatePlural(messages, LangRU, "stats.cycle_range_summary", 31)
	if got := fmt.Sprintf(rangePattern, 24, 31); got != "Ваши циклы: от 24 до 31 дня" {
		t.Errorf("range one-form: got %q", got)
	}
	// In «от X до Y …» the preposition «до» governs the genitive case, where
	// counted nouns after 2-4 take the genitive plural «дней» («до 24 дней»),
	// not «дня» — «до 24 дня» reads as the ordinal «до 24-го дня». Only the
	// .one form keeps «дня» («до 31 дня», genitive singular after «один»).
	rangePattern = TranslatePlural(messages, LangRU, "stats.cycle_range_summary", 24)
	if got := fmt.Sprintf(rangePattern, 21, 24); got != "Ваши циклы: от 21 до 24 дней" {
		t.Errorf("range few-form: got %q", got)
	}
	rangePattern = TranslatePlural(messages, LangRU, "stats.cycle_range_summary", 35)
	if got := fmt.Sprintf(rangePattern, 24, 35); got != "Ваши циклы: от 24 до 35 дней" {
		t.Errorf("range many-form: got %q", got)
	}

	// irregular_notice shares the same range-form contract as the range
	// summary above, including the genitive «дней» in its .few form.
	irregularFew := TranslatePlural(messages, LangRU, "stats.irregular_notice", 24)
	if got := fmt.Sprintf(irregularFew, 21, 24); got != "Ваши циклы варьируются от 21 до 24 дней — это нерегулярный ритм. Прогнозы приблизительны." {
		t.Errorf("irregular_notice few-form: got %q", got)
	}
}

func mustNewTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := NewManager(LangEN)
	if err != nil {
		t.Fatalf("load locales: %v", err)
	}
	return manager
}

// TestPluralVariantPatternsKeepPrintfVerbs guards against a plural variant
// dropping or adding a %-verb relative to its siblings: Sprintf with a
// mismatched argument count renders %!(EXTRA ...) noise straight into the
// page.
func TestPluralVariantPatternsKeepPrintfVerbs(t *testing.T) {
	locales := mustLoadAllLocaleMessages(t)
	reference := locales[LangEN]
	bases := pluralBaseKeys(reference)
	if len(bases) == 0 {
		t.Fatal("expected at least one plural group in the English locale")
	}

	for language, messages := range locales {
		for base := range bases {
			verbCounts := map[string]int{}
			for _, category := range PluralCategories(language) {
				value, ok := messages[base+"."+category]
				if !ok {
					continue // parity test reports missing variants
				}
				verbCounts[category] = strings.Count(value, "%d")
			}
			seen := -1
			for category, count := range verbCounts {
				if seen == -1 {
					seen = count
					continue
				}
				if count != seen {
					t.Errorf("%s: plural variants of %q disagree on %%d count (%s has %d)", language, base, category, count)
				}
			}
		}
	}
}
