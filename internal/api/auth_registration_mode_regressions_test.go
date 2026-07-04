package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

func TestRegisterClosedModeReturnsForbiddenJSONError(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	form := url.Values{
		"email":            {"closed-mode@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlAuthErrorByKey(document, "auth.error.registration_disabled") == nil {
		t.Fatal("expected register page to render auth.error.registration_disabled banner")
	}
	if htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-registration-disabled")
	}) == nil {
		t.Fatal("expected disabled register state marker")
	}
	if htmlElementByID(document, "register-form") != nil {
		t.Fatal("did not expect register form in closed mode")
	}
}

func TestLoginPageClosedModeHidesSignupCTA(t *testing.T) {
	app, _ := newOnboardingTestAppWithRegistrationMode(t, services.RegistrationModeClosed)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
