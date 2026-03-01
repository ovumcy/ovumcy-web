package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func dayHasData(entry models.DailyLog) bool {
	return services.DayHasData(entry)
}

func sameCalendarDay(a time.Time, b time.Time) bool {
	return services.SameCalendarDay(a, b)
}

func dateAtLocation(value time.Time, location *time.Location) time.Time {
	return services.DateAtLocation(value, location)
}

func symptomIDSet(ids []uint) map[uint]bool {
	return services.SymptomIDSet(ids)
}
