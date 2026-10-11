package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestCSRFRefusalAnswersUnderItsOwnKey pins the contract a client relies on to
// tell a stale token from a forbidden account: the CSRF check's 403 carries the
// `csrf_refused` key on the JSON envelope and on the HTMX fragment, while a 403
// raised by anything else keeps the shared `forbidden` key. Both keep the
// `forbidden` category, so a client that branches on status plus category is
// unaffected by the new key.
func TestCSRFRefusalAnswersUnderItsOwnKey(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{enableCSRF: true, envelopeTransportErrors: true})

	send := func(t *testing.T, path string, headers map[string]string) (int, string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(""))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := mustAppResponse(t, app, request)
		return response.StatusCode, mustReadBodyString(t, response.Body)
	}

	t.Run("json envelope", func(t *testing.T) {
		status, body := send(t, "/api/v1/sessions", map[string]string{"Accept": "application/json"})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
		var envelope struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Key      string `json:"key"`
				Category string `json:"category"`
			} `json:"error_detail"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err != nil {
			t.Fatalf("not the envelope: %q: %v", body, err)
		}
		if envelope.Error != "csrf_refused" || envelope.ErrorDetail.Key != "csrf_refused" || envelope.ErrorDetail.Category != "forbidden" {
			t.Fatalf("CSRF refusal envelope = %+v, want key csrf_refused under category forbidden", envelope)
		}
	})

	t.Run("htmx fragment", func(t *testing.T) {
		status, body := send(t, "/api/v1/sessions", map[string]string{"HX-Request": "true"})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
		if !strings.Contains(body, `data-flash-key="common.error.csrf_refused"`) || strings.Contains(body, "common.error.forbidden") {
			t.Fatalf("the HTMX fragment must carry the CSRF key and not the shared one, got %q", body)
		}
		if !strings.Contains(body, englishCopy(t, "common.error.csrf_refused")) {
			t.Fatalf("the HTMX fragment must carry the CSRF copy, got %q", body)
		}
	})

	t.Run("a 403 from another guard keeps the shared key", func(t *testing.T) {
		other := fiber.New()
		handler := &Handler{}
		other.Get("/probe", func(c fiber.Ctx) error { return handler.RespondTransportError(c, fiber.StatusForbidden) })
		request := httptest.NewRequest(http.MethodGet, "/probe", nil)
		request.Header.Set("Accept", "application/json")
		response := mustAppResponse(t, other, request)
		if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"forbidden"`) {
			t.Fatalf("a non-CSRF 403 must keep the shared key, got %q", body)
		}
	})
}

// TestCSRFRefusalErrorStaysAFiberForbidden holds the fallback: an error handler
// that does not know ErrCSRFRefused still answers a 403 rather than a 500.
func TestCSRFRefusalErrorStaysAFiberForbidden(t *testing.T) {
	t.Parallel()

	var fiberErr *fiber.Error
	if !errors.As(ErrCSRFRefused, &fiberErr) || fiberErr.Code != fiber.StatusForbidden || fiberErr.Message != "Forbidden" {
		t.Fatalf("ErrCSRFRefused = %v, want the framework's own 403", ErrCSRFRefused)
	}
}
