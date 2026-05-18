package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestRegisterClosedModeReturnsForbiddenJSONError(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	form := url.Values{
		"email":            {"closed-mode@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "registration disabled" {
		t.Fatalf("expected registration disabled error, got %q", got)
	}
}

func TestRegisterClosedModeRedirectDoesNotLeakEmailOrErrorInQuery(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	form := url.Values{
		"email":            {"closed-mode@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	location := strings.TrimSpace(response.Header.Get("Location"))
	if location != "/register" {
		t.Fatalf("expected redirect to /register, got %q", location)
	}
	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue == "" {
		t.Fatalf("expected flash cookie for registration disabled redirect")
	}
}

func TestRegisterPageClosedModeRendersDisabledStateWithoutForm(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, "Registration is disabled after initial setup.") {
		t.Fatalf("expected registration disabled copy in register page")
	}
	if !strings.Contains(rendered, "data-registration-disabled") {
		t.Fatalf("expected disabled register state marker")
	}
	if strings.Contains(rendered, `id="register-form"`) {
		t.Fatalf("did not expect register form in closed mode")
	}
}

func TestLoginPageClosedModeHidesSignupCTA(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "data-auth-signup-cta") {
		t.Fatalf("did not expect signup CTA in closed registration mode")
	}
}
