package services

import (
	"net/url"
	"strings"
)

func SanitizeRedirectPath(raw string, fallback string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return fallback
	}
	if !strings.HasPrefix(candidate, "/") {
		return fallback
	}
	if len(candidate) > 1 && (candidate[1] == '/' || candidate[1] == '\\') {
		return fallback
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.IsAbs() {
		return fallback
	}
	return candidate
}
