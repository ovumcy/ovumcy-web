package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRegisterPageKeepsEmailAfterPasswordValidationError(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "persist-register@example.com"

	response := mustAppResponse(t, app, weakRegisterRequest(email))
	assertStatusCode(t, response, http.StatusSeeOther)

	parsedLocation := mustParseLocationHeader(t, response)
	if parsedLocation.Path != "/register" {
		t.Fatalf("expected redirect to /register, got %q", parsedLocation.Path)
	}
	if strings.TrimSpace(parsedLocation.RawQuery) != "" {
		t.Fatalf("expected redirect without query params, got %q", parsedLocation.RawQuery)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in register redirect response")
	}

	rendered := renderRegisterPageWithFlash(t, app, flashValue)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="register-email" type="email" name="email" value="` + email + `"`, message: "expected register page to keep submitted email after validation error"},
		bodyStringMatch{fragment: "Use at least 8 characters with uppercase, lowercase, and a number.", message: "expected localized weak password message from flash"},
	)

	clean := renderCleanRegisterPage(t, app)
	assertBodyNotContainsAll(t, clean,
		bodyStringMatch{fragment: `id="register-email" type="email" name="email" value="` + email + `"`, message: "did not expect register email to persist after flash is consumed"},
		bodyStringMatch{fragment: "Use at least 8 characters with uppercase, lowercase, and a number.", message: "did not expect weak-password error after flash is consumed"},
	)
}

func weakRegisterRequest(email string) *http.Request {
	form := url.Values{
		"email":            {email},
		"password":         {"12345678"},
		"confirm_password": {"12345678"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func renderRegisterPageWithFlash(t *testing.T, app *fiber.App, flashValue string) string {
	t.Helper()

	registerRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	registerRequest.Header.Set("Accept-Language", "en")
	registerRequest.Header.Set("Cookie", flashCookieName+"="+flashValue)
	registerResponse := mustAppResponse(t, app, registerRequest)
	assertStatusCode(t, registerResponse, http.StatusOK)
	return mustReadBodyString(t, registerResponse.Body)
}

func renderCleanRegisterPage(t *testing.T, app *fiber.App) string {
	t.Helper()

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashResponse := mustAppResponse(t, app, afterFlashRequest)
	assertStatusCode(t, afterFlashResponse, http.StatusOK)
	return mustReadBodyString(t, afterFlashResponse.Body)
}
