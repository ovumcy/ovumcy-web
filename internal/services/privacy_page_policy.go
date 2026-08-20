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
	// second surface that echoes a caller-supplied query into the markup and
	// into an outgoing URL. It carries the same allowlist as the rendered
	// current path: SanitizeRedirectPath decides whether the path may be
	// navigated to, SanitizeCurrentPathQuery decides what of its query may be
	// shown.
	backPath := SanitizeCurrentPathQuery(SanitizeRedirectPath(backQuery, backFallback))
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
