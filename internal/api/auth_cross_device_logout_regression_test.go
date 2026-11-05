package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

const (
	crossDeviceOwnerEmail    = "cross-device-owner@example.com"
	crossDeviceOtherEmail    = "cross-device-other@example.com"
	crossDeviceOwnerPassword = "StrongPass1"
)

// crossDevicePath is one read the probes issue, with the Accept header that
// selects its HTML or API arm.
type crossDevicePath struct {
	path   string
	accept string
}

var crossDeviceReads = []crossDevicePath{
	{path: "/dashboard"},
	{path: "/api/v1/stats/overview", accept: "application/json"},
}

// TestLogoutOnOneDeviceEndsTheOwnersSessionOnTheOther pins that a sign-out is
// account-wide: two browsers hold two live cookies of one owner, one of them
// signs out through the real route with its CSRF token, and the other is then
// refused on both an HTML page (303 to /login) and an API read (401), while a
// fresh sign-in on it works. A second owner of the same instance, signed in
// before the sign-out, stays signed in: the revoke reaches the owner's own
// sessions and nobody else's. Only a replay of the SAME cookie was asserted
// before; revoking just the presented session id would leave the other device
// signed in, which the owner who left a shared machine cannot see.
func TestLogoutOnOneDeviceEndsTheOwnersSessionOnTheOther(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	createOnboardingTestUser(t, database, crossDeviceOwnerEmail, crossDeviceOwnerPassword, true)
	createOnboardingTestUser(t, database, crossDeviceOtherEmail, crossDeviceOwnerPassword, true)

	deviceA := loginAndExtractAuthCookieWithCSRF(t, app, crossDeviceOwnerEmail, crossDeviceOwnerPassword)
	deviceB := loginAndExtractAuthCookieWithCSRF(t, app, crossDeviceOwnerEmail, crossDeviceOwnerPassword)
	otherOwner := loginAndExtractAuthCookieWithCSRF(t, app, crossDeviceOtherEmail, crossDeviceOwnerPassword)
	if deviceA == deviceB {
		t.Fatal("fixture: both devices hold the same cookie, so the test could not tell them apart")
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	csrfRequest.Header.Set("Accept-Language", "en")
	csrfRequest.Header.Set("Cookie", deviceA)
	csrfResponse, err := app.Test(csrfRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request for the csrf context failed: %v", err)
	}
	defer func() { _ = csrfResponse.Body.Close() }()
	body, err := io.ReadAll(csrfResponse.Body)
	if err != nil {
		t.Fatalf("read the dashboard body: %v", err)
	}
	csrfToken := extractCSRFTokenFromHTML(t, string(body))
	csrfCookie := responseCookie(csrfResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("expected a csrf cookie in the dashboard response")
	}

	// Positive anchor: before the sign-out every probe lets every session in, so
	// a refusal afterwards is the sign-out and not a probe that never worked.
	devices := []struct{ name, cookie string }{{"A", deviceA}, {"B", deviceB}, {"other owner", otherOwner}}
	for _, device := range devices {
		for _, read := range crossDeviceReads {
			if status, _ := crossDeviceProbe(t, app, device.cookie, read.path, read.accept); status != http.StatusOK {
				t.Fatalf("%s GET %s before logout: want 200, got %d", device.name, read.path, status)
			}
		}
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
	logout.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logout.Header.Set("Cookie", joinCookieHeader(deviceA, cookiePair(csrfCookie)))
	logoutResponse, err := app.Test(logout, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	defer func() { _ = logoutResponse.Body.Close() }()
	if logoutResponse.StatusCode != http.StatusSeeOther || logoutResponse.Header.Get("Location") != "/login" {
		t.Fatalf("logout on device A: want 303 to /login, got %d %q", logoutResponse.StatusCode, logoutResponse.Header.Get("Location"))
	}

	// The device that signed out is refused too: the revoke is the thing under
	// test, and a logout that did nothing would fail here as well as below.
	if status, location := crossDeviceProbe(t, app, deviceA, "/dashboard", ""); status != http.StatusSeeOther || location != "/login" {
		t.Fatalf("device A dashboard after its own logout: want 303 /login, got %d %q", status, location)
	}

	if status, location := crossDeviceProbe(t, app, deviceB, "/dashboard", ""); status != http.StatusSeeOther || location != "/login" {
		t.Fatalf("device B dashboard after logout on A: want 303 /login, got %d %q", status, location)
	}
	if status, _ := crossDeviceProbe(t, app, deviceB, "/api/v1/stats/overview", "application/json"); status != http.StatusUnauthorized {
		t.Fatalf("device B api read after logout on A: want 401, got %d", status)
	}

	for _, read := range crossDeviceReads {
		if status, _ := crossDeviceProbe(t, app, otherOwner, read.path, read.accept); status != http.StatusOK {
			t.Fatalf("the other owner's GET %s after the first owner's logout: want 200, got %d", read.path, status)
		}
	}

	freshB := loginAndExtractAuthCookieWithCSRF(t, app, crossDeviceOwnerEmail, crossDeviceOwnerPassword)
	for _, read := range crossDeviceReads {
		if status, _ := crossDeviceProbe(t, app, freshB, read.path, read.accept); status != http.StatusOK {
			t.Fatalf("device B GET %s after a fresh sign-in: want 200, got %d", read.path, status)
		}
	}
}

func crossDeviceProbe(t *testing.T, app *fiber.App, cookie string, path string, accept string) (int, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	request.Header.Set("Cookie", cookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, response.Header.Get("Location")
}
