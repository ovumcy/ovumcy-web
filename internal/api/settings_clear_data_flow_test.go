package api

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
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
	// Validate must confirm the password WITHOUT wiping anything: the seeded logs,
	// custom symptoms, and cycle settings must all survive an accepted validation.
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
}

// TestValidateClearDataPasswordRateLimitsGuessesAndRefusesCorrectPassword closes
// the password-oracle gap at the transport layer: /data-wipe/validate changes no
// state, so without a budget it is a pure oracle capped only by the /api
// catch-all — 300 guesses per minute against 8 per 15 minutes on the login form.
// Once the budget is spent the endpoint must answer 429 even for the CORRECT
// password, mirroring the login-budget contract of
// TestCompleteOIDCLinkConfirmationRateLimitsPasswordAttempts.
func TestValidateClearDataPasswordRateLimitsGuessesAndRefusesCorrectPassword(t *testing.T) {
	scenario := setupClearDataScenario(t)
	ctx := settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}
	validate := func(t *testing.T, password string) int {
		t.Helper()
		response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
			"password": {password},
		}, map[string]string{
			"Accept": "application/json",
		})
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode
	}

	// The default budget is 5 failures per 15 minutes; spend it.
	for i := 1; i <= services.DefaultSettingsReauthAttemptsLimit; i++ {
		if status := validate(t, "WrongPass1"); status != http.StatusUnauthorized {
			t.Fatalf("wrong-password attempt %d: got %d, want 401", i, status)
		}
	}

	if status := validate(t, "WrongPass1"); status != http.StatusTooManyRequests {
		t.Fatalf("wrong password past the budget: got %d, want 429 — the endpoint has no attempt budget", status)
	}
	if status := validate(t, "StrongPass1"); status != http.StatusTooManyRequests {
		t.Fatalf("correct password past the budget: got %d, want 429 — an exhausted budget must refuse the correct password too", status)
	}

	// The wipe itself must be refused on the same budget, not just the
	// validation probe: otherwise the oracle simply moves one endpoint over.
	wipe := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = wipe.Body.Close() }()
	if wipe.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("data-wipe past the budget: got %d, want 429", wipe.StatusCode)
	}
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
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
