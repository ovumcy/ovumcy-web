package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func resolveCalendarMonthAndSelectedDate(monthQueryRaw string, selectedDayRaw string, now time.Time, location *time.Location) (time.Time, string, error) {
	return services.ResolveCalendarMonthAndSelectedDate(monthQueryRaw, selectedDayRaw, now, location)
}

func calendarLogRange(monthStart time.Time) (time.Time, time.Time) {
	return services.CalendarLogRange(monthStart)
}

func calendarAdjacentMonthValues(monthStart time.Time) (string, string) {
	return services.CalendarAdjacentMonthValues(monthStart)
}
