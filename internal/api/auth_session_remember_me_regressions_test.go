package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Remember-me is the owner's choice about a device, and only the login form
// carries a control to make it with. Two other paths minted a session — the
// recovery reset and the register pickup — and both passed rememberMe=true
// unconditionally, so an owner who was never asked was remembered for 30 days
// anyway; on the recovery path, on the one flow whose premise is that she just
// lost control of her password.
//
// The first row is the anchor. It keeps the rest reading as "not asked, not
// remembered" instead of "the feature was removed".
func TestSessionsMintedWithoutARememberChoiceAreSessionScoped(t *testing.T) {
	testCases := []struct {
		name           string
		mint           func(t *testing.T) *http.Response
		wantRemembered bool
	}{
		{name: "login with the box checked", mint: mintSessionByLoginRemembering, wantRemembered: true},
		{name: "login with the box unchecked", mint: mintSessionByLogin},
		{name: "recovery reset", mint: mintSessionByRecoveryReset},
		{name: "register pickup", mint: mintSessionByRegisterPickup},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := testCase.mint(t)

			authCookie := responseCookie(response.Cookies(), authCookieName)
			if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
				t.Fatalf("expected the flow to mint a session, got %#v", authCookie)
			}
			remembered := !authCookie.Expires.IsZero()
			if remembered != testCase.wantRemembered {
				t.Fatalf("expected remembered=%t, got %t (expires %v)", testCase.wantRemembered, remembered, authCookie.Expires)
			}
			// An anchor that only checked for the presence of an expiry would
			// still pass if remembering silently shortened to the default TTL.
			if remembered {
				if remaining := time.Until(authCookie.Expires); remaining < 29*24*time.Hour {
					t.Fatalf("expected a remembered cookie to outlive the default TTL, got %s", remaining)
				}
			}
		})
	}
}

func mintSessionByLogin(t *testing.T) *http.Response {
	t.Helper()
	return loginForRememberMe(t, "remember-plain@example.com", url.Values{})
}

func mintSessionByLoginRemembering(t *testing.T) *http.Response {
	t.Helper()
	return loginForRememberMe(t, "remember-checked@example.com", url.Values{"remember_me": {"1"}})
}

func loginForRememberMe(t *testing.T, email string, extra url.Values) *http.Response {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	createOnboardingTestUser(t, database, email, "StrongPass1", true)

	form := url.Values{"email": {email}, "password": {"StrongPass1"}}
	for key, values := range extra {
		form[key] = values
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return mustAppResponse(t, app, request)
}

func mintSessionByRecoveryReset(t *testing.T) *http.Response {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "remember-recovery@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	return redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
}

func mintSessionByRegisterPickup(t *testing.T) *http.Response {
	t.Helper()

	app, _ := newOnboardingTestApp(t)
	pickupCookie := registerAndExtractPickupCookie(t, app, "remember-pickup@example.com")

	return pickupRegisterWithHeaders(t, app, pickupCookie, sameOriginNavigation)
}
