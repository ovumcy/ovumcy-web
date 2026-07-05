package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func reloadUserTimezoneForTest(t *testing.T, ctx settingsSecurityTestContext, userID uint) string {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded.Timezone
}

func TestSettingsTimezoneEndpointPersistsValidTimezone(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-valid@example.com")

	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "" {
		t.Fatalf("expected empty timezone before the request, got %q", got)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"Europe/Belgrade"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected persisted timezone Europe/Belgrade, got %q", got)
	}
}

// TestSettingsTimezoneEndpointRejectsUnsafeTimezone drives the validator gate on
// the endpoint: the "Local" token is rejected with 400 and never persisted.
func TestSettingsTimezoneEndpointRejectsUnsafeTimezone(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-unsafe@example.com")

	for _, unsafe := range []string{"Local", "Europe/Belgrade\nInjected", "not a zone"} {
		response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
			"timezone": {unsafe},
		}, map[string]string{"Accept": "application/json"})
		assertStatusCode(t, response, http.StatusBadRequest)
		_ = response.Body.Close()

		if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "" {
			t.Fatalf("expected unsafe timezone %q to never persist, got %q", unsafe, got)
		}
	}
}

// TestSettingsTimezoneEndpointNoopWhenUnchanged asserts a repeat POST of the
// already-persisted value still succeeds and leaves the value intact (the
// no-DB-write path is unit-tested at the service layer).
func TestSettingsTimezoneEndpointNoopWhenUnchanged(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-noop@example.com")

	first := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"America/Toronto"},
	}, map[string]string{"Accept": "application/json"})
	assertStatusCode(t, first, http.StatusOK)
	_ = first.Body.Close()

	second := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"America/Toronto"},
	}, map[string]string{"Accept": "application/json"})
	assertStatusCode(t, second, http.StatusOK)
	_ = second.Body.Close()

	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "America/Toronto" {
		t.Fatalf("expected timezone to remain America/Toronto, got %q", got)
	}
}

// TestSettingsTimezoneEndpointScopedToSessionOwner proves a second owner's
// request writes only its own row: the target is the session user_id, never a
// request-supplied id, so owner B cannot rewrite owner A's timezone.
func TestSettingsTimezoneEndpointScopedToSessionOwner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-owner-a@example.com")

	// Seed owner A on the same app + DB.
	responseA := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"Europe/Belgrade"},
	}, map[string]string{"Accept": "application/json"})
	assertStatusCode(t, responseA, http.StatusOK)
	_ = responseA.Body.Close()

	// A second owner, authenticated on the same app, posts a different zone.
	ownerB := createOnboardingTestUser(t, ctx.database, "timezone-owner-b@example.com", "StrongPass1", true)
	authCookieB := issueAuthCookieForUser(t, ownerB)
	csrfCookieB, csrfTokenB := loadSettingsCSRFContext(t, ctx.app, authCookieB)
	ctxB := settingsSecurityTestContext{
		app:        ctx.app,
		database:   ctx.database,
		user:       ownerB,
		authCookie: authCookieB,
		csrfCookie: csrfCookieB,
		csrfToken:  csrfTokenB,
	}

	responseB := settingsFormRequestWithCSRF(t, ctxB, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"Asia/Tokyo"},
	}, map[string]string{"Accept": "application/json"})
	assertStatusCode(t, responseB, http.StatusOK)
	_ = responseB.Body.Close()

	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected owner A timezone unchanged at Europe/Belgrade, got %q", got)
	}
	if got := reloadUserTimezoneForTest(t, ctx, ownerB.ID); got != "Asia/Tokyo" {
		t.Fatalf("expected owner B timezone Asia/Tokyo, got %q", got)
	}
}

// TestSettingsTimezoneEndpointRejectsUnauthenticated confirms an unauthenticated
// caller (no auth cookie) with a valid CSRF token is refused and persists
// nothing — AuthRequired gates the endpoint before the handler runs.
func TestSettingsTimezoneEndpointRejectsUnauthenticated(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-unauth@example.com")

	// Carry only the csrf cookie + token (no auth cookie).
	form := url.Values{"timezone": {"Europe/Belgrade"}, "csrf_token": {ctx.csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/timezone", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", cookiePair(ctx.csrfCookie))
	request.Header.Set("Accept", "application/json")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unauthenticated timezone post failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 401/403 for unauthenticated timezone post, got %d", response.StatusCode)
	}
	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "" {
		t.Fatalf("expected no timezone persisted for unauthenticated caller, got %q", got)
	}
}

// timezoneJSONRequestWithCSRF posts a raw JSON body carrying the auth + csrf
// cookies and the CSRF token in the X-CSRF-Token header (a JSON body has no
// csrf_token form field, so the header is the token source the extractor uses).
func timezoneJSONRequestWithCSRF(t *testing.T, ctx settingsSecurityTestContext, body string, headers map[string]string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/timezone", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("timezone json request failed: %v", err)
	}
	return response
}

// TestSettingsTimezoneEndpointAcceptsJSONBody covers the JSON body-binding path
// (Content-Type: application/json) end to end.
func TestSettingsTimezoneEndpointAcceptsJSONBody(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-json@example.com")

	response := timezoneJSONRequestWithCSRF(t, ctx, `{"timezone":"Europe/Belgrade"}`, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected JSON-body timezone Europe/Belgrade to persist, got %q", got)
	}
}

// TestSettingsTimezoneEndpointRejectsMalformedJSON covers the body-parse error
// branch: a Content-Type: application/json request with an unparseable body is
// rejected with 400 and persists nothing.
func TestSettingsTimezoneEndpointRejectsMalformedJSON(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-badjson@example.com")

	response := timezoneJSONRequestWithCSRF(t, ctx, `{"timezone":`, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusBadRequest)
	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "" {
		t.Fatalf("expected malformed JSON to persist nothing, got %q", got)
	}
}

// TestSettingsTimezoneEndpointHTMXReturnsNoContent covers the HTMX response
// branch: an HX-Request save returns 204 No Content and still persists.
func TestSettingsTimezoneEndpointHTMXReturnsNoContent(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "timezone-endpoint-htmx@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/timezone", url.Values{
		"timezone": {"America/Toronto"},
	}, map[string]string{"HX-Request": "true"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusNoContent)
	if got := reloadUserTimezoneForTest(t, ctx, ctx.user.ID); got != "America/Toronto" {
		t.Fatalf("expected HTMX timezone save to persist America/Toronto, got %q", got)
	}
}
