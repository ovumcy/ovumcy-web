package services

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrDayDateRequired = errors.New("date is required")

	// ErrDayDateNonexistent reports a syntactically valid date that the request
	// zone never had — a zone crossing the date line skips a whole calendar day
	// (Pacific/Apia 2011-12-30, Pacific/Kiritimati 1994-12-31). It is invalid
	// input, not a date to resolve: there is no instant to resolve it to.
	ErrDayDateNonexistent = errors.New("date does not exist in this timezone")
)

// ParseDayDate parses a YYYY-MM-DD form value as a calendar day on the
// request-local calendar and returns the start of that day in `location` —
// midnight, or the day's first existing instant when a DST jump skips it.
// A date the zone never had at all is refused with ErrDayDateNonexistent.
// The two cases are deliberately different: a missing MIDNIGHT still names a
// real day and resolves forward to its first instant, while a missing DAY has
// no instant to resolve to and is therefore invalid input.
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

	// startOfCalendarDay returns the first instant that exists on the requested
	// day whenever one does, so a resolved value carrying a DIFFERENT calendar
	// date is the signal that the zone skipped the whole day: it is time.Date's
	// backward normalization into the previous day, kept there for the stored-
	// value helpers that have no error channel. Reading it as a parsed date is
	// the silent one-day shift, so the input boundary refuses it here instead.
	resolved := startOfCalendarDay(year, month, day, location)
	if resolvedYear, resolvedMonth, resolvedDay := resolved.Date(); resolvedYear != year || resolvedMonth != month || resolvedDay != day {
		return time.Time{}, ErrDayDateNonexistent
	}

	return resolved, nil
}
