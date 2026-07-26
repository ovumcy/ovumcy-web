package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestRecoveryCodePageRedirectsToDashboardWhenCookieMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-route-missing-cookie@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}
}

func TestRecoveryCodePageRejectsCookieFromDifferentUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userB := createOnboardingTestUser(t, database, "recovery-cookie-user-b@example.com", "StrongPass1", true)
	authCookieUserB := loginAndExtractAuthCookie(t, app, userB.Email, "StrongPass1")
	_, recoveryCookieUserA := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-user-a@example.com",
		"StrongPass1",
	)

	if recoveryCookieUserA == "" {
		t.Fatalf("expected recovery cookie for user A")
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieUserB+"; "+recoveryCodeCookieName+"="+recoveryCookieUserA)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected invalid recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

// TestRecoveryCodePageRefusesUnattributedRecoveryCookie is the recovery-code arm
// of the reveal contract that TestCalendarFeedRevealRefusesUnattributedCookie
// pins for the calendar feed: a sealed payload carrying `uid` 0 names no
// account, so the page must refuse it outright instead of skipping the owner
// comparison for want of an operand — otherwise the code renders for whichever
// session presents the cookie.
//
// Both payloads here are sealed under the app's own secret and open cleanly, so
// only the owner-scoping guard separates them. The attributed one is the
// positive anchor: it proves the page still reveals to the account it was minted
// for, so the refusal below is not just a page that shows nothing.
func TestRecoveryCodePageRefusesUnattributedRecoveryCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	owner := createOnboardingTestUser(t, database, "recovery-unattributed-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")

	const ownedCode = "OVUM-OWNED-CODE00"
	const unattributedCode = "OVUM-NOOWNER-CODE"

	// Positive anchor: a payload minted for this account reveals its code.
	attributed := sealCookieForTestApp(t, recoveryCodeCookieName,
		[]byte(`{"uid":`+strconv.FormatUint(uint64(owner.ID), 10)+`,"recovery_code":"`+ownedCode+`","continue_path":"/dashboard","continue_target":"dashboard","surface":"dedicated"}`))

	attributedRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	attributedRequest.Header.Set("Accept-Language", "en")
	attributedRequest.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+attributed)
	attributedResponse, err := app.Test(attributedRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("attributed recovery-code request failed: %v", err)
	}
	defer func() { _ = attributedResponse.Body.Close() }()
	if attributedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the owner's own recovery code to render, got %d", attributedResponse.StatusCode)
	}
	if !strings.Contains(mustReadBodyString(t, attributedResponse.Body), ownedCode) {
		t.Fatal("the recovery-code page must still reveal the code minted for this account")
	}

	// An unattributed payload carrying a different code must reveal nothing.
	unattributed := sealCookieForTestApp(t, recoveryCodeCookieName,
		[]byte(`{"uid":0,"recovery_code":"`+unattributedCode+`","continue_path":"/dashboard","continue_target":"dashboard","surface":"dedicated"}`))

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+unattributed)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unattributed recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected an unattributed recovery payload to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}
	if strings.Contains(mustReadBodyString(t, response.Body), unattributedCode) {
		t.Fatal("an unattributed recovery payload must not surface its code")
	}
	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatal("expected the refused recovery cookie to be cleared, not left presentable on a retry")
	}
}

func TestRecoveryCodePageRejectsTamperedRecoveryCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	authCookie, recoveryCookie := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-tampered@example.com",
		"StrongPass1",
	)

	if authCookie == "" || recoveryCookie == "" {
		t.Fatalf("expected auth and recovery cookies in register response")
	}

	separatorIndex := strings.Index(recoveryCookie, ".")
	if separatorIndex < 0 || separatorIndex+6 >= len(recoveryCookie) {
		t.Fatalf("expected versioned recovery cookie payload, got %q", recoveryCookie)
	}

	tampered := recoveryCookie[:separatorIndex+5] + "A" + recoveryCookie[separatorIndex+6:]
	if recoveryCookie[separatorIndex+5] == 'A' {
		tampered = recoveryCookie[:separatorIndex+5] + "B" + recoveryCookie[separatorIndex+6:]
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+tampered)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request with tampered cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected redirect to /onboarding, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected tampered recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

func registerAndExtractRecoveryCookies(t *testing.T, app *fiber.App, email string, password string) (string, string) {
	t.Helper()

	form := url.Values{
		"email":            {email},
		"password":         {password},
		"confirm_password": {password},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	registerResponse, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = registerResponse.Body.Close() }()

	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected register status 303, got %d", registerResponse.StatusCode)
	}

	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	pickupResponse, err := app.Test(pickupRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register/welcome request failed: %v", err)
	}
	defer func() { _ = pickupResponse.Body.Close() }()

	authCookie := responseCookieValue(pickupResponse.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName)
	return authCookie, recoveryCookie
}
