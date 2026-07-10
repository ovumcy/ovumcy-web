package services

import (
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// weekdayHeaderKeysSundayFirst is the calendar weekday header in the Go
// time.Weekday order (Sunday=0..Saturday=6). WeekdayHeaderKeys rotates this base
// order to place the owner's chosen first day of the week in column one.
var weekdayHeaderKeysSundayFirst = [7]string{
	"calendar.weekday.sun",
	"calendar.weekday.mon",
	"calendar.weekday.tue",
	"calendar.weekday.wed",
	"calendar.weekday.thu",
	"calendar.weekday.fri",
	"calendar.weekday.sat",
}

// NormalizeWeekStart clamps a raw week-start value to a known option, falling
// back to the default (Sunday) for empty or unrecognized input. Normalize,
// never reject — mirrors NormalizeTemperatureUnit and the other preference
// normalizers so a malformed value can never break calendar rendering.
func NormalizeWeekStart(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case models.WeekStartMonday:
		return models.WeekStartMonday
	case models.WeekStartSunday:
		return models.WeekStartSunday
	default:
		return models.DefaultWeekStart
	}
}

// weekStartOffset returns how many days weekday sits after the week-start
// column, i.e. its zero-based column index in the calendar grid. Sunday-first
// uses the raw time.Weekday index; Monday-first rotates so Monday is column 0
// and Sunday is column 6.
func weekStartOffset(weekday time.Weekday, weekStart string) int {
	if NormalizeWeekStart(weekStart) == models.WeekStartMonday {
		return (int(weekday) + 6) % 7
	}
	return int(weekday)
}

// WeekdayHeaderKeys returns the seven calendar weekday i18n keys in display
// order for the given week-start preference, so the header row is data-driven
// rather than hardcoded in the template.
func WeekdayHeaderKeys(weekStart string) []string {
	// Rotation amount equals the index of the first-day column within the
	// Sunday-first base array: 0 for Sunday, 1 for Monday.
	shift := 0
	if NormalizeWeekStart(weekStart) == models.WeekStartMonday {
		shift = 1
	}
	keys := make([]string, 0, 7)
	for i := range 7 {
		keys = append(keys, weekdayHeaderKeysSundayFirst[(i+shift)%7])
	}
	return keys
}
