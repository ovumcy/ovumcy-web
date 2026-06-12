package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRegisterPageDoesNotPersistEmailAfterPasswordValidationError(t *testing.T) {
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
		bodyStringMatch{fragment: `id="register-email"`, message: "expected register email input on validation-error page"},
		bodyStringMatch{fragment: "Use 8 to 72 characters with uppercase, lowercase, and a number.", message: "expected localized weak password message from flash"},
	)
	// Email PII must not round-trip through the flash cookie (H-2).
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `value="` + email + `"`, message: "did not expect register email to be restored from flash"},
	)

	clean := renderCleanRegisterPage(t, app)
	assertBodyNotContainsAll(t, clean,
		bodyStringMatch{fragment: `value="` + email + `"`, message: "did not expect register email to persist after flash is consumed"},
		bodyStringMatch{fragment: "Use 8 to 72 characters with uppercase, lowercase, and a number.", message: "did not expect weak-password error after flash is consumed"},
	)
}

func TestRegisterInlineRecoveryConsumesStaleFlashCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "inline-flash-consume@example.com"

	staleFlashResponse := mustAppResponse(t, app, weakRegisterRequest(email))
	assertStatusCode(t, staleFlashResponse, http.StatusSeeOther)

	staleFlashValue := responseCookieValue(staleFlashResponse.Cookies(), flashCookieName)
	if staleFlashValue == "" {
		t.Fatalf("expected flash cookie after failed register attempt")
	}

	successForm := url.Values{
		"email":            {email},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	successRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(successForm.Encode()))
	successRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	successRequest.Header.Set("Accept-Language", "en")
	successRequest.Header.Set("Cookie", flashCookieName+"="+staleFlashValue)

	successResponse := mustAppResponse(t, app, successRequest)
	assertStatusCode(t, successResponse, http.StatusSeeOther)
	if location := successResponse.Header.Get("Location"); location != "/register/welcome" {
		t.Fatalf("expected successful register redirect to /register/welcome, got %q", location)
	}

	pickupCookie := responseCookieValue(successResponse.Cookies(), registerPickupCookieName)
	if pickupCookie == "" {
		t.Fatalf("expected pickup cookie after successful register")
	}
	if cookie := responseCookieValue(successResponse.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("expected no auth cookie on POST register; got %q", cookie)
	}
	if cookie := responseCookieValue(successResponse.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("expected no recovery cookie on POST register; got %q", cookie)
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickupCookie)

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected pickup redirect to /register, got %q", location)
	}

	authCookie := responseCookieValue(pickupResponse.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName)
	if authCookie == "" || recoveryCookie == "" {
		t.Fatalf("expected auth and recovery cookies after pickup")
	}

	inlineRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	inlineRequest.Header.Set("Accept-Language", "en")
	inlineRequest.Header.Set(
		"Cookie",
		authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie+"; "+flashCookieName+"="+staleFlashValue,
	)

	inlineResponse := mustAppResponse(t, app, inlineRequest)
	assertStatusCode(t, inlineResponse, http.StatusOK)

	rendered := mustReadBodyString(t, inlineResponse.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-auth-inline-recovery`, message: "expected inline recovery block after successful register"},
		bodyStringMatch{fragment: `id="recovery-code"`, message: "expected recovery code field after successful register"},
	)

	clearedFlash := responseCookie(inlineResponse.Cookies(), flashCookieName)
	if clearedFlash == nil {
		t.Fatalf("expected inline recovery response to clear stale flash cookie")
	}
	if clearedFlash.Value != "" {
		t.Fatalf("expected cleared flash cookie value, got %q", clearedFlash.Value)
	}
}

func weakRegisterRequest(email string) *http.Request {
	form := url.Values{
		"email":            {email},
		"password":         {"12345678"},
		"confirm_password": {"12345678"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
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
