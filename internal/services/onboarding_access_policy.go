package services

import "strings"

func IsOnboardingPath(path string) bool {
	cleanPath := strings.TrimSpace(path)
	return cleanPath == "/onboarding" || strings.HasPrefix(cleanPath, "/onboarding/")
}

func ShouldEnforceOnboardingAccess(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "/api/v1/sessions/current" {
		return false
	}
	return !IsOnboardingPath(cleanPath)
}
