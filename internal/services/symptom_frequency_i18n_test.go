package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// TestLocalizedSymptomFrequencySummaryCoversEveryRequiredLocale is the sweep
// mechanism for LocalizedSymptomFrequencySummary's per-language branches.
// Unlike authErrorTranslationKeys/settingsStatusTranslationKeys (checked in
// i18n_policy_test.go) this function's per-language text is hardcoded
// directly in Go source rather than resolved through a locale JSON key, so
// TestLocaleKeysParity cannot see it either. A locale silently missing its
// own branch — a rebase conflict, a copy-paste slip — falls through to the
// generic English default at the bottom of the function instead of failing
// anything, which is the #287 failure class one function over. This walks
// every locale the i18n manager reports and requires its output to diverge
// from the English default; it deliberately does not pin exact translated
// wording (the per-language tests below already do that) so it only fails
// when a branch is lost, not when copy changes.
func TestLocalizedSymptomFrequencySummaryCoversEveryRequiredLocale(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	const count, days = 2, 4 // plural forms in every currently supported language
	english := LocalizedSymptomFrequencySummary(i18n.LangEN, count, days)

	for _, language := range languages {
		if language == i18n.LangEN {
			continue
		}
		if got := LocalizedSymptomFrequencySummary(language, count, days); got == english {
			t.Errorf("LocalizedSymptomFrequencySummary: locale %q has no dedicated branch and silently renders the English default %q", language, english)
		}
	}
}

func TestLocalizedSymptomFrequencySummary_EnglishPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular count and day", count: 1, days: 1, expected: "1 time (in 1 day)"},
		{name: "plural count and day", count: 2, days: 4, expected: "2 times (in 4 days)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("en", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_RussianPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "one form", count: 1, days: 1, expected: "1 раз (за 1 день)"},
		{name: "few form", count: 2, days: 4, expected: "2 раза (за 4 дня)"},
		{name: "many form", count: 5, days: 7, expected: "5 раз (за 7 дней)"},
		{name: "teens form", count: 11, days: 12, expected: "11 раз (за 12 дней)"},
		{name: "mixed form", count: 21, days: 22, expected: "21 раз (за 22 дня)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("ru", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_SpanishPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular count and day", count: 1, days: 1, expected: "1 vez (en 1 día)"},
		{name: "plural count and day", count: 2, days: 4, expected: "2 veces (en 4 días)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("es", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_GermanPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular day", count: 1, days: 1, expected: "1 Mal (an 1 Tag)"},
		{name: "plural day", count: 2, days: 4, expected: "2 Mal (an 4 Tagen)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("de", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_ItalianPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular count and day", count: 1, days: 1, expected: "1 volta (in 1 giorno)"},
		{name: "plural count and day", count: 2, days: 4, expected: "2 volte (in 4 giorni)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("it", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_FrenchPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular day", count: 1, days: 1, expected: "1 fois (en 1 jour)"},
		{name: "plural day", count: 2, days: 4, expected: "2 fois (en 4 jours)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("fr", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}
