package services

import (
	"testing"
	"time"
)

func mr3i18nDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// LocalizedMonthYear renders the Russian standalone (nominative) month name,
// not the genitive running-text form and not the English default. January is
// the first index into the table, so it also pins the 0-based conversion.
func TestMR3I18n_LocalizedMonthYearRuJanuary(t *testing.T) {
	got := LocalizedMonthYear("ru", mr3i18nDate(2026, time.January, 1))
	if want := "Январь 2026"; got != want {
		t.Fatalf("LocalizedMonthYear(ru, Jan 2026) = %q, want %q", got, want)
	}
}

// LocalizedDateLabel's Russian arm pairs the short weekday with the genitive
// long month — the one language where the label does not use the abbreviation.
func TestMR3I18n_LocalizedDateLabelRuJanuary(t *testing.T) {
	// 2026-01-05 is a Monday.
	got := LocalizedDateLabel("ru", mr3i18nDate(2026, time.January, 5))
	if want := "Пн, 5 января"; got != want {
		t.Fatalf("LocalizedDateLabel(ru, 2026-01-05) = %q, want %q", got, want)
	}
}

// LocalizedDashboardDate's Russian arm: day, genitive month, year, then the
// long weekday last.
func TestMR3I18n_LocalizedDashboardDateRuJanuary(t *testing.T) {
	// 2026-01-05 is a Monday -> понедельник.
	got := LocalizedDashboardDate("ru", mr3i18nDate(2026, time.January, 5))
	if want := "5 января 2026, понедельник"; got != want {
		t.Fatalf("LocalizedDashboardDate(ru, 2026-01-05) = %q, want %q", got, want)
	}
}
