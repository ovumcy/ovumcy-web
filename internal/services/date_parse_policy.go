package services

import (
	"errors"
	"strings"
	"time"
)

var ErrDayDateRequired = errors.New("date is required")

// ParseDayDate parses a YYYY-MM-DD form value as a calendar day on the
// request-local calendar and returns midnight of that day in `location`.
// This is the single parse entry point for date inputs: every other
// "2006-01-02" parse in the package routes through it, so the
// canonicalization shape stays a one-place decision. For values destined
// for date-only stored fields (DailyLog.Date, User.LastPeriodStart),
// follow up with CalendarDay(parsed, time.UTC) — see day_utils.go for why
// the two shapes must not be mixed.
func ParseDayDate(raw string, location *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, ErrDayDateRequired
	}

	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, err
	}

	return DateAtLocation(parsed, location), nil
}
