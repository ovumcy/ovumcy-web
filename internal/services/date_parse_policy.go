package services

import (
	"errors"
	"strings"
	"time"
)

var ErrDayDateRequired = errors.New("date is required")

// ParseDayDate parses a YYYY-MM-DD form value as a calendar day on the
// request-local calendar and returns the start of that day in `location` —
// midnight, or the day's first existing instant when a DST jump skips it.
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

	// Parsed in UTC, which has no transitions, so the calendar components
	// survive the parse untouched; the day is then resolved in `location`
	// through the package's single midnight-construction point. Parsing in
	// `location` directly would lose the day before that point is ever
	// reached: time.ParseInLocation resolves a nonexistent local midnight the
	// same way time.Date does, and in a UTC-minus zone whose DST jump lands on
	// midnight that resolution normalizes backward into the previous day.
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}

	year, month, day := parsed.Date()

	return startOfCalendarDay(year, month, day, location), nil
}
