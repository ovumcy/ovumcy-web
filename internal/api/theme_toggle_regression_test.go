package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// TestRenderedPagesAvoidInlineScriptsUnderStrictCSP pins the strict-CSP contract
// across pages that share the base layout: every <script> must load from an
// external src (no inline scripts), so the app needs no 'unsafe-inline' in
// script-src. Covers an unauthenticated page (login) plus two authenticated
// ones (dashboard, settings) so a page-specific inline script would be caught.
func TestRenderedPagesAvoidInlineScriptsUnderStrictCSP(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "csp-inline-scripts@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	pages := []struct {
		name   string
		path   string
		cookie string
	}{
		{name: "login", path: "/login"},
		{name: "dashboard", path: "/dashboard", cookie: authCookie},
		{name: "settings", path: "/settings", cookie: authCookie},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, page.path, nil)
			request.Header.Set("Accept-Language", "en")
			if page.cookie != "" {
				request.Header.Set("Cookie", page.cookie)
			}

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("%s request failed: %v", page.path, err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200 for %s, got %d", page.path, response.StatusCode)
			}

			assertNoInlineScriptTags(t, page.path, mustReadBodyString(t, response.Body))
		})
	}
}

func assertNoInlineScriptTags(t *testing.T, path string, rendered string) {
	t.Helper()
	scriptTagPattern := regexp.MustCompile(`(?is)<script\b[^>]*>`)
	for _, tag := range scriptTagPattern.FindAllString(rendered, -1) {
		if regexp.MustCompile(`(?is)\bsrc\s*=`).MatchString(tag) {
			continue
		}
		t.Fatalf("expected %s to avoid inline script tags under strict CSP, found %q", path, tag)
	}
}
