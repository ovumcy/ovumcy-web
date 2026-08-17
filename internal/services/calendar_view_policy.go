package services

import (
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var ErrCalendarMonthInvalid = errors.New("calendar invalid month")

// ResolveCalendarMonthAndSelectedDateWithinBounds resolves the active month and
// the selected day. A zero minMonth means no lower bound: calendarMonthBefore
// reports false for every month, so neither the clamp nor the selected-date
// reset fires.
func ResolveCalendarMonthAndSelectedDateWithinBounds(monthQueryRaw string, selectedDayRaw string, now time.Time, location *time.Location, minMonth time.Time) (time.Time, string, error) {
	if location == nil {
		location = time.UTC
	}

	monthQuery := strings.TrimSpace(monthQueryRaw)
	activeMonth, err := parseCalendarMonthQuery(monthQuery, now, location)
	if err != nil {
		return time.Time{}, "", ErrCalendarMonthInvalid
	}

	selectedDate := ""
	selectedDayRaw = strings.TrimSpace(selectedDayRaw)
	if selectedDayRaw != "" {
		if selectedDay, parseErr := parseCalendarDayParam(selectedDayRaw, location); parseErr == nil {
			selectedDate = selectedDay.Format("2006-01-02")
			if monthQuery == "" {
				activeMonth = calendarMonthAnchor(selectedDay, location)
			}
		}
	}
	if selectedDate == "" && monthQuery == "" {
		selectedDate = DateAtLocation(now, location).Format("2006-01-02")
	}

	activeMonth = clampCalendarMonthToMinimum(activeMonth, minMonth, location)
	if selectedDate != "" {
		selectedDay, parseErr := parseCalendarDayParam(selectedDate, location)
		if parseErr == nil && calendarMonthBefore(selectedDay, minMonth) {
			selectedDate = ""
		}
	}

	return activeMonth, selectedDate, nil
}

// CalendarAdjacentMonthValuesWithinBounds returns the previous and next month
// values for the navigation controls; the previous one is empty when it would
// fall before minMonth. A zero minMonth means no lower bound.
func CalendarAdjacentMonthValuesWithinBounds(monthStart time.Time, minMonth time.Time) (string, string) {
	// Stepping a month sideways is pure calendar arithmetic, so it runs on a
	// UTC-anchored copy — the convention calendarGridBounds already follows. Run
	// in the request zone instead, AddDate lands on a wall clock that a DST jump
	// on the target month's FIRST does not have, and resolves it backward into
	// the month before: the "previous month" link then skips a month entirely,
	// and the "next" link points back at the page the reader is already on.
	base := CalendarDay(monthStart, time.UTC)
	prevMonth := base.AddDate(0, -1, 0)
	prevValue := prevMonth.Format("2006-01")
	if calendarMonthBefore(prevMonth, minMonth) {
		prevValue = ""
	}
	return prevValue, base.AddDate(0, 1, 0).Format("2006-01")
}

func CalendarMinimumNavigableMonth(user *models.User, location *time.Location) time.Time {
	if user == nil || user.CreatedAt.IsZero() {
		return time.Time{}
	}
	if location == nil {
		location = time.UTC
	}

	// The three-year step is calendar arithmetic on a date, so it too runs
	// UTC-anchored: taken in the request zone it can land on a first-of-month
	// whose local midnight a DST jump skipped and slide the bound a month
	// earlier than the account's own history justifies.
	createdDay := CalendarDay(DateAtLocation(user.CreatedAt, location), time.UTC)
	return calendarMonthAnchor(createdDay.AddDate(-3, 0, 0), location)
}

func parseCalendarMonthQuery(raw string, now time.Time, location *time.Location) (time.Time, error) {
	if raw == "" {
		return calendarMonthAnchor(DateAtLocation(now, location), location), nil
	}
	// Parsed in UTC, which has no transitions, so the requested year and month
	// survive the parse untouched — the same reason ParseDayDate parses there.
	// time.ParseInLocation resolves a nonexistent local midnight exactly as
	// time.Date does, so in a UTC-minus zone whose DST jump lands on the first
	// of the requested month it returned the LAST day of the previous month, and
	// the whole page then rendered that month instead.
	parsed, err := time.Parse("2006-01", raw)
	if err != nil {
		return time.Time{}, err
	}
	return calendarMonthAnchor(parsed, location), nil
}

// calendarMonthAnchor returns the first day of value's calendar month, resolved
// in location through the package's single day-construction point
// (startOfCalendarDay, via CalendarDay): midnight, or the day's first existing
// instant when a DST jump skipped it. The month step itself is taken on a
// UTC-anchored copy, where no midnight is ever missing, so the year and month
// of the result always equal those of value. A zero value stays zero: CalendarDay
// passes it through, and the day step is a no-op on it.
func calendarMonthAnchor(value time.Time, location *time.Location) time.Time {
	utcDay := CalendarDay(value, time.UTC)
	return CalendarDay(utcDay.AddDate(0, 0, 1-utcDay.Day()), location)
}

func parseCalendarDayParam(raw string, location *time.Location) (time.Time, error) {
	return ParseDayDate(raw, location)
}

func clampCalendarMonthToMinimum(monthStart time.Time, minMonth time.Time, location *time.Location) time.Time {
	if calendarMonthBefore(monthStart, minMonth) {
		return calendarMonthAnchor(minMonth, resolveCalendarLocation(location, minMonth))
	}
	return monthStart
}

func calendarMonthBefore(month time.Time, minMonth time.Time) bool {
	if minMonth.IsZero() {
		return false
	}

	monthYear, monthNumber, _ := month.Date()
	minYear, minNumber, _ := minMonth.Date()
	if monthYear != minYear {
		return monthYear < minYear
	}
	return monthNumber < minNumber
}

func resolveCalendarLocation(location *time.Location, fallback time.Time) *time.Location {
	if location != nil {
		return location
	}
	if fallback.Location() != nil {
		return fallback.Location()
	}
	return time.UTC
}
