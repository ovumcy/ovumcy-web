package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
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
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
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
	if payload.ForgotEmail != "" {
		t.Fatalf("expected no email PII in register flash payload, got %#v", payload)
	}
}

func TestRespondMappedErrorSettingsFormRedirectsWithFlashOnly(t *testing.T) {
	t.Parallel()

	app, handler := newErrorMappingTransportTestApp(t)

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader("display_name="))
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

// bodyLimitGuardTestLimit keeps the compressed probes small: a payload crossing
// it gzips to a couple of hundred bytes, so the wire cap is never the thing
// under test.
const bodyLimitGuardTestLimit = 1024

func gzipTestBody(t *testing.T, payload []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("gzip payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return compressed.Bytes()
}

// TestFiberSignalsDecompressedOverflowByStampingTheResponseStatus pins the
// framework seam requestBodyLimitGuard is built on, because that seam is the
// whole reason the guard exists and nothing else in the repo would notice it
// moving.
//
// Fiber v3 decodes a Content-Encoding body lazily inside its body accessor and
// applies the configured BodyLimit to the DECODED stream. The accessor returns
// no error: it swallows fasthttp.ErrBodyTooLarge, stamps 413 on the response,
// and hands the caller the error's text in place of the payload. So the only
// observable signal is the stamped status — and the substituted text is what
// used to reach the import service. If a fiber upgrade changes either half
// (returns the real prefix, stops stamping, moves the enforcement point), this
// test fails and the guard must be re-derived rather than silently becoming a
// no-op or a false positive.
func TestFiberSignalsDecompressedOverflowByStampingTheResponseStatus(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{BodyLimit: bodyLimitGuardTestLimit})

	var observedBody string
	var observedStatus int
	app.Post("/probe", func(c fiber.Ctx) error {
		observedBody = string(c.Body())
		observedStatus = c.Response().StatusCode()
		return c.SendStatus(fiber.StatusTeapot)
	})

	marker := strings.Repeat("payload-", 4)
	payload := bytes.Repeat([]byte(marker), 1+bodyLimitGuardTestLimit/len(marker))
	request := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewReader(gzipTestBody(t, payload)))
	request.Header.Set("Content-Encoding", "gzip")

	assertStatusCode(t, mustAppResponse(t, app, request), fiber.StatusTeapot)

	if observedStatus != fiber.StatusRequestEntityTooLarge {
		t.Fatalf("expected fiber to stamp 413 on the response while decoding an over-limit body, got %d", observedStatus)
	}
	if strings.Contains(observedBody, marker) {
		t.Fatalf("expected none of the decoded payload to reach the handler, got %q", observedBody)
	}
}

// TestRequestBodyLimitGuardRejectsOnlyTheOverflowStamp pins the guard's three
// compressed-body branches on one app: an over-limit decoded body is answered
// with the mapped 413 and never reaches the route (previously a route that read
// the body without writing a response returned fiber's bare-text 413, which the
// app-wide envelope contract forbids); a body inside the cap reaches the route
// decoded; and an encoding fiber cannot decode is left exactly as it was, with
// no trace of the guard's probe on the response.
func TestRequestBodyLimitGuardRejectsOnlyTheOverflowStamp(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{BodyLimit: bodyLimitGuardTestLimit})
	app.Use(requestBodyLimitGuard)

	routeReached := false
	app.Post("/probe", func(c fiber.Ctx) error {
		routeReached = true
		return c.SendString("route saw " + strconv.Itoa(len(c.Body())) + " bytes")
	})
	// A route that reads the body and writes nothing of its own: whatever the
	// framework stamped while decoding stands, which is how the bare-text 413
	// used to escape.
	app.Post("/passive", func(c fiber.Ctx) error {
		routeReached = true
		_ = c.Body()
		return nil
	})

	t.Run("over the decoded cap", func(t *testing.T) {
		for _, path := range []string{"/probe", "/passive"} {
			routeReached = false
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(gzipTestBody(t, bytes.Repeat([]byte("a"), bodyLimitGuardTestLimit+1))))
			request.Header.Set("Content-Encoding", "gzip")
			request.Header.Set("Accept", "application/json")

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, fiber.StatusRequestEntityTooLarge)
			if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"request_too_large"`) {
				t.Fatalf("%s: expected the mapped request_too_large envelope, got %q", path, body)
			}
			if routeReached {
				t.Fatalf("%s: expected the over-limit request to be rejected before the route ran", path)
			}
		}
	})

	t.Run("inside the decoded cap", func(t *testing.T) {
		routeReached = false
		request := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewReader(gzipTestBody(t, bytes.Repeat([]byte("a"), bodyLimitGuardTestLimit))))
		request.Header.Set("Content-Encoding", "gzip")

		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, fiber.StatusOK)
		if !routeReached {
			t.Fatal("expected a body inside the cap to reach the route")
		}
		if body := mustReadBodyString(t, response.Body); body != "route saw "+strconv.Itoa(bodyLimitGuardTestLimit)+" bytes" {
			t.Fatalf("expected the route to receive the fully decoded body, got %q", body)
		}
	})

	t.Run("undecodable encoding is left to the route", func(t *testing.T) {
		routeReached = false
		request := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader("whatever"))
		request.Header.Set("Content-Encoding", "not-an-encoding")

		response := mustAppResponse(t, app, request)
		if !routeReached {
			t.Fatal("expected an undecodable encoding to still reach the route")
		}
		// The route's own body read re-raises fiber's 415; what matters is that
		// the guard's probe added nothing of its own on the way there.
		assertStatusCode(t, response, fiber.StatusUnsupportedMediaType)
	})
}

func newErrorMappingTransportTestApp(t *testing.T) (*fiber.App, *Handler) {
	t.Helper()

	handler := &Handler{secretKey: []byte("test-error-mapping-secret")}
	app := fiber.New()

	app.Get("/api/test/global", func(c fiber.Ctx) error {
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"))
	})
	app.Get("/api/test/htmx", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, map[string]string{"not found": "Localized not found."})
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found"))
	})
	app.Post("/api/v1/users", func(c fiber.Ctx) error {
		return handler.respondMappedError(c, authFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"))
	})
	app.Patch("/api/v1/users/current/profile", func(c fiber.Ctx) error {
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
