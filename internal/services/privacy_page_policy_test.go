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

// TestBuildPrivacyBackNavigationLabelsEveryBackDestination table-drives the
// breadcrumb label over all six arms of the mapping. Three of them — insights,
// settings, and the generic "/" arm that splits on the session — were reached
// by no test, so they could be deleted or reordered and the page would send an
// owner back to their insights under a "Home" breadcrumb. The generic arm is
// driven from both sides of isAuthenticated, since that is the only thing that
// distinguishes its two answers.
func TestBuildPrivacyBackNavigationLabelsEveryBackDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		back            string
		isAuthenticated bool
		wantPath        string
		wantLabelKey    string
	}{
		{name: "calendar", back: "/calendar", isAuthenticated: true, wantPath: "/calendar", wantLabelKey: "nav.calendar"},
		{name: "stats", back: "/stats", isAuthenticated: true, wantPath: "/stats", wantLabelKey: "nav.insights"},
		{name: "settings", back: "/settings", isAuthenticated: true, wantPath: "/settings", wantLabelKey: "nav.settings"},
		{name: "dashboard", back: "/dashboard", isAuthenticated: true, wantPath: "/dashboard", wantLabelKey: "nav.dashboard"},
		{name: "login", back: "/login", isAuthenticated: false, wantPath: "/login", wantLabelKey: "common.home"},
		{name: "register", back: "/register", isAuthenticated: false, wantPath: "/register", wantLabelKey: "common.home"},
		// Any other local path falls to the generic arm, which answers by
		// session state rather than by destination.
		{name: "other local path signed in", back: "/help", isAuthenticated: true, wantPath: "/help", wantLabelKey: "nav.dashboard"},
		{name: "other local path signed out", back: "/help", isAuthenticated: false, wantPath: "/help", wantLabelKey: "common.home"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			navigation := BuildPrivacyBackNavigation(testCase.back, testCase.isAuthenticated)
			if navigation.BackPath != testCase.wantPath {
				t.Fatalf("BuildPrivacyBackNavigation(%q, %v).BackPath = %q, want %q", testCase.back, testCase.isAuthenticated, navigation.BackPath, testCase.wantPath)
			}
			if navigation.BreadcrumbBackLabelKey != testCase.wantLabelKey {
				t.Fatalf("BuildPrivacyBackNavigation(%q, %v).BreadcrumbBackLabelKey = %q, want %q", testCase.back, testCase.isAuthenticated, navigation.BreadcrumbBackLabelKey, testCase.wantLabelKey)
			}
		})
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
