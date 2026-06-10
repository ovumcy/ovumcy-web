package services

import (
	"testing"
	"time"
)

func TestLocalizedDateDisplayEnglishJanuaryThreeDigitYear(t *testing.T) {
	// monthIndex==0 (January) is the only index where `monthIndex < 0` differs
	// from the mutated `monthIndex <= 0`. A 3-digit year makes the locale path
	// (year via %d) differ from the bounds-fallback value.Format("Jan 2, 2006")
	// (year zero-padded to 4 digits), so taking the fallback is observable.
	value := time.Date(999, time.January, 5, 0, 0, 0, 0, time.UTC)

	got := LocalizedDateDisplay("en", value)
	want := "Jan 5, 999"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizedDateDisplayEnglishAlwaysLocalePathThreeDigitYear(t *testing.T) {
	// Negating `monthIndex < 0` to `monthIndex >= 0` makes the bounds guard
	// always true, forcing every month onto the fallback value.Format("Jan 2, 2006").
	// February (monthIndex==1) is unaffected by the January-only boundary flip,
	// so it specifically pins the always-locale-path behavior. A 3-digit year
	// exposes the fallback via year zero-padding.
	value := time.Date(999, time.February, 5, 0, 0, 0, 0, time.UTC)

	got := LocalizedDateDisplay("en", value)
	want := "Feb 5, 999"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
