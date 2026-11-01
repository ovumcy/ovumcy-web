package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestErasureStepupStartRefusesAPageFromAnotherAccount drives the stale-tab
// hazard on the two erasure step-up starts (WEB-400): a settings page rendered
// for another account, or a browser submit with no binding at all, must not
// start a re-authentication whose completion erases the account now signed in.
// Positive control: the account's own binding reaches the handler, which arms
// the step-up cookie.
func TestErasureStepupStartRefusesAPageFromAnotherAccount(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v1/users/current/data-wipe/step-up",
		"/api/v1/users/current/deletion/step-up",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "erasure-binding-"+strings.ReplaceAll(strings.TrimPrefix(path, "/api/v1/users/current/"), "/", "-")+"@example.com")
			other := createOnboardingTestUser(t, fixture.database, "erasure-binding-other-"+strings.ReplaceAll(path, "/", "-")+"@example.com", "StrongPass1", true)
			otherBinding := renderedDayFormAccount(t, fixture.app, issueAuthCookieForUser(t, other), "/settings")
			ownBinding := renderedDayFormAccount(t, fixture.app, fixture.authCookie, "/settings")

			start := func(binding string) *http.Response {
				t.Helper()
				csrfCookie, csrfToken := fixture.settingsCSRF(t)
				form := url.Values{"csrf_token": {csrfToken}}
				if binding != "" {
					form.Set(dayFormAccountField, binding)
				}
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("Accept", "text/html")
				request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
				return mustAppResponse(t, fixture.app, request)
			}
			armedStepup := func(response *http.Response) bool {
				cookie := responseCookie(response.Cookies(), oidcStepupCookieName)
				return cookie != nil && cookie.Value != ""
			}

			for label, binding := range map[string]string{"another account's binding": otherBinding, "no binding": ""} {
				response := start(binding)
				assertSettingsAccountChangedRefusal(t, path+" with "+label, response)
				if armedStepup(response) {
					t.Fatalf("%s with %s: a refused start must not arm the step-up cookie", path, label)
				}
			}

			response := start(ownBinding)
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK || !armedStepup(response) {
				t.Fatalf("%s: the account's own binding must reach the handler and arm the step-up, got %d", path, response.StatusCode)
			}
		})
	}
}
