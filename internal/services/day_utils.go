package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// DateAtLocation projects an instant-in-time `value` onto the calendar of
// `location` and returns midnight of that calendar day. Use this for
// time.Time values that represent a real instant (time.Now(),
// user.CreatedAt) where the in-location calendar day is what you want.
//
// Do NOT use this for date-only stored values (DailyLog.Date,
// User.LastPeriodStart). Those values carry only a calendar date — their
// time-of-day and timezone metadata are storage artifacts. Applying
// In(location) to a UTC-midnight stored value in a UTC-minus locale shifts
// it one calendar day backward (issue #48). Use CalendarDay for those.
func DateAtLocation(value time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	localized := value.In(location)
	year, month, day := localized.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

// CalendarDay rebuilds a date-only stored value at midnight in `location`,
// preserving the calendar components of `value` exactly as stored. Use this
// for time.Time values whose semantics is "a calendar date" rather than
// "an instant in time" — DailyLog.Date, User.LastPeriodStart, derived stats
// fields. Unlike DateAtLocation, this does not apply In(location) and
// therefore does not shift the calendar day across timezones, which matters
// when stored values were persisted with a UTC-midnight timestamp.
func CalendarDay(value time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	if value.IsZero() {
		return time.Time{}
	}
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

// CalendarDayKey returns the YYYY-MM-DD ISO string for a date-only stored
// value, taking calendar components from the value as-is (no timezone
// shift). Equivalent to value.Format("2006-01-02") on a value that already
// carries the canonical calendar day.
func CalendarDayKey(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	year, month, day := value.Date()
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// DayRange returns the [start, end) bounds for the local calendar day of
// `value` in `location`, expressed as UTC-midnight instants. The local
// calendar day is computed via DateAtLocation; the resulting y/m/d is then
// rebuilt at UTC-midnight so the bounds match the on-disk shape produced
// by DailyLog.BeforeSave (which canonicalizes Date to UTC-midnight). This
// keeps DELETE/UPSERT range queries aligned with stored rows regardless
// of the request timezone offset.
func DayRange(value time.Time, location *time.Location) (time.Time, time.Time) {
	localMidnight := DateAtLocation(value, location)
	year, month, day := localMidnight.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 0, 1)
}

func SameCalendarDay(a time.Time, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func BetweenCalendarDaysInclusive(day time.Time, start time.Time, end time.Time) bool {
	if start.IsZero() || end.IsZero() {
		return false
	}
	return (day.Equal(start) || day.After(start)) && (day.Equal(end) || day.Before(end))
}

func SymptomIDSet(ids []uint) map[uint]bool {
	set := make(map[uint]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func DayHasData(entry models.DailyLog) bool {
	if entry.IsPeriod {
		return true
	}
	if entry.Mood >= MinDayMood && entry.Mood <= MaxDayMood {
		return true
	}
	if NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone {
		return true
	}
	if IsValidDayBBT(entry.BBT) && entry.BBT > 0 {
		return true
	}
	if NormalizeDayCervicalMucus(entry.CervicalMucus) != models.CervicalMucusNone {
		return true
	}
	if len(DayCycleFactorKeySet(entry.CycleFactorKeys)) > 0 {
		return true
	}
	if len(entry.SymptomIDs) > 0 {
		return true
	}
	if strings.TrimSpace(entry.Notes) != "" {
		return true
	}
	return strings.TrimSpace(entry.Flow) != "" && entry.Flow != models.FlowNone
}

func RemoveUint(values []uint, needle uint) []uint {
	filtered := make([]uint, 0, len(values))
	for _, value := range values {
		if value != needle {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
