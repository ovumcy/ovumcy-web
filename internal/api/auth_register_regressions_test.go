package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func TestRegisterValidationErrorRedirectDoesNotLeakEmailOrErrorInQuery(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "test@test.com"

	form := url.Values{
		"email":            {email},
		"password":         {"12345678"},
		"confirm_password": {"12345678"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	location := strings.TrimSpace(response.Header.Get("Location"))
	if location == "" {
		t.Fatalf("expected redirect location")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if parsed.Path != "/register" {
		t.Fatalf("expected redirect path /register, got %q", parsed.Path)
	}
	if strings.TrimSpace(parsed.RawQuery) != "" {
		t.Fatalf("expected empty redirect query, got %q", parsed.RawQuery)
	}
	if strings.Contains(strings.ToLower(location), "test%40test.com") || strings.Contains(strings.ToLower(location), "test@test.com") {
		t.Fatalf("did not expect email leakage in redirect location: %q", location)
	}
	if strings.Contains(strings.ToLower(location), "weak+password") || strings.Contains(strings.ToLower(location), "error=") {
		t.Fatalf("did not expect error leakage in redirect location: %q", location)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for register validation error")
	}
}

// TestRegisterResponseParityBetweenNewAndDuplicateEmail closes the per-request
// Set-Cookie enumeration oracle: POST /api/v1/users must emit identical
// status, body, redirect target, and Set-Cookie shape regardless of whether
// the email was new or already registered. Any divergence here re-opens the
// oracle that the pickup-cookie redesign was meant to remove.
func TestRegisterResponseParityBetweenNewAndDuplicateEmail(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	primaryEmail := "parity-primary@example.com"
	freshEmail := "parity-fresh@example.com"

	// Seed: register primary so a later attempt collides.
	seed := registerRequest(primaryEmail)
	seedResponse := mustAppResponse(t, app, seed)
	assertStatusCode(t, seedResponse, http.StatusSeeOther)

	// First branch: brand-new email, should succeed (creates user + pickup).
	newResponse := mustAppResponse(t, app, registerRequest(freshEmail))
	// Second branch: duplicate email, should silently emit the same shape.
	dupResponse := mustAppResponse(t, app, registerRequest(primaryEmail))

	if newResponse.StatusCode != dupResponse.StatusCode {
		t.Fatalf(
			"status mismatch between new (%d) and duplicate (%d) responses",
			newResponse.StatusCode, dupResponse.StatusCode,
		)
	}

	if newLoc, dupLoc := newResponse.Header.Get("Location"), dupResponse.Header.Get("Location"); newLoc != dupLoc {
		t.Fatalf("Location header mismatch: new=%q duplicate=%q", newLoc, dupLoc)
	}

	newBody := mustReadBodyString(t, newResponse.Body)
	dupBody := mustReadBodyString(t, dupResponse.Body)
	if len(newBody) != len(dupBody) {
		t.Fatalf("body length mismatch: new=%d duplicate=%d", len(newBody), len(dupBody))
	}

	newCookies := indexSetCookies(newResponse)
	dupCookies := indexSetCookies(dupResponse)

	if len(newCookies) != len(dupCookies) {
		t.Fatalf(
			"Set-Cookie count mismatch: new=%d duplicate=%d (new=%v duplicate=%v)",
			len(newCookies), len(dupCookies), cookieNames(newResponse), cookieNames(dupResponse),
		)
	}

	for _, name := range cookieNames(newResponse) {
		assertCookieParity(t, name, newCookies[name], dupCookies[name])
	}

	// Both branches must specifically emit the pickup cookie and must NOT leak
	// the real auth or recovery cookies on the register response itself.
	for label, cookies := range map[string]map[string]*http.Cookie{"new": newCookies, "duplicate": dupCookies} {
		if cookies[registerPickupCookieName] == nil {
			t.Fatalf("%s response missing pickup cookie", label)
		}
		if cookies[authCookieName] != nil {
			t.Fatalf("%s response unexpectedly issued auth cookie", label)
		}
		if cookies[recoveryCodeCookieName] != nil {
			t.Fatalf("%s response unexpectedly issued recovery cookie", label)
		}
	}
}

func assertCookieParity(t *testing.T, name string, want, got *http.Cookie) {
	t.Helper()
	if got == nil {
		t.Fatalf("duplicate response missing cookie %q present in new response", name)
	}
	if len(want.Value) != len(got.Value) {
		t.Fatalf("cookie %q length mismatch: new=%d duplicate=%d", name, len(want.Value), len(got.Value))
	}
	if want.Path != got.Path {
		t.Fatalf("cookie %q path mismatch: new=%q duplicate=%q", name, want.Path, got.Path)
	}
	if want.HttpOnly != got.HttpOnly {
		t.Fatalf("cookie %q HttpOnly mismatch", name)
	}
	if want.Secure != got.Secure {
		t.Fatalf("cookie %q Secure mismatch", name)
	}
	if want.SameSite != got.SameSite {
		t.Fatalf("cookie %q SameSite mismatch", name)
	}
}

func registerRequest(email string) *http.Request {
	form := url.Values{
		"email":            {email},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")
	return request
}

func indexSetCookies(response *http.Response) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, cookie := range response.Cookies() {
		out[cookie.Name] = cookie
	}
	return out
}

func cookieNames(response *http.Response) []string {
	names := []string{}
	for _, cookie := range response.Cookies() {
		names = append(names, cookie.Name)
	}
	sort.Strings(names)
	return names
}

func TestRegisterSuccessIssuesPickupCookieAndRedirectsToWelcome(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "autologin-register@example.com"

	form := url.Values{
		"email":            {email},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register success request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/register/welcome" {
		t.Fatalf("expected redirect to /register/welcome, got %q", location)
	}

	if cookie := responseCookieValue(response.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("expected no auth cookie on POST register; got %q", cookie)
	}
	if cookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("expected no recovery cookie on POST register; got %q", cookie)
	}
	if pickup := responseCookieValue(response.Cookies(), registerPickupCookieName); pickup == "" {
		t.Fatalf("expected pickup cookie in register response")
	}
}
