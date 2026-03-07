package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestLoginInvalidCredentialsRedirectPreservesEmail(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-email@example.com", "StrongPass1", true)

	response := mustAppResponse(t, app, loginInvalidCredentialsRequest(user.Email))
	assertStatusCode(t, response, http.StatusSeeOther)

	redirectURL := mustParseLocationHeader(t, response)
	assertCleanLoginRedirect(t, redirectURL)

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in login redirect response")
	}

	rendered := followLoginPageWithFlash(t, app, redirectURL.String(), flashValue)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="login-email"`, message: "expected login email input in page"},
		bodyStringMatch{fragment: `value="login-email@example.com"`, message: "expected login email input to keep previous value"},
		bodyStringMatch{fragment: "Invalid email or password.", message: "expected localized login error message from flash"},
		bodyStringMatch{fragment: `data-login-has-error="true"`, message: "expected login form to mark error state for password draft restore"},
		bodyStringMatch{fragment: `data-password-draft-key="ovumcy_login_password_draft"`, message: "expected login form to include password draft storage key"},
	)

	cleanPage := loadCleanLoginPage(t, app)
	assertBodyNotContainsAll(t, cleanPage,
		bodyStringMatch{fragment: `value="login-email@example.com"`, message: "did not expect login email to persist after flash is consumed"},
	)
	assertBodyContainsAll(t, cleanPage,
		bodyStringMatch{fragment: `data-login-has-error="false"`, message: "expected clean login page without error state marker after flash is consumed"},
	)
}

func loginInvalidCredentialsRequest(email string) *http.Request {
	form := url.Values{
		"email":    {email},
		"password": {"WrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func assertCleanLoginRedirect(t *testing.T, redirectURL *url.URL) {
	t.Helper()

	if redirectURL.Path != "/login" {
		t.Fatalf("expected redirect path /login, got %q", redirectURL.Path)
	}
	if query := strings.TrimSpace(redirectURL.RawQuery); query != "" {
		t.Fatalf("expected clean redirect without query params, got %q", query)
	}
}

func followLoginPageWithFlash(t *testing.T, app *fiber.App, location string, flashValue string) string {
	t.Helper()

	followRequest := httptest.NewRequest(http.MethodGet, location, nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", flashCookieName+"="+flashValue)
	followResponse := mustAppResponse(t, app, followRequest)
	assertStatusCode(t, followResponse, http.StatusOK)
	return mustReadBodyString(t, followResponse.Body)
}

func loadCleanLoginPage(t *testing.T, app *fiber.App) string {
	t.Helper()

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashResponse := mustAppResponse(t, app, afterFlashRequest)
	assertStatusCode(t, afterFlashResponse, http.StatusOK)
	return mustReadBodyString(t, afterFlashResponse.Body)
}
