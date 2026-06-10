package services

import (
	"testing"
	"time"
)

func TestCalendarMonthBefore_SelectedDayInExactMinimumMonthKept(t *testing.T) {
	now := time.Date(2026, time.February, 21, 10, 30, 0, 0, time.UTC)
	minMonth := time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC)

	// A day that falls inside the exact minimum month must NOT be dropped:
	// calendarMonthBefore(2023-03-15, 2023-03-01) is false under correct `<`
	// at line 115, but true under the `<=` mutation, which would clear the
	// selected date. Explicit month query keeps activeMonth at 2023-03 so the
	// selected-day month override does not interfere.
	month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2023-03", "2023-03-15", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selectedDate != "2023-03-15" {
		t.Fatalf("selected day in exact minimum month should be kept, got %q want 2023-03-15", selectedDate)
	}
	if month.Format("2006-01") != "2023-03" {
		t.Fatalf("month equal to minimum should not be clamped, got %s want 2023-03", month.Format("2006-01"))
	}
}
