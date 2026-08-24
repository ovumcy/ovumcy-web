package services

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// TestLocalizedDateFormsCoverEveryRequiredLocale is the sweep mechanism for the
// dateNames table, the same shape as
// TestLocalizedSymptomFrequencySummaryCoversEveryRequiredLocale one file over.
// Month and weekday names live in Go source rather than under a locale JSON
// key, so TestLocaleKeysParity and the rest of the six-file parity machinery
// cannot see them: a seventh locale is added by dropping a JSON file in, which
// extends SupportedLanguages automatically, and every date on the calendar
// header, the day label and the dashboard would silently render in English
// while every keyed string switched. Nothing would fail.
//
// This walks every locale the i18n manager reports and requires (a) a complete
// dateNames entry for it and (b) each rendered date to diverge from the English
// one. It deliberately does not pin translated wording — date_i18n_policy_test.go
// and the coverage tests do that — so it fails when a locale's forms are
// missing, not when copy changes.
func TestLocalizedDateFormsCoverEveryRequiredLocale(t *testing.T) {
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	// Monday, 5 January 2026: a month and a weekday whose names differ in every
	// currently supported language.
	value := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

	renderers := map[string]func(string, time.Time) string{
		"LocalizedMonthYear":     LocalizedMonthYear,
		"LocalizedDateLabel":     LocalizedDateLabel,
		"LocalizedDashboardDate": LocalizedDashboardDate,
		"LocalizedDateDisplay":   LocalizedDateDisplay,
		"LocalizedDateShort":     LocalizedDateShort,
	}

	for _, language := range languages {
		names, ok := dateNames[language]
		if !ok {
			t.Errorf("locale %q has no dateNames entry: every date surface renders it in English", language)
			continue
		}
		for index, month := range names.months {
			if month == "" || names.monthsLong[index] == "" || names.monthsShort[index] == "" {
				t.Errorf("locale %q is missing a month form at index %d (standalone=%q long=%q short=%q)",
					language, index, month, names.monthsLong[index], names.monthsShort[index])
			}
		}
		for index, weekday := range names.weekdaysShort {
			if weekday == "" || names.weekdaysLong[index] == "" {
				t.Errorf("locale %q is missing a weekday form at index %d (short=%q long=%q)",
					language, index, weekday, names.weekdaysLong[index])
			}
		}

		if language == i18n.LangEN {
			continue
		}
		for name, render := range renderers {
			english := render(i18n.LangEN, value)
			if got := render(language, value); got == english {
				t.Errorf("%s: locale %q renders the English form %q", name, language, english)
			}
		}
	}
}

// TestLocalizedDateFormsFallBackToEnglishForAnUnknownLanguage pins the arm the
// sweep above cannot reach: every language it walks has a table entry, so the
// lookup's miss branch is only taken by a language that is not in the table at
// all. The tables replaced per-form length checks and stdlib fallbacks, so this
// one branch is now the whole safety net — a language string that reaches these
// renderers from outside the supported set must render English words, never the
// zero value of a fixed-size array.
func TestLocalizedDateFormsFallBackToEnglishForAnUnknownLanguage(t *testing.T) {
	value := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	renderers := map[string]func(string, time.Time) string{
		"LocalizedMonthYear":     LocalizedMonthYear,
		"LocalizedDateLabel":     LocalizedDateLabel,
		"LocalizedDashboardDate": LocalizedDashboardDate,
		"LocalizedDateDisplay":   LocalizedDateDisplay,
		"LocalizedDateShort":     LocalizedDateShort,
	}

	// "" is the unset language every request-free surface can carry; "zz" is a
	// well-formed code no catalogue defines.
	for _, language := range []string{"", "zz", "en-GB"} {
		for name, render := range renderers {
			want := render(i18n.LangEN, value)
			got := render(language, value)
			if got != want {
				t.Errorf("%s(%q) = %q, want the English rendering %q", name, language, got, want)
			}
			if strings.TrimSpace(got) == "" {
				t.Errorf("%s(%q) rendered blank: the lookup returned a zero-valued table", name, language)
			}
		}
	}
}
