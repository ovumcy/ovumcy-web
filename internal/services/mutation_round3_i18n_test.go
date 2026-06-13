package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func mr3i18nDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// date_i18n_policy.go:55:34 BOUNDARY `monthIndex < 0`->`<= 0`.
// January -> monthIndex 0; with `<= 0` the guard fires and returns English.
func TestMR3I18n_LocalizedMonthYearRuJanuary(t *testing.T) {
	got := LocalizedMonthYear("ru", mr3i18nDate(2026, time.January, 1))
	if want := "Январь 2026"; got != want {
		t.Fatalf("LocalizedMonthYear(ru, Jan 2026) = %q, want %q", got, want)
	}
}

// date_i18n_policy.go:69:34 BOUNDARY in LocalizedDateLabel.
// date_i18n_policy.go:77:35 BOUNDARY (ru long-month guard) -- January keeps both
// guards on the in-range side; mutating either to `<= 0` flips January's branch.
func TestMR3I18n_LocalizedDateLabelRuJanuary(t *testing.T) {
	// 2026-01-05 is a Monday.
	got := LocalizedDateLabel("ru", mr3i18nDate(2026, time.January, 5))
	if want := "Пн, 5 января"; got != want {
		t.Fatalf("LocalizedDateLabel(ru, 2026-01-05) = %q, want %q", got, want)
	}
}

// date_i18n_policy.go:102:34 BOUNDARY in LocalizedDashboardDate.
func TestMR3I18n_LocalizedDashboardDateRuJanuary(t *testing.T) {
	// 2026-01-05 is a Monday -> понедельник.
	got := LocalizedDashboardDate("ru", mr3i18nDate(2026, time.January, 5))
	if want := "5 января 2026, понедельник"; got != want {
		t.Fatalf("LocalizedDashboardDate(ru, 2026-01-05) = %q, want %q", got, want)
	}
}

// date_i18n_policy.go:151:34 BOUNDARY in localizedDayMonth.
// Year 999 January via LocalizedDateDisplay("en", ...): correct format is
// "Jan 5, 999"; the `<= 0` mutant flips January (monthIndex 0) into the
// stdlib fallback which yields "Jan 5, 0999".
func TestMR3I18n_LocalizedDateDisplayEnJanuaryYear999(t *testing.T) {
	got := LocalizedDateDisplay("en", mr3i18nDate(999, time.January, 5))
	if want := "Jan 5, 999"; got != want {
		t.Fatalf("LocalizedDateDisplay(en, 0999-01-05) = %q, want %q", got, want)
	}
}

// i18n_policy.go:170:17 NEGATION `lastDigit == 1`->`!=`.
// 21 -> lastTwoDigits 21 (not teen), lastDigit 1 -> ONE.
func TestMR3I18n_RussianPluralFormOne21(t *testing.T) {
	if got := russianPluralForm(21, "ONE", "FEW", "MANY"); got != "ONE" {
		t.Fatalf("russianPluralForm(21) = %q, want ONE", got)
	}
}

// i18n_policy.go:172:17 BOUNDARY `lastDigit >= 2`->`> 2`.
// 2 -> lastDigit 2 -> FEW; the `> 2` mutant would route 2 to the default MANY.
func TestMR3I18n_RussianPluralFormFew2(t *testing.T) {
	if got := russianPluralForm(2, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(2) = %q, want FEW", got)
	}
}

// i18n_policy.go:172:35 BOUNDARY `lastDigit <= 4`->`< 4`.
// 4 and 24 -> lastDigit 4 -> FEW; the `< 4` mutant routes them to default MANY.
func TestMR3I18n_RussianPluralFormFew4(t *testing.T) {
	if got := russianPluralForm(4, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(4) = %q, want FEW", got)
	}
	if got := russianPluralForm(24, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(24) = %q, want FEW", got)
	}
}

// settings_view_service.go:311:31 NEGATION `==`->`!=` in compareISODate.
// Equal inputs (including with surrounding spaces) must yield 0.
func TestMR3I18n_CompareISODateEqual(t *testing.T) {
	if got := compareISODate("2026-06-01", "2026-06-01"); got != 0 {
		t.Fatalf("compareISODate(equal) = %d, want 0", got)
	}
	if got := compareISODate("  2026-06-01  ", "2026-06-01"); got != 0 {
		t.Fatalf("compareISODate(equal w/ spaces) = %d, want 0", got)
	}
}

// settings_view_service.go:313:12 BOUNDARY+NEGATION `left < right`.
// Distinct dates: less -> -1, greater -> 1. Equal is handled by the prior case,
// so flipping the comparator here must change one of these two results.
func TestMR3I18n_CompareISODateOrdering(t *testing.T) {
	if got := compareISODate("2026-01-01", "2026-06-01"); got != -1 {
		t.Fatalf("compareISODate(less) = %d, want -1", got)
	}
	if got := compareISODate("2026-12-31", "2026-06-01"); got != 1 {
		t.Fatalf("compareISODate(greater) = %d, want 1", got)
	}
}

// symptom_service.go:429:42 NEGATION `leftIndex != rightIndex`->`==`.
// Cramps (canonical index 0) and Bloating (canonical index 3): canonical order
// is the REVERSE of alphabetical (Bloating < Cramps). The `==` mutant skips the
// index-compare branch and falls through to alphabetical order -> Bloating first.
func TestMR3I18n_SortBuiltinCanonicalOverAlphabetical(t *testing.T) {
	symptoms := []models.SymptomType{
		{Name: "Bloating", IsBuiltin: true},
		{Name: "Cramps", IsBuiltin: true},
	}
	SortSymptomsByBuiltinAndName(symptoms)
	if symptoms[0].Name != "Cramps" || symptoms[1].Name != "Bloating" {
		t.Fatalf("canonical order lost: got [%q, %q], want [Cramps, Bloating]",
			symptoms[0].Name, symptoms[1].Name)
	}
}

// symptom_service.go:431:17 NEGATION `leftHas != rightHas`->`==`.
// One known builtin (Cramps) and one unknown builtin (Aardvark). Alphabetically
// Aardvark < Cramps, but known-builtins must sort before unknowns. The `==`
// mutant skips the known-first branch and falls through to alphabetical order
// -> Aardvark first.
func TestMR3I18n_SortBuiltinKnownBeforeUnknown(t *testing.T) {
	symptoms := []models.SymptomType{
		{Name: "Aardvark", IsBuiltin: true},
		{Name: "Cramps", IsBuiltin: true},
	}
	SortSymptomsByBuiltinAndName(symptoms)
	if symptoms[0].Name != "Cramps" || symptoms[1].Name != "Aardvark" {
		t.Fatalf("known-before-unknown lost: got [%q, %q], want [Cramps, Aardvark]",
			symptoms[0].Name, symptoms[1].Name)
	}
}
