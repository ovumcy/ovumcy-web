package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func sendSignedInDayForm(t *testing.T, ctx settingsSecurityTestContext, headers map[string]string) *http.Response {
	t.Helper()
	_, iso := noJSDay()
	body := url.Values{"_method": {"PUT"}, "is_period": {"true"}, "csrf_token": {ctx.csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+iso+"?source=calendar", strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", ctx.authCookie+"; "+ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return mustAppResponse(t, ctx.app, request)
}

// TestAuthRequiredAnswersAStorageFaultAsAServerError (WEB-290): a session that
// cannot be resolved because storage failed is not a signed-out caller. The
// gate must answer 5xx and leave the cookie alone — not clear it, not send a
// no-JS form to /login with a "not signed in" notice that a still-live session
// would bounce past, leaving the notice sealed for the next sign-in.
func TestAuthRequiredAnswersAStorageFaultAsAServerError(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		"no-JS browser form": {"Accept": noJSBrowserAccept},
		"JSON client":        {"Accept": "application/json"},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, "resolve-fault-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			sqlDB, err := ctx.database.DB()
			if err != nil {
				t.Fatalf("open sql db: %v", err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatalf("close sql db: %v", err)
			}

			response := sendSignedInDayForm(t, ctx, headers)
			defer func() { _ = response.Body.Close() }()
			assertStatusCode(t, response, http.StatusInternalServerError)
			if location := response.Header.Get("Location"); location != "" {
				t.Fatalf("Location %q, want none", location)
			}
			for _, cookie := range response.Cookies() {
				if cookie.Name == flashCookieName || cookie.Name == exemptFlashCookieName || cookie.Name == authCookieName {
					t.Fatalf("a storage fault set the cookie %q", cookie.Name)
				}
			}
		})
	}
}

// TestAuthRequiredSendsAnUnsupportedRoleNoJSFormToSignIn (WEB-290): the gate
// clears the session of an account whose role the web surface does not serve,
// so the refusal page's link back to the form would bounce off the gate with
// no notice. The browser goes to /login carrying the notice instead.
func TestAuthRequiredSendsAnUnsupportedRoleNoJSFormToSignIn(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "unsupported-role-no-js@example.com")
	if err := ctx.database.Model(&ctx.user).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}

	response := sendSignedInDayForm(t, ctx, map[string]string{"Accept": noJSBrowserAccept})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusSeeOther)
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	assertCleanLoginRedirect(t, location)
	want := authWebSignInUnavailableErrorSpec().Key
	for _, cookie := range response.Cookies() {
		if cookie.Name == flashCookieName && cookie.Value != "" {
			if got := decodeFlashCookieForTest(t, cookie.Value).AuthError; got != want {
				t.Fatalf("flash AuthError = %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatal("the redirect to /login carries no flash notice")
}
