package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
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
	defer func() { _ = response.Body.Close() }()

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

	// Byte-for-byte, not by length: a body that carries a per-branch token of
	// the same width ("register_welcome" vs "register_welcomf") is an oracle
	// that any length comparison reads as parity. Both bodies must also be
	// explicitly empty, so equal-but-populated redirect bodies cannot pass.
	newBody := mustReadBodyString(t, newResponse.Body)
	dupBody := mustReadBodyString(t, dupResponse.Body)
	if newBody != dupBody {
		t.Fatalf("body mismatch: new=%q duplicate=%q", newBody, dupBody)
	}
	if newBody != "" {
		t.Fatalf("expected an empty redirect body on the new-email branch, got %q", newBody)
	}
	if dupBody != "" {
		t.Fatalf("expected an empty redirect body on the duplicate-email branch, got %q", dupBody)
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

	assertRegisterJSONPayloadParity(t, app, "parity-fresh-json@example.com", primaryEmail)
}

// assertRegisterJSONPayloadParity re-drives both branches with a JSON-negotiated
// request. The HTML branch answers with an empty 303 body, so comparing bodies
// there — however strictly — cannot observe a payload-carried oracle at all; the
// JSON representation of the same endpoint is where the register response has
// fields to diverge in. Both payloads are decoded and pinned to their expected
// values, so a divergence that keeps every body the same width still fails, and
// so does a token that drifts on both branches at once.
func assertRegisterJSONPayloadParity(t *testing.T, app *fiber.App, freshEmail, duplicateEmail string) {
	t.Helper()

	newResponse := mustAppResponse(t, app, jsonRegisterRequest(freshEmail))
	dupResponse := mustAppResponse(t, app, jsonRegisterRequest(duplicateEmail))

	assertStatusCode(t, newResponse, http.StatusCreated)
	assertStatusCode(t, dupResponse, http.StatusCreated)

	newBody := mustReadBodyString(t, newResponse.Body)
	dupBody := mustReadBodyString(t, dupResponse.Body)
	if newBody != dupBody {
		t.Fatalf("JSON body mismatch: new=%q duplicate=%q", newBody, dupBody)
	}

	for _, branch := range []struct {
		label string
		body  string
	}{{label: "new", body: newBody}, {label: "duplicate", body: dupBody}} {
		payload := struct {
			OK       bool   `json:"ok"`
			NextStep string `json:"next_step"`
			NextPath string `json:"next_path"`
		}{}
		if err := json.Unmarshal([]byte(branch.body), &payload); err != nil {
			t.Fatalf("decode %s register payload %q: %v", branch.label, branch.body, err)
		}
		if !payload.OK {
			t.Fatalf("%s register payload reported ok=false: %q", branch.label, branch.body)
		}
		if payload.NextStep != "register_welcome" {
			t.Fatalf("%s register payload next_step = %q, want %q", branch.label, payload.NextStep, "register_welcome")
		}
		if payload.NextPath != "/register/welcome" {
			t.Fatalf("%s register payload next_path = %q, want %q", branch.label, payload.NextPath, "/register/welcome")
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

func jsonRegisterRequest(email string) *http.Request {
	request := registerRequest(email)
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
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
	defer func() { _ = response.Body.Close() }()

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
