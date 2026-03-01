package services

import (
	"errors"
	"testing"
	"time"
)

func TestResolveCalendarMonthAndSelectedDate(t *testing.T) {
	now := time.Date(2026, time.February, 21, 10, 30, 0, 0, time.UTC)

	t.Run("invalid month", func(t *testing.T) {
		_, _, err := ResolveCalendarMonthAndSelectedDate("2026-99", "", now, time.UTC)
		if !errors.Is(err, ErrCalendarMonthInvalid) {
			t.Fatalf("expected ErrCalendarMonthInvalid, got %v", err)
		}
	})

	t.Run("uses selected day month when month missing", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDate("", "2026-02-17", now, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selectedDate != "2026-02-17" {
			t.Fatalf("expected selected date 2026-02-17, got %q", selectedDate)
		}
		if month.Format("2006-01") != "2026-02" {
			t.Fatalf("expected month 2026-02, got %s", month.Format("2006-01"))
		}
	})

	t.Run("keeps explicit month query", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDate("2026-03", "2026-02-17", now, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selectedDate != "2026-02-17" {
			t.Fatalf("expected selected date 2026-02-17, got %q", selectedDate)
		}
		if month.Format("2006-01") != "2026-03" {
			t.Fatalf("expected month 2026-03, got %s", month.Format("2006-01"))
		}
	})

	t.Run("ignores invalid selected day", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDate("2026-03", "invalid-day", now, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selectedDate != "" {
			t.Fatalf("expected empty selected date, got %q", selectedDate)
		}
		if month.Format("2006-01") != "2026-03" {
			t.Fatalf("expected month 2026-03, got %s", month.Format("2006-01"))
		}
	})

	t.Run("defaults selected day to today when both params missing", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDate("", "", now, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selectedDate != "2026-02-21" {
			t.Fatalf("expected selected date 2026-02-21, got %q", selectedDate)
		}
		if month.Format("2006-01") != "2026-02" {
			t.Fatalf("expected month 2026-02, got %s", month.Format("2006-01"))
		}
	})
}

func TestCalendarAdjacentMonthValues(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	prev, next := CalendarAdjacentMonthValues(monthStart)
	if prev != "2026-01" {
		t.Fatalf("expected prev month 2026-01, got %q", prev)
	}
	if next != "2026-03" {
		t.Fatalf("expected next month 2026-03, got %q", next)
	}
}
