package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// CSRF protection on state-mutating endpoints now accepts the token via the
// `X-CSRF-Token` request header in addition to the legacy `csrf_token` form
// field. The header path exists for canonical-REST programmatic clients
// (curl, language SDKs, future wrappers) that do not POST form-encoded
// bodies; the form field path remains the default for HTMX browser
// submissions.
//
// Sister tests in `state_mutation_csrf_regression_test.go` cover the
// "missing token" rejection branch with the form-field surface; these
// tests cover the "header path is honored" branch.

func TestStateMutationAcceptsCSRFTokenViaXCSRFTokenHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "csrf-header@example.com")

	form := url.Values{
		"display_name": {"Header Persona"},
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	request.Header.Set("Accept", "application/json")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings profile request via header CSRF failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusForbidden {
		t.Fatalf("expected CSRF middleware to accept X-CSRF-Token header, got 403")
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings profile update via header to succeed (200), got %d", response.StatusCode)
	}
}

func TestStateMutationRejectsInvalidCSRFTokenInHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "csrf-header-invalid@example.com")

	form := url.Values{
		"display_name": {"Invalid Header Token"},
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	request.Header.Set("X-CSRF-Token", "definitely-not-the-real-token")
	request.Header.Set("Accept", "application/json")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings profile request with bogus header token failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected CSRF middleware to reject bogus X-CSRF-Token header with 403, got %d", response.StatusCode)
	}
}

func TestStateMutationPrefersFormCSRFFieldOverHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "csrf-prefer-form@example.com")

	// HTMX traffic dominates browser submissions; the extractor must keep
	// honoring the form field path even when the header is also present
	// (e.g. a stray header on a browser request). This test pins that
	// precedence: form field wins, header is ignored, request succeeds.
	form := url.Values{
		"display_name": {"Form Wins"},
		"csrf_token":   {ctx.csrfToken},
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	request.Header.Set("X-CSRF-Token", "stale-or-bogus")
	request.Header.Set("Accept", "application/json")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings profile request with form + header failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected form CSRF field to take precedence and succeed (200), got %d", response.StatusCode)
	}
}
