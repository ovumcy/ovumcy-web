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

// TestLogoutOnOneDeviceEndsTheOwnersSessionOnTheOther pins that a sign-out is
// account-wide: two browsers hold two live cookies of one owner, one of them
// signs out through the real route with its CSRF token, and the other is then
// refused on both an HTML page (303 to /login) and an API read (401), while a
// fresh sign-in on it works. Only a replay of the SAME cookie was asserted
// before; revoking just the presented session id would leave the other device
// signed in, which the owner who left a shared machine cannot see.
func TestLogoutOnOneDeviceEndsTheOwnersSessionOnTheOther(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "cross-device-logout@example.com", "StrongPass1", true)

	deviceA := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	deviceB := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	if deviceA == deviceB {
		t.Fatal("fixture: both devices hold the same cookie, so the test could not tell them apart")
	}

	// Positive anchor: before the sign-out every probe lets both devices in, so
	// a refusal afterwards is the sign-out and not a probe that never worked.
	for name, cookie := range map[string]string{"A": deviceA, "B": deviceB} {
		if status, _ := crossDeviceProbe(t, app, cookie, "/dashboard", ""); status != http.StatusOK {
			t.Fatalf("device %s dashboard before logout: want 200, got %d", name, status)
		}
		if status, _ := crossDeviceProbe(t, app, cookie, "/api/v1/stats/overview", "application/json"); status != http.StatusOK {
			t.Fatalf("device %s api read before logout: want 200, got %d", name, status)
		}
	}

	dashboard := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboard.Header.Set("Accept-Language", "en")
	dashboard.Header.Set("Cookie", deviceA)
	dashboardResponse, err := app.Test(dashboard, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request for csrf context failed: %v", err)
	}
	defer func() { _ = dashboardResponse.Body.Close() }()
	body, err := io.ReadAll(dashboardResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	csrfToken := extractCSRFTokenFromHTML(t, string(body))
	csrfCookie := responseCookie(dashboardResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("csrf cookie is missing in the dashboard response")
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
	logout.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logout.Header.Set("Cookie", joinCookieHeader(deviceA, cookiePair(csrfCookie)))
	logoutResponse, err := app.Test(logout, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	defer func() { _ = logoutResponse.Body.Close() }()
	if logoutResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout on device A: want 303, got %d", logoutResponse.StatusCode)
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

	freshB := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	if status, _ := crossDeviceProbe(t, app, freshB, "/dashboard", ""); status != http.StatusOK {
		t.Fatalf("device B dashboard after a fresh sign-in: want 200, got %d", status)
	}
	if status, _ := crossDeviceProbe(t, app, freshB, "/api/v1/stats/overview", "application/json"); status != http.StatusOK {
		t.Fatalf("device B api read after a fresh sign-in: want 200, got %d", status)
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
