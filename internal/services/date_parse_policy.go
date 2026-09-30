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

	// ErrDayDateOutOfRange reports a well-formed, real calendar date outside
	// [DayDateMin, DayDateMax]. It is invalid input like the two above, and every
	// caller answers it with the same invalid-date outcome as a malformed value.
	ErrDayDateOutOfRange = errors.New("date is outside the accepted range")
)

// The calendar days ParseDayDate accepts, inclusive, as zero-padded ISO dates.
// The lower edge keeps year 1 — Go's zero time, which the day readers treat as
// "unset" — out of every date input. The upper edge is one day short of year
// 9999's end because a day's read range ends at the NEXT day's midnight:
// 9999-12-31 would end in year 10000, which SQLite's text-stored timestamps
// order before the range start and time.MarshalJSON refuses to encode.
// A four-digit year with fixed-width month and day orders as text exactly as it
// orders as a calendar, so the bound is compared on the parsed components.
const (
	DayDateMin = "1900-01-01"
	DayDateMax = "9999-12-30"
)

var (
	dayDateMinDay = mustParseDayDateBound(DayDateMin)
	dayDateMaxDay = mustParseDayDateBound(DayDateMax)
)

func mustParseDayDateBound(value string) time.Time {
	bound, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err) // codecov:ignore -- DayDateMin and DayDateMax are constant ISO dates; a typo in them fails every test at package init
	}
	return bound
}

// dayDateAccepted reports whether day's calendar date is one ParseDayDate
// accepts, so a surface can decline to offer a date the parse would refuse. It
// compares calendar components, never keys: past year 9999 a key grows a fifth
// digit and "10000-01-01" orders as text before DayDateMax.
func dayDateAccepted(day time.Time) bool {
	calendar := dateOnly(day)
	return !calendar.Before(dayDateMinDay) && !calendar.After(dayDateMaxDay)
}

// ParseDayDate parses a YYYY-MM-DD form value as a calendar day on the
// request-local calendar and returns the start of that day in `location` —
// midnight, or the day's first existing instant when a DST jump skips it.
// A date outside DayDateMin..DayDateMax is refused with ErrDayDateOutOfRange.
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

	// The range is a property of the calendar date, not of an instant, so it is
	// checked on the parsed components before any zone is applied: the accepted
	// set is the same in every request timezone.
	if canonical := parsed.Format("2006-01-02"); canonical < DayDateMin || canonical > DayDateMax {
		return time.Time{}, ErrDayDateOutOfRange
	}

	year, month, day := parsed.Date()

	// StartOfCalendarDay returns the first instant that exists on the requested
	// day whenever one does, so a resolved value carrying a DIFFERENT calendar
	// date is the signal that the zone skipped the whole day: it is time.Date's
	// backward normalization into the previous day, kept there for the stored-
	// value helpers that have no error channel. Reading it as a parsed date is
	// the silent one-day shift, so the input boundary refuses it here instead.
	resolved := StartOfCalendarDay(year, month, day, location)
	if resolvedYear, resolvedMonth, resolvedDay := resolved.Date(); resolvedYear != year || resolvedMonth != month || resolvedDay != day {
		return time.Time{}, ErrDayDateNonexistent
	}

	return resolved, nil
}
