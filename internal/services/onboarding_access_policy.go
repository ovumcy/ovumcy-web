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
// A request whose routed path is not already canonical, or whose routed path
// differs from the path it arrived with, is refused outright rather than
// exempted. No page or form of this app produces such a path, so a request
// carrying one is not a legitimate onboarding request whatever it resolves to,
// and refusing it keeps the decision independent of how much normalization a
// given router release performs.
func ShouldEnforceOnboardingAccess(wirePath string, routedPath string) bool {
	routed := strings.TrimSpace(routedPath)
	if strings.TrimSpace(wirePath) != routed || !isCanonicalRequestPath(routed) {
		return true
	}
	if routed == "/api/v1/sessions/current" {
		return false
	}
	return !IsOnboardingPath(routed)
}

// isCanonicalRequestPath reports whether requestPath is an absolute path with
// no ".." segment that path.Clean leaves unchanged: no "." segment, no empty
// segment, no trailing slash.
func isCanonicalRequestPath(requestPath string) bool {
	if !strings.HasPrefix(requestPath, "/") {
		return false
	}
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == ".." {
			return false
		}
	}
	return path.Clean(requestPath) == requestPath
}
