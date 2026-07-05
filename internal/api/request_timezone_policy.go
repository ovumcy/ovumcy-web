package api

import (
	"net/url"
	"strings"
	"time"
)

const maxRequestTimezoneLength = 128

func resolveRequestLocation(headerValue string, cookieValue string, fallback *time.Location) (*time.Location, string) {
	if fallback == nil {
		fallback = time.UTC
	}

	if location, canonical, ok := parseRequestTimezone(headerValue); ok {
		return location, canonical
	}
	if location, _, ok := parseRequestTimezone(cookieValue); ok {
		return location, ""
	}
	return fallback, ""
}

func parseRequestTimezone(raw string) (*time.Location, string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > maxRequestTimezoneLength {
		return nil, "", false
	}
	if strings.Contains(value, "%") {
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return nil, "", false
		}
		value = strings.TrimSpace(decoded)
	}
	if !isSafeTimezoneIdentifier(value) {
		return nil, "", false
	}
	// Reject the "Local" special token by input. time.LoadLocation("Local")
	// returns the server's own zone (time.Local); the canonical check below only
	// catches it when time.Local.String() == "Local", which holds when TZ is
	// unset (Windows dev, default CI runner) but NOT when an operator sets TZ —
	// common on self-hosted Linux Docker — where the loaded zone stringifies to
	// its real name and slips through, letting a client pin requests to the
	// server's timezone. Guarding the input keeps rejection deterministic across
	// platforms and TZ configs.
	if strings.EqualFold(value, "Local") {
		return nil, "", false
	}

	location, err := time.LoadLocation(value)
	if err != nil {
		return nil, "", false
	}
	canonical := strings.TrimSpace(location.String())
	if canonical == "" || strings.EqualFold(canonical, "Local") {
		return nil, "", false
	}

	return location, canonical, true
}

func isSafeTimezoneIdentifier(value string) bool {
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			continue
		case ch >= 'A' && ch <= 'Z':
			continue
		case ch >= '0' && ch <= '9':
			continue
		case ch == '/', ch == '_', ch == '+', ch == '-':
			continue
		default:
			return false
		}
	}
	return true
}
