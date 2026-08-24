package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		log.Printf("invalid %s=%q, using fallback %d", key, value, fallback)
		return fallback
	}
	return parsed
}

// getEnvIntInRange parses an integer env var and accepts it only within the
// inclusive [min, max] range, falling back otherwise. Unlike getEnvInt (which
// rejects anything below 1), it admits 0, which is required for
// REMINDER_SCHEDULER_HOUR where 0 is a valid midnight run hour.
func getEnvIntInRange(key string, fallback, minValue, maxValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minValue || parsed > maxValue {
		log.Printf("invalid %s=%q, using fallback %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Second {
		log.Printf("invalid %s=%q, using fallback %s", key, value, fallback)
		return fallback
	}
	return parsed
}

// parseBoolEnvValue holds the accepted vocabulary for every boolean env var. Both
// getEnvBool and getEnvBoolStrict route through it so the lenient and the
// refusing getter can never accept different spellings of the same value.
func parseBoolEnvValue(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, ok := parseBoolEnvValue(value)
	if !ok {
		log.Printf("invalid %s=%q, using fallback %t", key, value, fallback)
		return fallback
	}
	return parsed
}

// getEnvBoolStrict reads a boolean env var like getEnvBool but refuses an
// unparseable value instead of falling back to the default.
//
// It is used for the toggles whose fallback IS the insecure posture —
// COOKIE_SECURE, HSTS_ENABLED, TRUST_PROXY_ENABLED,
// WEBHOOK_BLOCK_PRIVATE_ADDRESSES. There a typo (COOKIE_SECURE=ture) used to
// start the process on the default, so what the instance ran with could not be
// answered from the operator's env file, only from the boot log. An unset
// value is still the documented default; only a value the operator wrote and
// this process cannot read stops the boot, naming the key and the value.
func getEnvBoolStrict(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, ok := parseBoolEnvValue(value)
	if !ok {
		return false, fmt.Errorf("invalid %s=%q: expected one of 1/true/yes/on or 0/false/no/off", key, value)
	}
	return parsed, nil
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
