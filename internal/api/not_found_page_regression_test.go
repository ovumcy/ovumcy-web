package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotFoundPageForGuestUsesLoginPrimaryAction(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/missing-page", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("not-found page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read not-found page body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, "Page not found") {
		t.Fatalf("expected localized not-found title")
	}
	if !strings.Contains(rendered, `href="/login"`) {
		t.Fatalf("expected login primary action for guest not-found page")
	}
}

func TestNotFoundPageForAuthenticatedUserUsesDashboardPrimaryAction(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "not-found-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/missing-owner-page", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("authenticated not-found page request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read authenticated not-found page body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `href="/dashboard"`) {
		t.Fatalf("expected dashboard primary action for authenticated not-found page")
	}
	if !strings.Contains(rendered, "not-found-owner") {
		t.Fatalf("expected authenticated nav identity in not-found page layout")
	}
}

func TestNotFoundAPIPathReturnsJSONError(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/missing-endpoint", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("not-found api request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}

	errorMessage := readAPIError(t, response.Body)
	if errorMessage != "not found" {
		t.Fatalf("expected not found api error, got %q", errorMessage)
	}
}

func TestNotFoundHTMXPathReturnsLocalizedStatusErrorMarkup(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/missing-fragment", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "ru")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("not-found htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read htmx not-found body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `class="status-error"`) {
		t.Fatalf("expected shared status-error markup, got %q", rendered)
	}
	if !strings.Contains(rendered, "Страница не найдена") {
		t.Fatalf("expected localized not-found htmx message, got %q", rendered)
	}
	if strings.Contains(rendered, "<html") {
		t.Fatalf("expected htmx branch to avoid full page markup, got %q", rendered)
	}
}
