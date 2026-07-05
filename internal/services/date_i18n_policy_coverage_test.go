package services

// date_i18n_policy_coverage_test.go
//
// Targets surviving mutants at lines 55, 69, 77, 102, 136, 144, 152, 161,
// 179, 187, 195, 204 of internal/services/date_i18n_policy.go.
//
// Lines 136, 144, 152, 161, 179, 187, 195, 204 are equivalent mutants:
// the monthIndex bounds guards in LocalizedDateDisplay and LocalizedDateShort
// are unreachable dead-code (time.Month is always 1-12, all locale slices
// have 12 entries), and the existing tests already exercise January for those
// functions. Any surviving mutation on those lines (e.g. || → &&) produces
// identical observable behaviour. No additional tests are needed there.
//
// Lines 55, 69, 77, 102 guard LocalizedMonthYear, LocalizedDateLabel, and
// LocalizedDashboardDate. The existing tests only use February (monthIndex=1).
// A relational-operator mutation of "< 0" to "<= 0" would silently redirect
// January (monthIndex=0) to the English fallback path, which produces a
// different string for non-English locales. Tests below use January to close
// that gap.

import (
	"testing"
	"time"
)

// datei18npolicyCovJanuary is the date used across all coverage tests below.
// January (monthIndex=0) is the specific value needed to catch mutations that
// change the lower-bound check from "< 0" to "<= 0".
var datei18npolicyCovJanuary = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC) // Monday

// ---------------------------------------------------------------------------
// LocalizedMonthYear – line 55 guard
// ---------------------------------------------------------------------------

// TestDateI18nPolicyMonthYearJanuary ensures that January is formatted with
// the locale-specific name, not the English fallback.  A mutation of
// "monthIndex < 0" to "monthIndex <= 0" would redirect index 0 (January) to
// value.Format("January 2006"), producing "January 2026" for every locale
// instead of the expected translated month name.
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
		tc := tc
		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedMonthYear(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedMonthYear(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LocalizedDateLabel – line 69 guard (all locales) and line 77 guard (ru)
// ---------------------------------------------------------------------------

// TestDateI18nPolicyDateLabelJanuary ensures that January is formatted with
// locale-specific month names.  The mutations guarded at lines 69 and 77 would
// cause January to fall through to the English fallback "Mon, Jan 5" instead
// of the correct translated representation.
func TestDateI18nPolicyDateLabelJanuary(t *testing.T) {
	// Jan 5, 2026 is a Monday.
	tests := []struct {
		lang string
		want string
	}{
		// Line 69 guard (general) + line 77 guard (ru long-month lookup)
		{"ru", "Пн, 5 января"},
		// Line 69 guard for other locales
		{"de", "Mo., 5. Jan."},
		{"es", "lun, 5 ene"},
		{"fr", "lun 5 jan"},
		{"en", "Mon, Jan 5"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedDateLabel(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedDateLabel(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LocalizedDashboardDate – line 102 guard
// ---------------------------------------------------------------------------

// TestDateI18nPolicyDashboardDateJanuary ensures that January is formatted
// with locale-specific month names rather than the English fallback.  A
// "monthIndex <= 0" mutation on line 102 would redirect January to
// value.Format("January 2, 2006, Monday") for every locale.
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
		tc := tc
		t.Run(tc.lang, func(t *testing.T) {
			got := LocalizedDashboardDate(tc.lang, datei18npolicyCovJanuary)
			if got != tc.want {
				t.Errorf("LocalizedDashboardDate(%q, Jan 5 2026) = %q; want %q", tc.lang, got, tc.want)
			}
		})
	}
}
