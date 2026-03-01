package api

import (
	"errors"
	"time"
)

func parseDayParam(raw string, location *time.Location) (time.Time, error) {
	if raw == "" {
		return time.Time{}, errors.New("date is required")
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		return time.Time{}, err
	}
	return dateAtLocation(parsed, location), nil
}
