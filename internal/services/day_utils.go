package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// DateAtLocation projects an instant-in-time `value` onto the calendar of
// `location` and returns the start of that calendar day — midnight, or the
// day's first existing instant when a DST jump skips it (StartOfCalendarDay).
// Use this for time.Time values that represent a real instant (time.Now(),
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
	return StartOfCalendarDay(year, month, day, location)
}

// StartOfCalendarDay returns the first instant that actually exists on the
// given calendar day in `location`. It is the one construction point for
// every "midnight of this calendar day" value in the package, so the
// YYYY-MM-DD key a helper is handed always round-trips. It is exported for
// the same reason it exists: the reminder scheduler resolves the day of its
// daily pass the same way, and a second implementation of this rule is how
// the class of defects it fixes comes back.
//
// `location` must not be nil — every caller resolves that first (the wrappers
// below and reminders.nextRun default it to UTC).
//
// Plain time.Date cannot be used directly: in a UTC-minus zone whose DST jump
// lands exactly on midnight (America/Santiago, America/Havana), local midnight
// does not exist and time.Date resolves the nonexistent wall clock through its
// `utc >= end` branch, which normalizes BACKWARD into the previous calendar
// day — the spring-forward date could then not be entered at all. Positive
// offsets take the `utc < start` branch and normalize forward within the same
// day, so they were never affected and are left byte-for-byte alone here, as
// are zones without transitions (UTC included): the fallback below only fires
// when the requested day did not survive the construction.
func StartOfCalendarDay(year int, month time.Month, day int, location *time.Location) time.Time {
	candidate := time.Date(year, month, day, 0, 0, 0, 0, location)
	if y, m, d := candidate.Date(); y == year && m == month && d == day {
		return candidate
	}

	// Midnight was skipped: the day really begins at the transition ending the
	// zone period the normalized candidate fell back into.
	if _, transition := candidate.ZoneBounds(); !transition.IsZero() {
		if y, m, d := transition.Date(); y == year && m == month && d == day {
			return transition
		}
	}

	// No instant on the requested day exists (a zone that skips a whole
	// calendar day, e.g. Pacific/Apia 2011-12-30). Keep time.Date's own answer:
	// the callers reached from here (CalendarDay, DateAtLocation) rebuild stored
	// values and have no error channel. The returned value therefore carries a
	// DIFFERENT calendar date than the one requested, which is exactly how
	// ParseDayDate detects the case and refuses the input instead.
	return candidate
}

