package services

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrExportFromDateInvalid = errors.New("export invalid from date")
	ErrExportToDateInvalid   = errors.New("export invalid to date")
	ErrExportRangeInvalid    = errors.New("export invalid range")
)

func ParseExportRange(rawFrom string, rawTo string, location *time.Location) (*time.Time, *time.Time, error) {
	fromRaw := strings.TrimSpace(rawFrom)
	toRaw := strings.TrimSpace(rawTo)

	var from *time.Time
	if fromRaw != "" {
		parsedFrom, err := ParseDayDate(fromRaw, location)
		if err != nil {
			return nil, nil, ErrExportFromDateInvalid
		}
		from = &parsedFrom
	}

	var to *time.Time
	if toRaw != "" {
		parsedTo, err := ParseDayDate(toRaw, location)
		if err != nil {
			return nil, nil, ErrExportToDateInvalid
		}
		to = &parsedTo
	}

	if from != nil && to != nil && to.Before(*from) {
		return nil, nil, ErrExportRangeInvalid
	}

	return from, to, nil
}
