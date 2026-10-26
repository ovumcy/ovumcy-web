package services

import (
	"path"
	"strings"
)

func IsOnboardingPath(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "/onboarding" {
		return true
	}
	return strings.HasPrefix(cleanPath, "/api/v1/onboarding/")
}

// ShouldEnforceOnboardingAccess decides whether the onboarding gate refuses a
// request from an owner who has not finished onboarding. It decides on
// routedPath — the path the router dispatches on — and never on the wire path
// alone: a router that removes dot segments or decodes escapes before matching
// turns "/api/v1/onboarding/../days/…" into a days route, and the exemption
// prefix read off the wire string would wave that request past the gate.
//
// A request whose routed path differs from the path it arrived with, or that
// path.Clean would still change, is refused outright rather than exempted.
// path.Clean removes every "." and ".." segment and every empty segment, so a
// path carrying one never equals its cleaned form. No page or form of this app
// produces such a path, so a request carrying one is not a legitimate
// onboarding request whatever it resolves to, and refusing it keeps the
// decision independent of how much normalization a given router release
// performs. A relative or empty path is never exempt either: neither carries
// an exempt prefix.
//
// Both sign-out routes stay reachable: an owner must be able to end a session
// on a shared device before the first cycle is entered, and a sign-out
// redirected to /onboarding left the session alive with no control to end it.
func ShouldEnforceOnboardingAccess(wirePath string, routedPath string) bool {
	routed := strings.TrimSpace(routedPath)
	if strings.TrimSpace(wirePath) != routed || path.Clean(routed) != routed {
		return true
	}
	if routed == "/api/v1/sessions/current" || routed == "/logout" {
		return false
	}
	return !IsOnboardingPath(routed)
}
