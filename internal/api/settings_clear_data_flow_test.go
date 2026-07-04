package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestClearDataRemovesTrackedCalendarEntriesAndResetsCycleSettings(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected clear data status 200, got %d", response.StatusCode)
	}

	assertClearDataPostconditions(t, scenario.database, scenario.user)
}

func TestClearDataRejectsMissingPassword(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected clear data status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected invalid password error, got %q", got)
	}
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
}

func TestClearDataRejectsInvalidPassword(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected clear data status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected invalid password error, got %q", got)
	}
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
}

func TestValidateClearDataPasswordAcceptsCorrectPassword(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected validate clear data status 200, got %d", response.StatusCode)
	}
}

func TestValidateClearDataPasswordRejectsInvalidPassword(t *testing.T) {
	scenario := setupClearDataScenario(t)

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected validate clear data status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected invalid password error, got %q", got)
	}
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
}
