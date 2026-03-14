package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

var csrfMetaTokenPattern = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

func TestAuthLogoutPostWithCSRFRedirectsAndClearsCookies(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			authCookie,
			cookiePair(csrfCookie),
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
		),
	)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("logout POST request with csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	authCookieAfterLogout := responseCookie(response.Cookies(), authCookieName)
	if authCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear auth cookie")
	}
	if authCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", authCookieAfterLogout.Value)
	}

	recoveryCookieAfterLogout := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear recovery code cookie")
	}
	if recoveryCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared recovery code cookie value, got %q", recoveryCookieAfterLogout.Value)
	}

	resetCookieAfterLogout := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookieAfterLogout == nil {
		t.Fatalf("expected logout response to clear reset password cookie")
	}
	if resetCookieAfterLogout.Value != "" {
		t.Fatalf("expected cleared reset password cookie value, got %q", resetCookieAfterLogout.Value)
	}
}

func TestAuthLogoutPostRevokesPreviousSessionCookie(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {csrfToken}}
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(form.Encode()))
	logoutRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	logoutResponse, err := app.Test(logoutRequest, -1)
	if err != nil {
		t.Fatalf("logout POST request failed: %v", err)
	}
	defer logoutResponse.Body.Close()

	if logoutResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected logout status 303, got %d", logoutResponse.StatusCode)
	}

	replayRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	replayRequest.Header.Set("Accept-Language", "en")
	replayRequest.Header.Set("Cookie", authCookie)

	replayResponse, err := app.Test(replayRequest, -1)
	if err != nil {
		t.Fatalf("dashboard replay request failed: %v", err)
	}
	defer replayResponse.Body.Close()

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

	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("logout POST request without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}

	assertAuthenticatedDashboardAccess(t, app, authCookie)
}

func TestAuthLogoutPostInvalidCSRFRejectedByMiddleware(t *testing.T) {
	app, authCookie, csrfCookie, csrfToken := prepareAuthenticatedLogoutCSRFContext(t)

	form := url.Values{"csrf_token": {"invalid-" + csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("logout POST request with invalid csrf failed: %v", err)
	}
	defer response.Body.Close()

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

	csrfResponse, err := app.Test(csrfRequest, -1)
	if err != nil {
		t.Fatalf("dashboard request for csrf context failed: %v", err)
	}
	defer csrfResponse.Body.Close()

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

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request after csrf failure failed: %v", err)
	}
	defer response.Body.Close()

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
