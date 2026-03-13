package services

import (
	"errors"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var ErrCalendarMonthInvalid = errors.New("calendar invalid month")

func ResolveCalendarMonthAndSelectedDate(monthQueryRaw string, selectedDayRaw string, now time.Time, location *time.Location) (time.Time, string, error) {
	return ResolveCalendarMonthAndSelectedDateWithinBounds(monthQueryRaw, selectedDayRaw, now, location, time.Time{})
}

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
				activeMonth = time.Date(selectedDay.Year(), selectedDay.Month(), 1, 0, 0, 0, 0, location)
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

func CalendarAdjacentMonthValues(monthStart time.Time) (string, string) {
	return CalendarAdjacentMonthValuesWithinBounds(monthStart, time.Time{})
}

func CalendarAdjacentMonthValuesWithinBounds(monthStart time.Time, minMonth time.Time) (string, string) {
	prevMonth := monthStart.AddDate(0, -1, 0)
	prevValue := prevMonth.Format("2006-01")
	if calendarMonthBefore(prevMonth, minMonth) {
		prevValue = ""
	}
	return prevValue, monthStart.AddDate(0, 1, 0).Format("2006-01")
}

func CalendarMinimumNavigableMonth(user *models.User, location *time.Location) time.Time {
	if user == nil || user.CreatedAt.IsZero() {
		return time.Time{}
	}
	if location == nil {
		location = time.UTC
	}

	minDate := DateAtLocation(user.CreatedAt, location).AddDate(-3, 0, 0)
	return time.Date(minDate.Year(), minDate.Month(), 1, 0, 0, 0, 0, location)
}

func parseCalendarMonthQuery(raw string, now time.Time, location *time.Location) (time.Time, error) {
	if raw == "" {
		current := DateAtLocation(now, location)
		return time.Date(current.Year(), current.Month(), 1, 0, 0, 0, 0, location), nil
	}
	parsed, err := time.ParseInLocation("2006-01", raw, location)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, location), nil
}

func parseCalendarDayParam(raw string, location *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		return time.Time{}, err
	}
	return DateAtLocation(parsed, location), nil
}

func clampCalendarMonthToMinimum(monthStart time.Time, minMonth time.Time, location *time.Location) time.Time {
	if calendarMonthBefore(monthStart, minMonth) {
		return time.Date(minMonth.Year(), minMonth.Month(), 1, 0, 0, 0, 0, resolveCalendarLocation(location, minMonth))
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
