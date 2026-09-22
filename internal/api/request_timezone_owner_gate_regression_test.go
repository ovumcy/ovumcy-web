package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// WEB-35: X-Ovumcy-Timezone (and its ovumcy_tz cookie fallback) used to
// resolve in LanguageMiddleware for every request, authenticated or not — a
// caller sending a distinct zone name on every request bought one
// time.LoadLocation (a zoneinfo read and parse) per request, on every route
// without a rate limit. Resolution now runs only from
// resolveOwnerRequestTimezone, called by AuthRequired after a session has
// actually verified (middleware_auth_required.go), so these regressions pin
// the gate itself: an unauthenticated caller — including one presenting a
// session cookie that fails to verify — gets no zoneinfo work done on its
// behalf, while a verified owner still gets the header honoured exactly as
// before.
//
// The observable proxy for "resolution ran" is the ovumcy_tz Set-Cookie:
// resolveOwnerRequestTimezone only ever runs together with the cookie-refresh
// guard covered by TestResolveOwnerRequestTimezonePersistsCanonicalTimezoneCookie,
// so its absence means the header was never parsed at all.

// TestUnauthenticatedRequestNeverResolvesTheTimezoneHeader drives a protected
// page (GET /settings, which carries AuthRequired) with no auth cookie at
// all. Before WEB-35 this still cost a zoneinfo load and a Set-Cookie; now
// AuthRequired refuses before resolveOwnerRequestTimezone ever runs.
func TestUnauthenticatedRequestNeverResolvesTheTimezoneHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "tz-gate-no-cookie@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set(timezoneHeaderName, "Europe/Belgrade")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unauthenticated settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		t.Fatalf("expected the unauthenticated request to be refused, got 200")
	}
	if cookie := responseCookie(response.Cookies(), timezoneCookieName); cookie != nil {
		t.Fatalf("expected no ovumcy_tz cookie for an unauthenticated request, got %q", cookie.Value)
	}
}

// TestForgedSessionCookieNeverResolvesTheTimezoneHeader is the sharper case:
// a caller that presents a session cookie which FAILS to verify must not be
// able to buy the zoneinfo cost by pretending to be an owner. Gating on the
// verified session (not merely "a cookie named ovumcy_auth is present")
// closes exactly this path.
func TestForgedSessionCookieNeverResolvesTheTimezoneHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "tz-gate-forged-cookie@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set(timezoneHeaderName, "Europe/Belgrade")
	request.Header.Set("Cookie", authCookieName+"=v1.not-a-real-sealed-token")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("forged-cookie settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		t.Fatalf("expected the forged-session request to be refused, got 200")
	}
	if cookie := responseCookie(response.Cookies(), timezoneCookieName); cookie != nil {
		t.Fatalf("expected no ovumcy_tz cookie for a forged session cookie, got %q", cookie.Value)
	}
}

// TestAuthenticatedRequestStillResolvesTheTimezoneHeader is the positive
// control: a genuinely verified owner still gets the header honoured and the
// cookie refreshed, exactly as before WEB-35 — the gate must not have turned
// into a silent refusal of the feature for real owners.
func TestAuthenticatedRequestStillResolvesTheTimezoneHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "tz-gate-authenticated@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set(timezoneHeaderName, "Europe/Belgrade")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("authenticated settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	cookie := responseCookie(response.Cookies(), timezoneCookieName)
	if cookie == nil || cookie.Value != "Europe/Belgrade" {
		t.Fatalf("expected ovumcy_tz=Europe/Belgrade for a verified owner, got %#v", cookie)
	}
}

// TestAnonymousPublicPageNeverResolvesTheTimezoneHeader covers the other
// named surface: a public page outside AuthRequired entirely (GET /login)
// must not resolve the header either, whether or not a session cookie is
// present.
func TestAnonymousPublicPageNeverResolvesTheTimezoneHeader(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "tz-gate-public-page@example.com")

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set(timezoneHeaderName, "Europe/Belgrade")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("anonymous login page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if cookie := responseCookie(response.Cookies(), timezoneCookieName); cookie != nil {
		t.Fatalf("expected no ovumcy_tz cookie on the anonymous login page, got %q", cookie.Value)
	}
}
