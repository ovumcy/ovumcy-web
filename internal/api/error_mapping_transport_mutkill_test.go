package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Mutation-kill tests for the localized-status substitution guards in
// error_mapping_transport.go. Both apiError (L22) and respondSettingsError
// (L119) resolve an auth/settings error key to its i18n key, then substitute
// the localized string ONLY when translateMessage returns something different
// from the key (`localized != key`). Negating that guard (CONDITIONALS_NEGATION)
// leaves the raw spec.Key in the rendered status-error body instead of the
// localized text. A synthetic catalog (never real i18n copy) is injected so the
// assertion pins the substitution logic, not a copy string.

// TestApiErrorHTMXSubstitutesLocalizedAuthMessage pins
// error_mapping_transport.go:22. "invalid input" maps to
// "auth.error.invalid_input"; with that key present in the catalog, the HTMX
// status body must render the localized value. Negating `localized != key`
// leaves the bare "invalid input" spec key in the body.
func TestApiErrorHTMXSubstitutesLocalizedAuthMessage(t *testing.T) {
	app := fiber.New()
	app.Get("/apierr", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, map[string]string{"auth.error.invalid_input": "SENTINEL_AUTH_LOCALIZED"})
		return apiError(c, globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"))
	})

	req := httptest.NewRequest(http.MethodGet, "/apierr", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("apiError HTMX probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("apiError HTMX status = %d, want 400", resp.StatusCode)
	}

	body := mustReadBodyString(t, resp.Body)
	if !strings.Contains(body, "SENTINEL_AUTH_LOCALIZED") {
		t.Fatalf("HTMX status body must render the localized auth message, got %q", body)
	}
	// The bare spec key must NOT leak as the visible message when a translation exists.
	if strings.Contains(body, ">invalid input<") {
		t.Fatalf("HTMX status body must not render the raw spec key as the message, got %q", body)
	}
	// data-flash-key exposes the resolved i18n key regardless of substitution.
	if !strings.Contains(body, `data-flash-key="auth.error.invalid_input"`) {
		t.Fatalf("expected resolved auth flash key attribute, got %q", body)
	}
}

// TestRespondSettingsErrorHTMXSubstitutesLocalizedMessage pins
// error_mapping_transport.go:119. "invalid settings input" maps to
// "settings.error.invalid_input"; the HTMX status body must render the
// localized value present in the catalog. Negating `localized != key` leaves the
// raw spec key.
func TestRespondSettingsErrorHTMXSubstitutesLocalizedMessage(t *testing.T) {
	handler := &Handler{}
	app := fiber.New()
	app.Post("/setterr", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, map[string]string{"settings.error.invalid_input": "SENTINEL_SETTINGS_LOCALIZED"})
		return handler.respondSettingsError(c, settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid settings input"))
	})

	req := httptest.NewRequest(http.MethodPost, "/setterr", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("respondSettingsError HTMX probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// HTMX settings errors deliberately return 200 with the status-error partial.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("respondSettingsError HTMX status = %d, want 200", resp.StatusCode)
	}

	body := mustReadBodyString(t, resp.Body)
	if !strings.Contains(body, "SENTINEL_SETTINGS_LOCALIZED") {
		t.Fatalf("HTMX settings status body must render the localized message, got %q", body)
	}
	if strings.Contains(body, ">invalid settings input<") {
		t.Fatalf("HTMX settings status body must not render the raw spec key, got %q", body)
	}
	if !strings.Contains(body, `data-flash-key="settings.error.invalid_input"`) {
		t.Fatalf("expected resolved settings flash key attribute, got %q", body)
	}
}
