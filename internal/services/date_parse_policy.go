package services

import (
	"errors"
	"strings"
	"time"
)

var ErrDayDateRequired = errors.New("date is required")

func ParseDayDate(raw string, location *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, ErrDayDateRequired
	}

	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, err
	}

	return DateAtLocation(parsed, location), nil
}