// CalendarDay rebuilds a date-only stored value at the start of its calendar
// day in `location` (StartOfCalendarDay), preserving the calendar components
// of `value` exactly as stored. Use this for time.Time values whose semantics
// is "a calendar date" rather than "an instant in time" — DailyLog.Date,
// User.LastPeriodStart, derived stats fields. Unlike DateAtLocation, this
// does not apply In(location) and therefore does not shift the calendar day
// across timezones, which matters when stored values were persisted with a
// UTC-midnight timestamp.
func CalendarDay(value time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	if value.IsZero() {
		return time.Time{}
	}
	year, month, day := value.Date()
	return StartOfCalendarDay(year, month, day, location)
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

// FilterLogsByDateRange narrows an already-fetched, unbounded log slice down
// to the same [from, to] window that DayService.FetchLogsForUser would query
// for, so a caller holding a superset of logs in memory can derive the
// narrower range without a second daily_logs round-trip. Bounds and inclusion
// match FetchLogsForUser exactly (half-open [fromStart, toEnd) via DayRange).
func FilterLogsByDateRange(logs []models.DailyLog, from time.Time, to time.Time, location *time.Location) []models.DailyLog {
	fromStart, _ := DayRange(from, location)
	_, toEnd := DayRange(to, location)
	filtered := make([]models.DailyLog, 0, len(logs))
	for _, entry := range logs {
		if entry.Date.Before(fromStart) || !entry.Date.Before(toEnd) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// CalendarDaysBetween returns the signed number of calendar days from `from`
// to `to`, comparing only the calendar components of the two values. Each
// operand is re-anchored to UTC-midnight of its own calendar day before
// subtracting, so the result is a pure calendar-day difference immune to the
// operands carrying different midnight shapes (location-midnight working
// values vs UTC-midnight stored values, issue #48 class) and to DST
// transitions between the two days.
func CalendarDaysBetween(from time.Time, to time.Time) int {
	start := dateOnly(from)
	end := dateOnly(to)
	return int(end.Sub(start).Hours() / 24)
}

// AddCalendarDays returns the calendar day `days` after `day` (negative steps
// backward), start-of-day in `location`. It is the counterpart of
// CalendarDaysBetween — that one MEASURES a calendar-day distance, this one
// WALKS one — and the single stepping point for code that is not already
// walking a run of days through forEachCalendarDay.
//
// `day.AddDate(0, 0, n)` is not that. AddDate re-enters time.Date in the
// receiver's own location, so where the resulting day's local midnight does not
// exist (America/Santiago 2026-09-06, America/Havana 2026-03-08) it resolves
// the missing wall clock BACKWARD and returns the PREVIOUS calendar day at
// 23:00. Every reader that then takes the calendar components — CalendarDayKey,
// a map key, a rendered date — is handed the wrong day, one day a year per
// affected zone, silently.
//
// The step therefore runs over UTC-anchored days, where no midnight is ever
// skipped, and the result is resolved back into `location` through the single
// construction point. Keeping the caller's anchor is deliberate: these values
// are routinely ordered against other location-midnight values, and returning a
// UTC-midnight one would trade this defect for the cross-anchor comparison
// defect (issue #48 class) the barrier's shape (b) exists to catch.
func AddCalendarDays(day time.Time, days int, location *time.Location) time.Time {
	if day.IsZero() {
		return time.Time{}
	}
	stepped := dateOnly(day).AddDate(0, 0, days)
	return CalendarDay(stepped, location)
}

// uniqueSymptomIDs returns a day's symptom ids in their stored order with
// repeats removed. Every surface that COUNTS symptoms per day walks the slice
// through this (or through uniqueKnownSymptomIDs, which filters unknown ids on
// top of it): a repeated id would count its day twice, which renders a phase
// percentage above 100 and skews both the frequency list and the picker's usage
// ranking. ValidateSymptomIDs already deduplicates before persisting, so this is
// the second of two barriers rather than the only one — the read side does not
// depend on the write side having been the one that ran.
func uniqueSymptomIDs(symptomIDs []uint) []uint {
	if len(symptomIDs) == 0 {
		return nil
	}

	unique := make([]uint, 0, len(symptomIDs))
	seen := make(map[uint]struct{}, len(symptomIDs))
	for _, symptomID := range symptomIDs {
		if _, duplicate := seen[symptomID]; duplicate {
			continue
		}
		seen[symptomID] = struct{}{}
		unique = append(unique, symptomID)
	}
	return unique
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
	if entry.BBT != nil && IsValidDayBBT(entry.BBT) {
		return true
	}
	if NormalizeDayCervicalMucus(entry.CervicalMucus) != models.CervicalMucusNone {
		return true
	}
	if NormalizeDayPregnancyTest(entry.PregnancyTest) != models.PregnancyTestNone {
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

// IsAutoFilledPeriodCandidate reports whether a day log carries no manual
// signal besides the IsPeriod flag (and the Flow value that
// AutoFillFollowingPeriodDays propagates from the anchor), so toggling the
// anchor day off can safely clear it. Days touched manually (mood, intimacy,
// BBT, mucus, cycle factors, symptoms, notes), days marked as a cycle anchor,
// and uncertain anchors are kept intact. The Flow field is excluded from the
// check because web's auto-fill replays the anchor's flow into the neighbors;
// a neighbor whose only manual change was a flow override therefore falls
// inside the auto-fill window for clearing purposes. This is the parity
// counterpart of `isAutoFilledPeriodCandidate` in ovumcy-app, where auto-fill
// does not propagate flow.
//
// The manual-signal fields it tests mirror DayHasData's, and
// day_field_mirror_barrier_test.go holds the two together: a field wired into
// one of them alone fails there, naming the field and which side is missing.
// Flow, CycleStart and IsUncertain are the only asymmetry, each carried in that
// barrier's exception sets with the reason above.
func IsAutoFilledPeriodCandidate(entry models.DailyLog) bool {
	if !entry.IsPeriod || entry.CycleStart || entry.IsUncertain {
		return false
	}
	if entry.Mood >= MinDayMood && entry.Mood <= MaxDayMood {
		return false
	}
	if NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone {
		return false
	}
	if entry.BBT != nil && IsValidDayBBT(entry.BBT) {
		return false
	}
	if NormalizeDayCervicalMucus(entry.CervicalMucus) != models.CervicalMucusNone {
		return false
	}
	if NormalizeDayPregnancyTest(entry.PregnancyTest) != models.PregnancyTestNone {
		return false
	}
	if len(DayCycleFactorKeySet(entry.CycleFactorKeys)) > 0 {
		return false
	}
	if len(entry.SymptomIDs) > 0 {
		return false
	}
	return strings.TrimSpace(entry.Notes) == ""
}
