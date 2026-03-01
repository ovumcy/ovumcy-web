package services

import (
	"errors"
	"strings"
	"time"
)

var ErrCalendarMonthInvalid = errors.New("calendar invalid month")

func ResolveCalendarMonthAndSelectedDate(monthQueryRaw string, selectedDayRaw string, now time.Time, location *time.Location) (time.Time, string, error) {
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

	return activeMonth, selectedDate, nil
}

func CalendarAdjacentMonthValues(monthStart time.Time) (string, string) {
	return monthStart.AddDate(0, -1, 0).Format("2006-01"), monthStart.AddDate(0, 1, 0).Format("2006-01")
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
