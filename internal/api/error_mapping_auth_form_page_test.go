package api

import (
	"html/template"
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

// newRefusalPageTemplates parses the real refusal page into the shared layout,
// for a test handler that renders it rather than the bare-fragment fallback
// sendPageFormRefusalPage takes when the page is missing.
func newRefusalPageTemplates(t *testing.T) map[string]*template.Template {
	t.Helper()
	parsed, err := parsePageTemplates(newTemplateFuncMap(), []string{pageFormRefusalTemplate})
	if err != nil {
		t.Fatalf("parse the refusal page: %v", err)
	}
	return parsed
}

// requireAuthFormRefusalPage reads a plain auth-form refusal the way a browser
// does (WEB-264): a whole page in the shared layout with <html lang="en">, the
// layout's main landmark, and the refusal's own section holding the shared
// status markup and exactly one link, back to the form. The signed-out header
// links to /login itself, so the back link is looked for inside the section.
func requireAuthFormRefusalPage(t *testing.T, where string, body string, back string) {
	t.Helper()
	if !strings.Contains(body, `<html lang="en"`) || !strings.Contains(body, `id="main-content"`) {
		t.Fatalf("%s: expected the refusal page in the shared layout, got %q", where, body)
	}
	_, section, found := strings.Cut(body, "data-page-form-refusal")
	section, _, closed := strings.Cut(section, "</section>")
	if !found || !closed || !strings.Contains(section, `class="status-error"`) {
		t.Fatalf("%s: the refusal page lacks its status section: %q", where, body)
	}
	if got := strings.Count(section, "href="); got != 1 {
		t.Fatalf("%s: the refusal section has %d links, want exactly the one back link: %q", where, got, section)
	}
	if want := `<a href="` + back + `"`; !strings.Contains(section, want) {
		t.Fatalf("%s: expected the fixed back link %s, got %q", where, want, section)
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
// domain spec. The answer is the refusal page in the shared layout (WEB-264),
// so the handler here carries the real templates.
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
	handler := &Handler{i18n: newRateLimitResponderTestI18n(t), templates: newRefusalPageTemplates(t)}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
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
				t.Fatalf("status %d: expected the page-form refusal page, got the raw JSON envelope: %q", status, body)
			}
			contentType := response.Header.Get(fiber.HeaderContentType)
			if !strings.HasPrefix(contentType, fiber.MIMETextHTML) {
				t.Fatalf("status %d: expected text/html, got %q (%q)", status, contentType, body)
			}
			requireAuthFormRefusalPage(t, http.StatusText(status), body, "/login")
			if cookies := response.Header.Values(fiber.HeaderSetCookie); len(cookies) != 0 {
				t.Fatalf("status %d: the refusal page set cookies %v", status, cookies)
			}
		})
	}
}

// TestHandlerLayer500OnAPlainAuthFormPageAnswersTheFragment closes the other
// half of the coverage gap this fix round flagged: the branch does not only
// catch a transport-level rejection raised before any handler runs — it also
// catches a handler-layer 500 the handler itself builds, because that spec's
// Target is the default APIErrorTargetGlobal rather than APIErrorTargetAuthForm,
// so respondMappedError never routes it through respondAuthError's
// flash-redirect and it falls straight through to apiError like any other
// rejection on the route. authSessionCreateErrorSpec (key "failed to create
// session") is exercised here as the representative: it is what
// handlers_auth_session_login.go and handlers_auth_2fa.go answer when
// setAuthCookie/setTOTPPendingCookie fails.
//
// A plain browser Accept must get the refusal page with its fixed back link,
// never the raw JSON envelope; a caller whose Accept names application/json
// must keep the JSON envelope unchanged (WEB-84 leaves JSON clients untouched
// even for this route's own internal errors).
func TestHandlerLayer500OnAPlainAuthFormPageAnswersTheFragment(t *testing.T) {
	handler := &Handler{i18n: newRateLimitResponderTestI18n(t), templates: newRefusalPageTemplates(t)}
	app := fiber.New()
	app.Post("/api/v1/sessions", func(c fiber.Ctx) error {
		return handler.respondMappedError(c, authSessionCreateErrorSpec())
	})

	t.Run("browser accept", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(""))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "text/html,application/xhtml+xml")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", response.StatusCode)
		}
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body := string(bodyBytes)
		if strings.HasPrefix(strings.TrimSpace(body), "{") {
			t.Fatalf("handler-layer 500: painted the raw JSON envelope into the browser, got %q", body)
		}
		contentType := response.Header.Get(fiber.HeaderContentType)
		if !strings.HasPrefix(contentType, fiber.MIMETextHTML) {
			t.Fatalf("handler-layer 500: expected text/html, got %q (%q)", contentType, body)
		}
		requireAuthFormRefusalPage(t, "handler-layer 500", body, "/login")
	})

	t.Run("json client", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(""))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", response.StatusCode)
		}
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(bodyBytes)), "{") {
			t.Fatalf("json client: expected the JSON envelope, got %q", bodyBytes)
		}
		if !strings.Contains(string(bodyBytes), `"failed to create session"`) {
			t.Fatalf("json client: expected the envelope to carry the spec's own key, got %q", bodyBytes)
		}
	})
}
