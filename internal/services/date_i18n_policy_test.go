package services

import (
	"testing"
	"time"
)

func TestLocalizedDashboardDateRussian(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	got := LocalizedDashboardDate("ru", value)
	want := "18 февраля 2026, среда"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedDashboardDateGerman(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	got := LocalizedDashboardDate("de", value)
	want := "Mittwoch, 18. Februar 2026"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedDashboardDateEnglish(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	got := LocalizedDashboardDate("en", value)
	want := "February 18, 2026, Wednesday"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedDashboardDateSpanish(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	got := LocalizedDashboardDate("es", value)
	want := "18 de febrero de 2026, miércoles"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedDashboardDateFrench(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	got := LocalizedDashboardDate("fr", value)
	want := "mercredi 18 février 2026"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedMonthYear(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	if got := LocalizedMonthYear("de", value); got != "Februar 2026" {
		t.Fatalf("expected german month-year, got %q", got)
	}
	if got := LocalizedMonthYear("ru", value); got != "Февраль 2026" {
		t.Fatalf("expected russian month-year, got %q", got)
	}
	if got := LocalizedMonthYear("en", value); got != "February 2026" {
		t.Fatalf("expected english month-year, got %q", got)
	}
	if got := LocalizedMonthYear("es", value); got != "Febrero 2026" {
		t.Fatalf("expected spanish month-year, got %q", got)
	}
	if got := LocalizedMonthYear("fr", value); got != "Février 2026" {
		t.Fatalf("expected french month-year, got %q", got)
	}
}

func TestLocalizedDateLabel(t *testing.T) {
	value := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	if got := LocalizedDateLabel("de", value); got != "Mi., 18. Feb." {
		t.Fatalf("expected german date label, got %q", got)
	}
	if got := LocalizedDateLabel("ru", value); got != "Ср, 18 февраля" {
		t.Fatalf("expected russian date label, got %q", got)
	}
	if got := LocalizedDateLabel("en", value); got != "Wed, Feb 18" {
		t.Fatalf("expected english date label, got %q", got)
	}
	if got := LocalizedDateLabel("es", value); got != "mié, 18 feb" {
		t.Fatalf("expected spanish date label, got %q", got)
	}
	if got := LocalizedDateLabel("fr", value); got != "mer 18 fév" {
		t.Fatalf("expected french date label, got %q", got)
	}
}

func TestLocalizedDateDisplay(t *testing.T) {
	value := time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC)

	if got := LocalizedDateDisplay("de", value); got != "29. Jan. 2026" {
		t.Fatalf("expected german display date, got %q", got)
	}
	if got := LocalizedDateDisplay("ru", value); got != "29.01.2026" {
		t.Fatalf("expected russian display date, got %q", got)
	}
	if got := LocalizedDateDisplay("en", value); got != "Jan 29, 2026" {
		t.Fatalf("expected english display date, got %q", got)
	}
	if got := LocalizedDateDisplay("es", value); got != "29 ene 2026" {
		t.Fatalf("expected spanish display date, got %q", got)
	}
	if got := LocalizedDateDisplay("fr", value); got != "29 jan 2026" {
		t.Fatalf("expected french display date, got %q", got)
	}
}

func TestLocalizedDateShort(t *testing.T) {
	value := time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC)

	if got := LocalizedDateShort("de", value); got != "29. Jan." {
		t.Fatalf("expected german short date, got %q", got)
	}
	if got := LocalizedDateShort("ru", value); got != "29.01" {
		t.Fatalf("expected russian short date, got %q", got)
	}
	if got := LocalizedDateShort("en", value); got != "Jan 29" {
		t.Fatalf("expected english short date, got %q", got)
	}
	if got := LocalizedDateShort("es", value); got != "29 ene" {
		t.Fatalf("expected spanish short date, got %q", got)
	}
	if got := LocalizedDateShort("fr", value); got != "29 jan" {
		t.Fatalf("expected french short date, got %q", got)
	}
}
