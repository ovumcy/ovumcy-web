package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
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

func TestResolveCalendarMonthAndSelectedDateWithinBounds(t *testing.T) {
	now := time.Date(2026, time.February, 21, 10, 30, 0, 0, time.UTC)
	minMonth := time.Date(2023, time.February, 1, 0, 0, 0, 0, time.UTC)

	t.Run("clamps explicit month before minimum", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2020-01", "", now, time.UTC, minMonth)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if month.Format("2006-01") != "2023-02" {
			t.Fatalf("expected clamped month 2023-02, got %s", month.Format("2006-01"))
		}
		if selectedDate != "" {
			t.Fatalf("expected empty selected date, got %q", selectedDate)
		}
	})

	t.Run("drops selected date before minimum month", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("", "2022-12-17", now, time.UTC, minMonth)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if month.Format("2006-01") != "2023-02" {
			t.Fatalf("expected clamped month 2023-02, got %s", month.Format("2006-01"))
		}
		if selectedDate != "" {
			t.Fatalf("expected selected date to be cleared, got %q", selectedDate)
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

func TestCalendarAdjacentMonthValuesWithinBounds(t *testing.T) {
	monthStart := time.Date(2023, time.February, 1, 0, 0, 0, 0, time.UTC)
	minMonth := time.Date(2023, time.February, 1, 0, 0, 0, 0, time.UTC)

	prev, next := CalendarAdjacentMonthValuesWithinBounds(monthStart, minMonth)
	if prev != "" {
		t.Fatalf("expected empty prev month at lower bound, got %q", prev)
	}
	if next != "2023-03" {
		t.Fatalf("expected next month 2023-03, got %q", next)
	}
}

func TestCalendarMinimumNavigableMonth(t *testing.T) {
	user := &models.User{
		CreatedAt: time.Date(2026, time.March, 13, 14, 30, 0, 0, time.UTC),
	}

	minMonth := CalendarMinimumNavigableMonth(user, time.UTC)
	if minMonth.Format("2006-01-02") != "2023-03-01" {
		t.Fatalf("expected minimum month 2023-03-01, got %s", minMonth.Format("2006-01-02"))
	}
}
