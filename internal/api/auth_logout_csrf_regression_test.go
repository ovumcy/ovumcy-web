package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

var csrfMetaTokenPattern = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

func TestAuthLogoutPostWithCSRFRedirectsAndClearsCookies(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			authCookie,
			cookiePair(csrfCookie),
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
			totpPendingCookieName+"=temporary-totp-pending",
			totpSetupCookieName+"=temporary-totp-setup",
			calendarFeedRevealCookieName+"=temporary-feed-reveal",
			languageCookieName+"=ru",
		),
	)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout POST request with csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	// Every sealed cookie scoped to the ended session comes back retracted, the
	// pending TOTP enrollment secret and the one-time calendar-feed subscribe URL
	// included: both outlive the session otherwise, the first for the whole
	// browser session and the second as a capability sign-out does not revoke.
	// The cookies logout has always cleared stay in the same list as the anchor —
	// a logout that stopped emitting Set-Cookie at all fails here rather than
	// passing for want of anything left to check.
	//
	// `ovumcy_lang` is the one member that is neither sealed nor session-scoped:
	// a year-long plaintext cache of the account's stored language. Surviving a
	// sign-out it tells whoever opens the browser next that this app is used here
	// and in which language, before any authentication — so a deliberate session
	// end retracts it too (clearSessionEndCookies). A session REJECTION must not,
	// which TestSessionRejectionLeavesTheLanguageCookieInPlace pins.
	for _, cookieName := range []string{
		authCookieName,
		recoveryCodeCookieName,
		resetPasswordCookieName,
		totpPendingCookieName,
		totpSetupCookieName,
		calendarFeedRevealCookieName,
		languageCookieName,
	} {
		cleared := responseCookie(response.Cookies(), cookieName)
		if cleared == nil {
			t.Fatalf("expected logout response to clear %s", cookieName)
		}
		if cleared.Value != "" {
			t.Fatalf("expected cleared %s value, got %q", cookieName, cleared.Value)
		}
		if !cleared.Expires.Before(time.Now()) {
			t.Fatalf("expected %s to be retracted with a past expiry, got %s", cookieName, cleared.Expires)
		}
	}
}

func TestAuthLogoutPostRevokesPreviousSessionCookie(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {csrfToken}}
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	logoutRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	logoutResponse, err := app.Test(logoutRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout POST request failed: %v", err)
	}
	defer func() { _ = logoutResponse.Body.Close() }()

	if logoutResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected logout status 303, got %d", logoutResponse.StatusCode)
	}

	replayRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	replayRequest.Header.Set("Accept-Language", "en")
	replayRequest.Header.Set("Cookie", authCookie)

	replayResponse, err := app.Test(replayRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard replay request failed: %v", err)
	}
	defer func() { _ = replayResponse.Body.Close() }()

	if replayResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected replayed cookie to be rejected with 303, got %d", replayResponse.StatusCode)
	}
	if location := replayResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected replayed cookie redirect to /login, got %q", location)
	}

	clearedCookie := responseCookie(replayResponse.Cookies(), authCookieName)
	if clearedCookie == nil {
		t.Fatal("expected replayed cookie response to clear auth cookie")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared replayed auth cookie value, got %q", clearedCookie.Value)
	}
}

func TestAuthLogoutPostMissingCSRFRejectedByMiddleware(t *testing.T) {
	app, authCookie, _, _ := prepareAuthenticatedLogoutCSRFContext(t)

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout POST request without csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}

	assertAuthenticatedDashboardAccess(t, app, authCookie)
}

func TestAuthLogoutPostInvalidCSRFRejectedByMiddleware(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {"invalid-" + csrfToken}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout POST request with invalid csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}

	assertAuthenticatedDashboardAccess(t, app, authCookie)
}

func prepareAuthenticatedLogoutCSRFContext(t *testing.T) (*fiber.App, string, *http.Cookie, string) {
	t.Helper()

	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "logout-csrf@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	csrfRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	csrfRequest.Header.Set("Accept-Language", "en")
	csrfRequest.Header.Set("Cookie", authCookie)

	csrfResponse, err := app.Test(csrfRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request for csrf context failed: %v", err)
	}
	defer func() { _ = csrfResponse.Body.Close() }()

	if csrfResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard status 200 while preparing csrf context, got %d", csrfResponse.StatusCode)
	}

	body, err := io.ReadAll(csrfResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body while preparing csrf context: %v", err)
	}
	csrfToken := extractCSRFTokenFromHTML(t, string(body))

	csrfCookie := responseCookie(csrfResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatalf("expected csrf cookie in dashboard response")
	}

	return app, authCookie, csrfCookie, csrfToken
}

func extractCSRFTokenFromHTML(t *testing.T, html string) string {
	t.Helper()

	match := csrfMetaTokenPattern.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf("expected csrf token meta tag in rendered html")
	}
	token := strings.TrimSpace(match[1])
	if token == "" {
		t.Fatalf("expected non-empty csrf token value")
	}
	return token
}

func assertAuthenticatedDashboardAccess(t *testing.T, app *fiber.App, authCookie string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request after csrf failure failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard status 200 after csrf failure, got %d", response.StatusCode)
	}
}

func joinCookieHeader(values ...string) string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "; ")
}

func cookiePair(cookie *http.Cookie) string {
	if cookie == nil {
		return ""
	}
	return cookie.Name + "=" + cookie.Value
}
