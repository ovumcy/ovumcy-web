package services

import "testing"

func TestResolvePrivacyMetaDescriptionFallback(t *testing.T) {
	t.Parallel()

	if got := ResolvePrivacyMetaDescription(""); got != defaultPrivacyMetaDescription {
		t.Fatalf("unexpected fallback description: %q", got)
	}
	if got := ResolvePrivacyMetaDescription("meta.description.privacy"); got != defaultPrivacyMetaDescription {
		t.Fatalf("expected key fallback description, got %q", got)
	}
}

func TestBuildPrivacyBackNavigationGuestUsesLoginFallback(t *testing.T) {
	t.Parallel()

	navigation := BuildPrivacyBackNavigation("https://evil.example/path", false)
	if navigation.BackPath != "/login" {
		t.Fatalf("expected guest back path /login, got %q", navigation.BackPath)
	}
	if navigation.BreadcrumbBackLabelKey != "common.home" {
		t.Fatalf("expected guest breadcrumb key common.home, got %q", navigation.BreadcrumbBackLabelKey)
	}
}

func TestBuildPrivacyBackNavigationAuthenticatedUsesDashboardFallback(t *testing.T) {
	t.Parallel()

	navigation := BuildPrivacyBackNavigation("https://evil.example/path", true)
	if navigation.BackPath != "/dashboard" {
		t.Fatalf("expected auth back path /dashboard, got %q", navigation.BackPath)
	}
	if navigation.BreadcrumbBackLabelKey != "nav.dashboard" {
		t.Fatalf("expected auth breadcrumb key nav.dashboard, got %q", navigation.BreadcrumbBackLabelKey)
	}
}

func TestBuildPrivacyBackNavigationUsesCalendarBackLabelWhenRequested(t *testing.T) {
	t.Parallel()

	navigation := BuildPrivacyBackNavigation("/calendar?month=2026-03", true)
	if navigation.BackPath != "/calendar?month=2026-03" {
		t.Fatalf("expected sanitized calendar back path, got %q", navigation.BackPath)
	}
	if navigation.BreadcrumbBackLabelKey != "nav.calendar" {
		t.Fatalf("expected nav.calendar breadcrumb key, got %q", navigation.BreadcrumbBackLabelKey)
	}
}

// TestBuildPrivacyBackNavigationFiltersTheQueryOfTheBackPath pins the second
// surface that echoes a caller-supplied query into the markup: the privacy
// page's own back link. SanitizeRedirectPath accepts any local path, so without
// the query allowlist a crafted /privacy?back=… link put the address it carried
// into the rendered href — and into the next navigation the visitor makes.
func TestBuildPrivacyBackNavigationFiltersTheQueryOfTheBackPath(t *testing.T) {
	t.Parallel()

	navigation := BuildPrivacyBackNavigation("/login?email=victim@example.com&error=invalid", false)
	if navigation.BackPath != "/login" {
		t.Fatalf("expected the unlisted query parameters dropped from the back path, got %q", navigation.BackPath)
	}
	if navigation.BreadcrumbBackLabelKey != "common.home" {
		t.Fatalf("expected common.home breadcrumb key, got %q", navigation.BreadcrumbBackLabelKey)
	}
}

// TestBuildPrivacyBackNavigationAppliesTheWholeBackValuePolicy pins that this
// surface gets the *same* decision as the `back` parameter of a rendered current
// path, not a weaker one. It receives the value straight from the query string
// rather than through the current-path filter, so every carrier has to be
// refused here independently: a hostile value under an allowlisted key, a
// fragment, an address in the path half itself, and a non-local target.
func TestBuildPrivacyBackNavigationAppliesTheWholeBackValuePolicy(t *testing.T) {
	t.Parallel()

	const fallback = "/dashboard"

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "hostile value under an allowlisted key", raw: "/calendar?month=victim@example.com", want: "/calendar"},
		{name: "fragment", raw: "/login#email=victim@example.com", want: "/login"},
		{name: "address in the path half", raw: "/victim@example.com", want: fallback},
		{name: "address as the whole value", raw: "victim@example.com", want: fallback},
		{name: "absolute url", raw: "https://evil.example/path", want: fallback},
		{name: "protocol relative", raw: "//evil.example", want: fallback},
		{name: "empty falls back", raw: "", want: fallback},
		// Positive controls: real navigation must still work.
		{name: "a real month survives", raw: "/calendar?month=2026-08", want: "/calendar?month=2026-08"},
		{name: "a plain path survives", raw: "/settings", want: "/settings"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := BuildPrivacyBackNavigation(testCase.raw, true).BackPath; got != testCase.want {
				t.Fatalf("BuildPrivacyBackNavigation(%q).BackPath = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}
