package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// clearDataDashboardProbe drives a real authenticated page request carrying
// exactly the cookie header it is handed. Every session claim in this file is
// settled by sending the cookie somewhere it has to be honoured — a cookie that
// is merely present in a response says nothing about whether it still opens a
// door.
func clearDataDashboardProbe(t *testing.T, ctx settingsSecurityTestContext, cookieHeader string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookieHeader)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard probe (%s) failed: %v", cookieHeader, err)
	}
	return response
}

// assertAuthCookieOpensDashboard requires the session to still be live: 200 on
// /dashboard, not "some status other than the refusal I had in mind".
func assertAuthCookieOpensDashboard(t *testing.T, ctx settingsSecurityTestContext, cookieHeader string, label string) {
	t.Helper()

	response := clearDataDashboardProbe(t, ctx, cookieHeader)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: /dashboard status = %d, want 200 (session must be accepted)", label, response.StatusCode)
	}
}

// assertAuthCookieIsRevoked requires the exact refusal AuthRequired gives a
// revoked session on a page route — 303 to /login. Asserting merely "not 200"
// would also accept a 404, a 500 or an onboarding bounce, none of which prove
// the session was invalidated.
func assertAuthCookieIsRevoked(t *testing.T, ctx settingsSecurityTestContext, cookieHeader string, label string) {
	t.Helper()

	response := clearDataDashboardProbe(t, ctx, cookieHeader)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("%s: /dashboard status = %d, want 303 (revoked session must be bounced)", label, response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("%s: /dashboard redirect Location = %q, want %q", label, location, "/login")
	}
}

// TestClearDataRevokesOtherSessionsAndReissuesTheActingOne pins the contract
// that a successful POST /api/v1/users/current/data-wipe invalidates every auth
// cookie issued before the wipe: the originating device is refreshed inline so
// the owner who triggered the flow stays signed in, while a session that exists
// on a different device is rejected on its next request.
//
// The two halves are exercised through the app, not inferred:
//   - the "other device" is a genuinely separate login round trip, so it carries
//     its own session id. Reusing ctx.authCookie here would probe the acting
//     session — the one the handler deliberately refreshes — and the revocation
//     claim would hold vacuously.
//   - the reissued cookie is sent on a real request and must open /dashboard. A
//     cookie sealed against the pre-bump version is well-formed and non-empty
//     and dead on first use, so "the response carried a value" is not evidence
//     the originating device stayed signed in.
func TestClearDataRevokesOtherSessionsAndReissuesTheActingOne(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "clear-data-bumps-sv@example.com")

	otherDeviceCookie := loginAndExtractAuthCookieWithCSRF(t, ctx.app, ctx.user.Email, "StrongPass1")
	if strings.TrimSpace(otherDeviceCookie) == strings.TrimSpace(ctx.authCookie) {
		t.Fatal("the second login returned the acting session's cookie; the other-device probe would prove nothing")
	}
	actingCookieBeforeWipe := ctx.authCookie
	preWipeVersion := ctx.user.AuthSessionVersion

	// Positive anchor for the refusals below: both sessions open /dashboard
	// before the wipe, so a later 303 is the wipe's doing and not a fixture that
	// never authenticated in the first place.
	assertAuthCookieOpensDashboard(t, ctx, otherDeviceCookie, "other device before clear-data")
	assertAuthCookieOpensDashboard(t, ctx, actingCookieBeforeWipe, "acting device before clear-data")

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", form, map[string]string{"Accept": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("clear-data status = %d, want 200 or 303", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion <= preWipeVersion {
		t.Fatalf("auth_session_version did not advance after clear-data: before=%d after=%d", preWipeVersion, reloaded.AuthSessionVersion)
	}

	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("clear-data handler must reissue ovumcy_auth so the originating session stays alive")
	}
	assertAuthCookieOpensDashboard(t, ctx, cookiePair(refreshed), "reissued acting session after clear-data")

	assertAuthCookieIsRevoked(t, ctx, otherDeviceCookie, "other device after clear-data")
	assertAuthCookieIsRevoked(t, ctx, actingCookieBeforeWipe, "acting device's pre-wipe cookie after clear-data")
}
