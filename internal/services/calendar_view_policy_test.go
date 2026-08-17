package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A zero minMonth is the "no lower bound" input: the clamp and the
// selected-date reset both stay inert, which is the behaviour these cases pin.
func TestResolveCalendarMonthAndSelectedDateWithoutMinimumMonth(t *testing.T) {
	now := time.Date(2026, time.February, 21, 10, 30, 0, 0, time.UTC)

	t.Run("invalid month", func(t *testing.T) {
		_, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2026-99", "", now, time.UTC, time.Time{})
		if !errors.Is(err, ErrCalendarMonthInvalid) {
			t.Fatalf("expected ErrCalendarMonthInvalid, got %v", err)
		}
	})

	t.Run("uses selected day month when month missing", func(t *testing.T) {
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("", "2026-02-17", now, time.UTC, time.Time{})
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
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2026-03", "2026-02-17", now, time.UTC, time.Time{})
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
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2026-03", "invalid-day", now, time.UTC, time.Time{})
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
		month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("", "", now, time.UTC, time.Time{})
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

// With a zero minMonth there is no lower bound, so the previous month is always
// offered — the complement of the bounded case below.
func TestCalendarAdjacentMonthValuesWithoutMinimumMonth(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	prev, next := CalendarAdjacentMonthValuesWithinBounds(monthStart, time.Time{})
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

// TestCalendarMonthAnchorsSurviveASkippedFirstOfMonthMidnight pins the whole
// month-anchoring surface of this file against the one date shape that breaks
// it: a UTC-minus zone whose DST jump lands exactly on the FIRST of a month.
// No instant exists at 00:00 local that day, and a plain time.Date /
// ParseInLocation resolves the missing wall clock BACKWARD into the last day of
// the PREVIOUS month. Every value derived from that anchor then names the wrong
// month — the rendered grid, the month label, the ?month= value, both
// navigation links, and the lower navigation bound.
//
// The two reproducing pairs come from a full scan of the zone database shipped
// with the toolchain (598 zones, tzdata 2025c, every date 1970-01-01..2040-12-31):
// 66 zone/date pairs skip midnight on the 1st, all of them west of UTC.
//
//	America/Asuncion 2023-10-01 → time.Date(...) = 2023-09-30 23:00 -04:00
//	America/Havana   2012-04-01 → time.Date(...) = 2012-03-31 23:00 -05:00
//
// East-of-UTC zones normalize FORWARD and keep the date, so a test on one of
// them goes green while proving nothing; both controls below therefore hold the
// ordinary path instead — the same zone in a month with no transition, and UTC.
func TestCalendarMonthAnchorsSurviveASkippedFirstOfMonthMidnight(t *testing.T) {
	asuncion, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		t.Fatalf("load America/Asuncion: %v", err)
	}
	havana, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatalf("load America/Havana: %v", err)
	}

	testCases := []struct {
		name        string
		location    *time.Location
		month       string
		selectedDay string
		prevMonth   string
		nextMonth   string
		skipsFirst  bool
	}{
		{
			name:        "asuncion october 2023 skips the first midnight",
			location:    asuncion,
			month:       "2023-10",
			selectedDay: "2023-10-15",
			prevMonth:   "2023-09",
			nextMonth:   "2023-11",
			skipsFirst:  true,
		},
		{
			name:        "havana april 2012 skips the first midnight",
			location:    havana,
			month:       "2012-04",
			selectedDay: "2012-04-15",
			prevMonth:   "2012-03",
			nextMonth:   "2012-05",
			skipsFirst:  true,
		},
		{
			// November 2023 has an ordinary first, so its own anchor is fine; the
			// PREVIOUS-month link steps onto 2023-10-01 and is not.
			name:        "asuncion november 2023 steps back onto a skipped first",
			location:    asuncion,
			month:       "2023-11",
			selectedDay: "2023-11-15",
			prevMonth:   "2023-10",
			nextMonth:   "2023-12",
		},
		{
			name:        "control: asuncion june 2023, no transition in reach",
			location:    asuncion,
			month:       "2023-06",
			selectedDay: "2023-06-15",
			prevMonth:   "2023-05",
			nextMonth:   "2023-07",
		},
		{
			name:        "control: utc october 2023",
			location:    time.UTC,
			month:       "2023-10",
			selectedDay: "2023-10-15",
			prevMonth:   "2023-09",
			nextMonth:   "2023-11",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requested, err := time.Parse("2006-01", testCase.month)
			if err != nil {
				t.Fatalf("parse the requested month %q: %v", testCase.month, err)
			}
			// Re-measure the premise against the tzdata actually linked into this
			// run rather than trusting the table: a zone-database revision that
			// moves either transition must fail loudly here, not turn the case
			// into a control that silently proves nothing.
			probe := time.Date(requested.Year(), requested.Month(), 1, 0, 0, 0, 0, testCase.location)
			skipped := probe.Month() != requested.Month()
			if skipped != testCase.skipsFirst {
				t.Fatalf("premise drifted: time.Date(1st of %s, %s) = %s, so midnight-skipped=%t but the case expects %t — rescan the zone database and repin",
					testCase.month, testCase.location, probe.Format(time.RFC3339), skipped, testCase.skipsFirst)
			}

			now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)

			t.Run("month query", func(t *testing.T) {
				month, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds(testCase.month, "", now, testCase.location, time.Time{})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := month.Format("2006-01"); got != testCase.month {
					t.Fatalf("?month=%s rendered %s", testCase.month, got)
				}

				prev, next := CalendarAdjacentMonthValuesWithinBounds(month, time.Time{})
				if prev != testCase.prevMonth {
					t.Fatalf("previous month link for %s = %q, want %q", testCase.month, prev, testCase.prevMonth)
				}
				if next != testCase.nextMonth {
					t.Fatalf("next month link for %s = %q, want %q", testCase.month, next, testCase.nextMonth)
				}
			})

			t.Run("month derived from the selected day", func(t *testing.T) {
				month, selectedDate, err := ResolveCalendarMonthAndSelectedDateWithinBounds("", testCase.selectedDay, now, testCase.location, time.Time{})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if selectedDate != testCase.selectedDay {
					t.Fatalf("selected date = %q, want %q", selectedDate, testCase.selectedDay)
				}
				if got := month.Format("2006-01"); got != testCase.month {
					t.Fatalf("selected day %s put the grid on %s, want %s", testCase.selectedDay, got, testCase.month)
				}
			})

			t.Run("minimum navigable month", func(t *testing.T) {
				// The bound is the month of CreatedAt minus three years, so an
				// account created three years after the transition date has its
				// lower bound land on exactly that first-of-month.
				user := &models.User{
					CreatedAt: time.Date(requested.Year()+3, requested.Month(), 14, 12, 0, 0, 0, time.UTC),
				}
				minMonth := CalendarMinimumNavigableMonth(user, testCase.location)
				if got := minMonth.Format("2006-01"); got != testCase.month {
					t.Fatalf("minimum navigable month = %s, want %s", got, testCase.month)
				}
			})

			t.Run("clamp to the minimum month", func(t *testing.T) {
				minMonth := time.Date(requested.Year(), requested.Month(), 1, 0, 0, 0, 0, time.UTC)
				month, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2001-01", "", now, testCase.location, minMonth)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := month.Format("2006-01"); got != testCase.month {
					t.Fatalf("month clamped to the lower bound = %s, want %s", got, testCase.month)
				}
			})
		})
	}
}
