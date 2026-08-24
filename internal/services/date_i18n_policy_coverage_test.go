package services

// date_i18n_policy_coverage_test.go
//
// Per-locale January assertions for LocalizedMonthYear, LocalizedDateLabel and
// LocalizedDashboardDate in internal/services/date_i18n_policy.go.
//
// These were written against monthIndex bounds guards that no longer exist:
// the five parallel name maps became one fixed-size table per language
// (localizedDateNames), and with [12]string / [7]string fields every index is
// in range by construction — time.Month() is 1-12 and time.Weekday() is 0-6 —
// so there is no lower-bound term to mutate and no stdlib fallback to be
// redirected onto. What survives here is what was always worth keeping: each
// locale renders January in its own words, which fails if a language's forms
// go missing or the language dispatch is mutated. Table completeness for a
// newly added locale is TestLocalizedDateFormsCoverEveryRequiredLocale's job.

import (
	"testing"
	"time"
)

// datei18npolicyCovJanuary is the date used across all coverage tests below:
// the first month and a weekday whose name differs in every supported language.
var datei18npolicyCovJanuary = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC) // Monday

// ---------------------------------------------------------------------------
// LocalizedMonthYear
// ---------------------------------------------------------------------------

// TestDateI18nPolicyMonthYearJanuary ensures that January is formatted with the
// locale-specific standalone month name — the heading form, title-cased where
// the language's running-text form is not. Russian is the language where that
// distinction is visible: the standalone nominative "Январь", never the
// genitive "января" the running-text surfaces use. January is also the first
// index into each language's month table, so this table pins the 0-based
// conversion for every locale at once.
//
// These three tables are the package's only kills for the Russian January
// outputs; a round-three file restated the same three calls and the same three
// expected strings, and was removed rather than kept in parallel.
func TestDateI18nPolicyMonthYearJanuary(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{"de", "Januar 2026"},
		{"es", "Enero 2026"},
		{"fr", "Janvier 2026"},
		{"ru", "Январь 2026"},
		{"en", "January 2026"},
	}
	for _, tc := range tests {

		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedMonthYear(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedMonthYear(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LocalizedDateLabel (all locales, and the Russian long-month path)
// ---------------------------------------------------------------------------

// TestDateI18nPolicyDateLabelJanuary ensures that January is formatted with
// locale-specific month names, including the Russian arm that reaches for the
// genitive long form instead of the abbreviation the others use.
func TestDateI18nPolicyDateLabelJanuary(t *testing.T) {
	// Jan 5, 2026 is a Monday.
	tests := []struct {
		lang string
		want string
	}{
		// The Russian arm renders the genitive long month, not the abbreviation.
		{"ru", "Пн, 5 января"},
		{"de", "Mo., 5. Jan."},
		{"es", "lun, 5 ene"},
		{"fr", "lun 5 jan"},
		{"en", "Mon, Jan 5"},
	}
	for _, tc := range tests {

		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedDateLabel(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedDateLabel(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LocalizedDashboardDate
// ---------------------------------------------------------------------------

// TestDateI18nPolicyDashboardDateJanuary ensures that January is formatted with
// locale-specific long month and weekday names, in each language's own word
// order.
func TestDateI18nPolicyDashboardDateJanuary(t *testing.T) {
	// Jan 5, 2026 is a Monday.
	tests := []struct {
		lang string
		want string
	}{
		{"ru", "5 января 2026, понедельник"},
		{"de", "Montag, 5. Januar 2026"},
		{"es", "5 de enero de 2026, lunes"},
		{"fr", "lundi 5 janvier 2026"},
		{"en", "January 5, 2026, Monday"},
	}
	for _, tc := range tests {

		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedDashboardDate(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedDashboardDate(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}
