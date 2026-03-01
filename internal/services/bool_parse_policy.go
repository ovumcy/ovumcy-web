package services

import "strings"

func ParseBoolLike(raw string) bool {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	return normalized == "1" || normalized == "true" || normalized == "on" || normalized == "yes"
}
