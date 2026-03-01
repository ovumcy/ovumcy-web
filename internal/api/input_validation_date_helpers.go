package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func parseDayParam(raw string, location *time.Location) (time.Time, error) {
	return services.ParseDayDate(raw, location)
}
