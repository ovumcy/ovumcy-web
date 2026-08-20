package services

import "strings"

const defaultPrivacyMetaDescription = "Ovumcy Privacy Policy - Zero data collection, self-hosted period tracker."

type PrivacyBackNavigation struct {
	BackPath               string
	BreadcrumbBackLabelKey string
}

func ResolvePrivacyMetaDescription(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || value == "meta.description.privacy" {
		return defaultPrivacyMetaDescription
	}
	return value
}

func BuildPrivacyBackNavigation(backQuery string, isAuthenticated bool) PrivacyBackNavigation {
	backFallback := "/login"
	breadcrumbBackLabelKey := "common.home"
	if isAuthenticated {
		backFallback = "/dashboard"
		breadcrumbBackLabelKey = "nav.dashboard"
	}

	// The back path is rendered into the page's own back link, so it is the
	// second surface that echoes a caller-supplied value into the markup and
	// into an outgoing URL. It runs through the same single decision the `back`
	// parameter of a rendered current path does — fragment stripped, own query
	// filtered by the allowlist, and required to be a redirect-safe path built
	// from the characters this app's routes use — falling back when it is not.
	backPath := backFallback
	if sanitized, ok := SanitizeBackNavigationValue(backQuery); ok {
		backPath = sanitized
	}
	if labelKey := privacyBackLabelKeyForPath(backPath, isAuthenticated); labelKey != "" {
		breadcrumbBackLabelKey = labelKey
	}

	return PrivacyBackNavigation{
		BackPath:               backPath,
		BreadcrumbBackLabelKey: breadcrumbBackLabelKey,
	}
}

func privacyBackLabelKeyForPath(backPath string, isAuthenticated bool) string {
	switch {
	case strings.HasPrefix(backPath, "/calendar"):
		return "nav.calendar"
	case strings.HasPrefix(backPath, "/stats"):
		return "nav.insights"
	case strings.HasPrefix(backPath, "/settings"):
		return "nav.settings"
	case strings.HasPrefix(backPath, "/dashboard"):
		return "nav.dashboard"
	case strings.HasPrefix(backPath, "/login"), strings.HasPrefix(backPath, "/register"):
		return "common.home"
	case strings.HasPrefix(backPath, "/"):
		if isAuthenticated {
			return "nav.dashboard"
		}
		return "common.home"
	default:
		return ""
	}
}
