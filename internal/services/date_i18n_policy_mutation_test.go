package services

import (
	"testing"
	"time"
)

// TestLocalizedDateDisplayRendersAnUnpaddedYear pins the year rendering of the
// compact day-month form. Both cases used to exist to kill boundary mutants on
// the `monthIndex < 0` terms in localizedDayMonth: a 3-digit year was the way
// to observe the stdlib fallback, whose "2006" layout zero-pads the year while
// the locale path prints it with %d. The fixed-size [12]string tables removed
// both the bounds terms and the fallback, so what is left is the assertion that
// was always about behaviour — the year is printed as written, in January as in
// any other month.
func TestLocalizedDateDisplayRendersAnUnpaddedYear(t *testing.T) {
	tests := []struct {
		name  string
		value time.Time
		want  string
	}{
		{name: "january", value: time.Date(999, time.January, 5, 0, 0, 0, 0, time.UTC), want: "Jan 5, 999"},
		{name: "february", value: time.Date(999, time.February, 5, 0, 0, 0, 0, time.UTC), want: "Feb 5, 999"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := LocalizedDateDisplay("en", testCase.value); got != testCase.want {
				t.Fatalf("LocalizedDateDisplay(en, %s) = %q, want %q", testCase.name, got, testCase.want)
			}
		})
	}
}
