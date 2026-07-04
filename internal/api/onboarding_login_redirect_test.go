package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginRedirectsToOnboardingWhenOnboardingIncomplete(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "owner@example.com", "StrongPass1", false)

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected redirect to /onboarding, got %q", location)
	}
}
