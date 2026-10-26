package services

import "strings"

func IsOnboardingPath(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "/onboarding" {
		return true
	}
	return strings.HasPrefix(cleanPath, "/api/v1/onboarding/")
}

// ShouldEnforceOnboardingAccess reports whether a signed-in account that has
// not finished onboarding is held to the onboarding pages on this path. Both
// sign-out routes stay reachable: an owner must be able to end a session on a
// shared device before the first cycle is entered, and a sign-out redirected
// to /onboarding left the session alive with no control to end it.
func ShouldEnforceOnboardingAccess(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "/api/v1/sessions/current" || cleanPath == "/logout" {
		return false
	}
	return !IsOnboardingPath(cleanPath)
}
