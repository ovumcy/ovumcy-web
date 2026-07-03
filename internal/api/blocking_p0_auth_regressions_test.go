package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginPageUsesLoginHeadingOnFirstLaunch(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "ru")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected login status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlElementByID(document, "login-form") == nil {
		t.Fatalf("expected login form on login page")
	}
	if htmlElementByID(document, "register-form") != nil {
		t.Fatalf("did not expect register form on login page")
	}
}

func TestOnboardingStep1ShowsLocalizedErrorWhenDateMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "missing-date@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"last_period_start": {""},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "ru")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("step1 request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected step1 status 400, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFlashByKey(document, "onboarding.error.date_required") == nil {
		t.Fatalf("expected flash key onboarding.error.date_required in error response")
	}
}
