package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func submitOnboardingStep1(t *testing.T, app *fiber.App, authCookie string, form url.Values) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/step1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("step1 request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected step1 status 204, got %d", response.StatusCode)
	}
}

func submitOnboardingStep2(t *testing.T, app *fiber.App, authCookie string, form url.Values) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("step2 request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected step2 status 204, got %d", response.StatusCode)
	}
}

func submitOnboardingComplete(t *testing.T, app *fiber.App, authCookie string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/complete", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("step3 request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected step3 status 200, got %d", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); redirect != "/dashboard" {
		t.Fatalf("expected HX-Redirect /dashboard, got %q", redirect)
	}
}
