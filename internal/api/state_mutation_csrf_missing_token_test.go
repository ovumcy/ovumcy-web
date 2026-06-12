package api

import (
	"net/http"
	"net/url"
	"testing"
)

// TestStateMutatingEndpointsRejectMissingCSRFToken closes the route-level CSRF
// gap for the state-mutating endpoints whose only prior coverage went through a
// no-CSRF test app or a valid-token happy path. Each request is sent through the
// real app (CSRF middleware mounted) with the auth and csrf cookies present but
// no csrf_token form field, so the CSRF middleware must reject it with 403
// before the handler runs. Per the CSRF security invariant, every state-mutating
// endpoint needs its own missing-token 403 regression — adjacent coverage does
// not satisfy the rule, so each route gets its own subtest assertion here.
func TestStateMutatingEndpointsRejectMissingCSRFToken(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "state-mutation-csrf@example.com")

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "update tracking settings", method: http.MethodPatch, path: "/api/v1/users/current/tracking"},
		{name: "enable 2fa", method: http.MethodPut, path: "/api/v1/users/current/2fa"},
		{name: "disable 2fa", method: http.MethodDelete, path: "/api/v1/users/current/2fa"},
		{name: "password step-up reauth", method: http.MethodPost, path: "/api/v1/users/current/password/step-up"},
		{name: "login", method: http.MethodPost, path: "/api/v1/sessions"},
		{name: "totp login challenge", method: http.MethodPost, path: "/api/v1/sessions/2fa-challenge"},
		{name: "forgot password", method: http.MethodPost, path: "/api/v1/password-resets"},
		{name: "reset password redeem", method: http.MethodPost, path: "/api/v1/password-resets/redeem"},
		{name: "create symptom", method: http.MethodPost, path: "/api/v1/symptoms"},
		{name: "update symptom", method: http.MethodPatch, path: "/api/v1/symptoms/1"},
		{name: "delete symptom", method: http.MethodDelete, path: "/api/v1/symptoms/1"},
		{name: "restore symptom", method: http.MethodPost, path: "/api/v1/symptoms/1/restore"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := settingsRequestWithoutCSRF(t, ctx, tc.method, tc.path, url.Values{}, nil)
			defer response.Body.Close()

			assertStatusCode(t, response, http.StatusForbidden)
		})
	}
}
