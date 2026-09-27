package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestPlainAuthFormPagePathsAllHaveAFixedBackLink pins WEB-84's back-link fix:
// plainAuthFormPageBackPaths (the fixed, server-owned back link a plain
// auth-form refusal renders) must cover exactly the same route set as
// plainAuthFormPagePaths (the routes that take the page-form fragment at
// all), in both directions — a route added to one without the other either
// silently falls back to "/" (plainAuthFormPageBackPath's defensive default)
// or defines a back link for a route that never renders the fragment.
func TestPlainAuthFormPagePathsAllHaveAFixedBackLink(t *testing.T) {
	for path := range plainAuthFormPagePaths {
		back, ok := plainAuthFormPageBackPaths[path]
		if !ok {
			t.Fatalf("%s: takes the plain auth-form fragment but has no fixed back link in plainAuthFormPageBackPaths", path)
		}
		if back == "" {
			t.Fatalf("%s: fixed back link is empty", path)
		}
		if got := plainAuthFormPageBackPath(path); got != back {
			t.Fatalf("%s: plainAuthFormPageBackPath returned %q, want %q", path, got, back)
		}
	}
	for path := range plainAuthFormPageBackPaths {
		if _, ok := plainAuthFormPagePaths[path]; !ok {
			t.Fatalf("%s: has a fixed back link but is not a member of plainAuthFormPagePaths", path)
		}
	}
}

// TestPlainAuthFormPageBackPathFallsBackToRootForAnUnmappedPath pins the
// defensive default plainAuthFormPageBackPath documents: a path outside the
// fixed mapping (unreachable in production, since isPlainAuthFormPageNavigation
// only admits mapped paths) still returns a safe same-origin path rather than
// panicking or returning something request-derived.
func TestPlainAuthFormPageBackPathFallsBackToRootForAnUnmappedPath(t *testing.T) {
	if got := plainAuthFormPageBackPath("/not-a-real-route"); got != "/" {
		t.Fatalf("expected the defensive fallback %q, got %q", "/", got)
	}
}

// TestEveryTransportStatusOnAPlainAuthFormPageAnswersTheFragment closes the
// coverage gap the WEB-84 fix round flagged: apiError's
// isPlainAuthFormPageNavigation branch is checked before the spec's status is
// ever inspected, so it carries EVERY apiError call these routes reach for a
// plain HTML client, not only CSRF's 403 — the previous round tested 403
// alone. 413 (RespondRequestEntityTooLarge) and 503 (RespondRequestTimeout)
// reach apiError directly, the same as CSRF's 403 does through
// RespondTransportError, and are exercised here through RespondTransportError
// for the same reason CSRF is: both are raised before any handler builds a
// domain spec.
//
// 429 is included for the same reason, but note what it does NOT cover: the
// app's own login/registration/recovery rate limiters answer through
// RespondAuthRateLimited, whose spec carries Target APIErrorTargetAuthForm, so
// respondMappedError dispatches it through respondAuthError's flash-redirect
// for a plain HTML client — apiError is never called, and this branch never
// fires there. TestRespondAuthRateLimitedFallsBackThroughAuthFlash pins that
// redirect unchanged; a bare 429 landing here is the total-mapping fallback
// (transportErrorSpecsByStatus) for a 429 raised by anything else — a
// hypothetical future limiter that has not (yet) been given its own domain
// spec, or an upstream middleware's raw *fiber.Error.
func TestEveryTransportStatusOnAPlainAuthFormPageAnswersTheFragment(t *testing.T) {
	statuses := []int{
		fiber.StatusForbidden,
		fiber.StatusRequestEntityTooLarge,
		fiber.StatusTooManyRequests,
		fiber.StatusServiceUnavailable,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := &Handler{i18n: newRateLimitResponderTestI18n(t)}
			app := fiber.New()
			app.Post("/api/v1/sessions", func(c fiber.Ctx) error {
				return handler.RespondTransportError(c, status)
			})

			request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(""))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "text/html,application/xhtml+xml")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != status {
				t.Fatalf("status = %d, want %d", response.StatusCode, status)
			}
			bodyBytes, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			body := string(bodyBytes)
			if strings.HasPrefix(strings.TrimSpace(body), "{") {
				t.Fatalf("status %d: expected the page-form fragment, got the raw JSON envelope: %q", status, body)
			}
			contentType := response.Header.Get(fiber.HeaderContentType)
			if !strings.HasPrefix(contentType, fiber.MIMETextHTML) || !strings.Contains(body, `class="status-error"`) {
				t.Fatalf("status %d: expected the shared page-form status fragment, got %q (%q)", status, contentType, body)
			}
			if want := `<a href="/login">`; !strings.Contains(body, want) {
				t.Fatalf("status %d: expected the fixed back link to /login, got %q", status, body)
			}
		})
	}
}
