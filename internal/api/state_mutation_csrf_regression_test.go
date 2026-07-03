package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Defense-in-depth: every state-mutating endpoint must be CSRF-protected.
// These regressions assert that omitting the csrf_token form field returns
// 403 even on authenticated days/onboarding endpoints — covering the gap
// that prior coverage only spanned logout/export/settings.

func TestDaysUpsertPostRejectsRequestsMissingCSRFToken(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "days-csrf@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	today := time.Now().UTC().Format("2006-01-02")
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+today, strings.NewReader(url.Values{
		"notes": {"missing csrf token"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("days upsert without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware to reject days upsert with 403, got %d", response.StatusCode)
	}
}

func TestOnboardingStep1PostRejectsRequestsMissingCSRFToken(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "onboarding-csrf@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(url.Values{
		"cycle_length":  {"28"},
		"period_length": {"5"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding step1 without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware to reject onboarding step1 with 403, got %d", response.StatusCode)
	}
}

func TestOnboardingStep2PostRejectsRequestsMissingCSRFToken(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-csrf@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader(url.Values{
		"cycle_length":  {"28"},
		"period_length": {"5"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding step2 without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware to reject onboarding step2 with 403, got %d", response.StatusCode)
	}
}

func TestOnboardingCompletePostRejectsRequestsMissingCSRFToken(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "onboarding-complete-csrf@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding complete without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware to reject onboarding complete with 403, got %d", response.StatusCode)
	}
}
