package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRespondMappedErrorGlobalJSONReturnsStableErrorPayload(t *testing.T) {
	t.Parallel()

	app, _ := newErrorMappingTransportTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/test/global", nil)
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid input" {
		t.Fatalf("expected invalid input error payload, got %q", got)
	}
}

func TestRespondMappedErrorGlobalHTMXReturnsLocalizedStatusMarkup(t *testing.T) {
	t.Parallel()

	app, _ := newErrorMappingTransportTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/test/htmx", nil)
	request.Header.Set("HX-Request", "true")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusNotFound)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `class="status-error"`, message: "expected shared status-error wrapper for HTMX errors"},
		bodyStringMatch{fragment: "Localized not found.", message: "expected HTMX branch to localize mapped error text"},
	)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: "<html", message: "did not expect full-page markup in HTMX mapped error response"},
	)
}

func TestRespondMappedErrorAuthFormRedirectsWithFlashOnly(t *testing.T) {
	t.Parallel()

	app, handler := newErrorMappingTransportTestApp(t)

	form := url.Values{"email": {"MixedCase@Example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	location := mustParseLocationHeader(t, response)
	if location.Path != "/register" {
		t.Fatalf("expected auth form redirect to /register, got %q", location.Path)
	}
	if strings.TrimSpace(location.RawQuery) != "" {
		t.Fatalf("expected auth redirect without query params, got %q", location.RawQuery)
	}

	payload := mustReadFlashPayload(t, handler.secretKey, response.Cookies())
	if payload.AuthError != "weak password" {
		t.Fatalf("expected auth flash error, got %#v", payload)
	}
	if payload.RegisterEmail != "mixedcase@example.com" {
		t.Fatalf("expected normalized register email in flash payload, got %#v", payload)
	}
}

func TestRespondMappedErrorSettingsFormRedirectsWithFlashOnly(t *testing.T) {
	t.Parallel()

	app, handler := newErrorMappingTransportTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader("display_name="))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	location := mustParseLocationHeader(t, response)
	if location.Path != "/settings" {
		t.Fatalf("expected settings form redirect to /settings, got %q", location.Path)
	}
	if strings.TrimSpace(location.RawQuery) != "" {
		t.Fatalf("expected settings redirect without query params, got %q", location.RawQuery)
	}

	payload := mustReadFlashPayload(t, handler.secretKey, response.Cookies())
	if payload.SettingsError != "invalid settings input" {
		t.Fatalf("expected settings flash error, got %#v", payload)
	}
	if payload.AuthError != "" || payload.SettingsSuccess != "" {
		t.Fatalf("expected only settings error in flash payload, got %#v", payload)
	}
}

func newErrorMappingTransportTestApp(t *testing.T) (*fiber.App, *Handler) {
	t.Helper()

	handler := &Handler{secretKey: []byte("test-error-mapping-secret")}
	app := fiber.New()

	app.Get("/api/test/global", func(c *fiber.Ctx) error {
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"))
	})
	app.Get("/api/test/htmx", func(c *fiber.Ctx) error {
		c.Locals(contextMessagesKey, map[string]string{"not found": "Localized not found."})
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found"))
	})
	app.Post("/api/auth/register", func(c *fiber.Ctx) error {
		return handler.respondMappedError(c, authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"))
	})
	app.Post("/api/settings/profile", func(c *fiber.Ctx) error {
		return handler.respondMappedError(c, settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid settings input"))
	})

	return app, handler
}

func mustReadFlashPayload(t *testing.T, secretKey []byte, cookies []*http.Cookie) FlashPayload {
	t.Helper()

	rawValue := responseCookieValue(cookies, flashCookieName)
	if rawValue == "" {
		t.Fatal("expected flash cookie in response")
	}

	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("create secure cookie codec: %v", err)
	}

	decoded, err := codec.open(flashCookieName, rawValue)
	if err != nil {
		t.Fatalf("open flash cookie: %v", err)
	}

	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("decode flash cookie payload: %v", err)
	}
	return payload
}
