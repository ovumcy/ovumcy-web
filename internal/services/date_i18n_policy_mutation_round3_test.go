package services

import (
	"testing"
	"time"
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
