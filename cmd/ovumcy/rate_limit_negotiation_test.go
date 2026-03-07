package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/i18n"
)

func newRateLimitTestI18nManager(t *testing.T) *i18n.Manager {
	t.Helper()

	candidates := []string{
		filepath.Join("..", "..", "internal", "i18n", "locales"),
		filepath.Join("internal", "i18n", "locales"),
	}
	for _, candidate := range candidates {
		manager, err := i18n.NewManager("en", candidate)
		if err == nil {
			return manager
		}
	}
	t.Fatal("failed to initialize i18n manager for rate-limit tests")
	return nil
}

func TestAuthRateLimitHandlerTreatsJSONContentTypeAsJSONRequest(t *testing.T) {
	manager := newRateLimitTestI18nManager(t)
	app := fiber.New()
	app.Post("/limited", newAuthRateLimitHandler(manager, authRateLimitConfig{
		RedirectPath: "/login",
		ErrorCode:    "too_many_login_attempts",
		MessageKey:   "auth.error.too_many_login_attempts",
	}, false, []byte("test-secret-key-with-32-char-minimum")))

	request := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader(`{"email":"rate-limit@example.com"}`))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("auth rate-limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("expected json error payload, got %#v", payload)
	}
}

func TestAPIRateLimitHandlerReturnsStatusErrorMarkupForHTMX(t *testing.T) {
	manager := newRateLimitTestI18nManager(t)
	app := fiber.New()
	app.Post("/limited", newAPIRateLimitHandler(manager))

	request := httptest.NewRequest(http.MethodPost, "/limited", nil)
	request.Header.Set("HX-Request", "true")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("api rate-limit htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `class="status-error"`) {
		t.Fatalf("expected status-error markup, got %q", rendered)
	}
}
