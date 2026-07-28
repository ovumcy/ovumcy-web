package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The mapped error envelope is an APP-WIDE contract: every rejection the app
// emits carries {error, error_detail} with a stable key, negotiated into the
// shared status fragment for the browser flows. It used to hold for exactly
// three statuses — the two pre-routing rejections (413, 431) and the
// request-budget 503 — while every other explicit *fiber.Error fell through to
// `c.Status(code).SendString(fiberErr.Message)`, i.e. the framework's bare
// English regardless of what the client asked for. That covered the CSRF 403
// (every mutating route), the `POST /lang` 400, the OIDC logout-bridge 400, the
// calendar-feed 500, and anything fiber raises for a request it cannot route.
//
// The tests below are the class sweep for that defect, not a fix for its four
// known instances: the first walks statuses rather than call sites, so a status
// nothing raises today is covered the day something starts raising it.

// transportEnvelopeExpectation is the enveloped answer one status must produce.
type transportEnvelopeExpectation struct {
	key      string
	category string
}

// transportEnvelopeStatuses enumerates the statuses an explicit *fiber.Error can
// carry. Three of them deliberately share a spec defined elsewhere, so one
// status keeps one key no matter which layer produced it: 401 with the auth
// guard, 404 with the route-level not-found, 429 with the rate limiters.
var transportEnvelopeStatuses = map[int]transportEnvelopeExpectation{
	fiber.StatusBadRequest:                  {key: "bad_request", category: "validation"},
	fiber.StatusUnauthorized:                {key: "unauthorized", category: "unauthorized"},
	fiber.StatusForbidden:                   {key: "forbidden", category: "forbidden"},
	fiber.StatusNotFound:                    {key: "not found", category: "not_found"},
	fiber.StatusMethodNotAllowed:            {key: "method_not_allowed", category: "validation"},
	fiber.StatusRequestEntityTooLarge:       {key: "request_too_large", category: "too_large"},
	fiber.StatusUnsupportedMediaType:        {key: "unsupported_media_type", category: "validation"},
	fiber.StatusTooManyRequests:             {key: "too many requests", category: "rate_limited"},
	fiber.StatusRequestHeaderFieldsTooLarge: {key: "request_headers_too_large", category: "too_large"},
	fiber.StatusInternalServerError:         {key: "internal_error", category: "internal"},
	fiber.StatusServiceUnavailable:          {key: "service_unavailable", category: "internal"},
	// Not in the mapped table: an unlisted status must still be enveloped, from
	// its class rather than from fiber's text. 418 and 507 are chosen because
	// nothing in the app raises them, so only the fallback can answer them.
	fiber.StatusTeapot:              {key: "request_rejected", category: "validation"},
	fiber.StatusInsufficientStorage: {key: "internal_error", category: "internal"},
}

// TestOvumcyErrorHandlerEnvelopesEveryFiberErrorStatus is the no-allowlist half
// of the contract: it drives the real top-level handler with one explicit
// *fiber.Error per status and requires the mapped envelope every time. A status
// added to the mapped table later inherits the assertion by being named here;
// one that is never added is still covered by the two fallback rows.
func TestOvumcyErrorHandlerEnvelopesEveryFiberErrorStatus(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ovumcyErrorHandler})
	app.Get("/probe/:status", func(c fiber.Ctx) error {
		status, err := strconv.Atoi(c.Params("status"))
		if err != nil {
			return err
		}
		// A message no client may ever see, standing in for the internal detail a
		// wrapped error can carry.
		return fiber.NewError(status, "ovumcy-internal-detail-marker")
	})

	for status, want := range transportEnvelopeStatuses {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/probe/"+strconv.Itoa(status), nil)
			request.Header.Set("Accept", "application/json")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("probe %d: %v", status, err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != status {
				t.Fatalf("status = %d, want %d — the handler must preserve an explicit fiber error's status", response.StatusCode, status)
			}
			body := mustReadAll(t, response)
			if strings.Contains(string(body), "ovumcy-internal-detail-marker") {
				t.Fatalf("the fiber error's message reached the response body: %q", body)
			}
			if bare := http.StatusText(status); bare != "" && strings.TrimSpace(string(body)) == bare {
				t.Fatalf("status %d answered with the framework's bare text %q; the envelope is app-wide", status, body)
			}
			assertTransportErrorEnvelope(t, body, want.key, want.category)
		})
	}
}

// TestCSRFDenialAnswersThroughTheEnvelope covers the widest instance of the
// defect: the CSRF middleware answers `fiber.ErrForbidden` for every mutating
// route, so before this change every CSRF refusal in the app — API client and
// browser alike — was the bare string "Forbidden".
//
// The HTMX arm is the browser-facing half: the app's own forms submit through
// HTMX, so an expired token there must render the shared status-error fragment
// with a stable key rather than dumping framework text into the page.
func TestCSRFDenialAnswersThroughTheEnvelope(t *testing.T) {
	app := newCSRFGuardTestApp(t)

	t.Run("json client", func(t *testing.T) {
		response := deleteSessionWithoutCSRFToken(t, app, map[string]string{"Accept": "application/json"})
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", response.StatusCode)
		}
		body := mustReadAll(t, response)
		if strings.TrimSpace(string(body)) == "Forbidden" {
			t.Fatalf("csrf denial answered with fiber's bare text: %q", body)
		}
		assertTransportErrorEnvelope(t, body, "forbidden", "forbidden")
	})

	// A plain browser form post (no HX-Request, Accept: text/html) shares the
	// non-HTMX branch of the shared negotiation with the JSON client — the same
	// branch the mapped 413 and 431 have always used. What this change moves is
	// that it is now the app's own enveloped answer with a stable key rather than
	// fiber's bare English.
	t.Run("browser accept", func(t *testing.T) {
		response := deleteSessionWithoutCSRFToken(t, app, map[string]string{"Accept": "text/html,application/xhtml+xml"})
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", response.StatusCode)
		}
		body := mustReadAll(t, response)
		if strings.TrimSpace(string(body)) == "Forbidden" {
			t.Fatalf("csrf denial answered with fiber's bare text: %q", body)
		}
		assertTransportErrorEnvelope(t, body, "forbidden", "forbidden")
	})

	t.Run("htmx flow", func(t *testing.T) {
		response := deleteSessionWithoutCSRFToken(t, app, map[string]string{"HX-Request": "true"})
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", response.StatusCode)
		}
		body := string(mustReadAll(t, response))
		if strings.TrimSpace(body) == "Forbidden" {
			t.Fatalf("csrf denial answered with fiber's bare text: %q", body)
		}
		if !strings.Contains(body, `class="status-error"`) {
			t.Fatalf("expected the shared status-error fragment for an HTMX flow, got %q", body)
		}
		// The stable key rides next to the localized copy, so the assertion never
		// has to pin the copy itself.
		if !strings.Contains(body, `data-flash-key="common.error.forbidden"`) {
			t.Fatalf("expected the stable flash key on the HTMX csrf fragment, got %q", body)
		}
	})
}

// TestUnmatchedRouteAnswersThroughTheEnvelope pins the request the framework
// itself rejects. The composition root's NotFound catch-all is what keeps this
// out of fiber's own 404 text; the assertion is that a path nobody registered
// answers in the app's format, whichever layer ends up producing it — so
// removing the catch-all cannot silently restore "Cannot GET /...".
func TestUnmatchedRouteAnswersThroughTheEnvelope(t *testing.T) {
	app := newCSRFGuardTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/no-such-route-exists", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unmatched route request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.StatusCode)
	}
	body := mustReadAll(t, response)
	if strings.Contains(string(body), "Cannot GET") {
		t.Fatalf("unmatched route answered with fiber's bare text: %q", body)
	}
	assertTransportErrorEnvelope(t, body, "not found", "not_found")
}

// TestLanguageSwitchRejectionAnswersThroughTheEnvelope covers the one app
// handler that returns a naked fiber sentinel on a validation failure. It is a
// public, unauthenticated route reachable from every page's language switcher,
// so its rejection was the bare "Bad Request" for anyone who submitted the form
// without a language.
func TestLanguageSwitchRejectionAnswersThroughTheEnvelope(t *testing.T) {
	app := newCSRFGuardTestApp(t)
	token, cookie := issueCSRFFormCredentials(t, app)

	form := url.Values{"csrf_token": {token}, "lang": {"   "}}
	request := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", cookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("language switch request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	// 400 rather than 403 is itself part of the assertion: it proves the request
	// carried a valid token and reached the handler, so the rejection under test
	// is the handler's own and not the CSRF middleware's.
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — a blank language must be refused by the handler, not by CSRF", response.StatusCode)
	}
	body := mustReadAll(t, response)
	if strings.TrimSpace(string(body)) == "Bad Request" {
		t.Fatalf("language switch rejection answered with fiber's bare text: %q", body)
	}
	assertTransportErrorEnvelope(t, body, "bad_request", "validation")
}

// TestProbeEndpointsKeepFixedOneWordBodies is the guard on the one place the
// envelope must NOT reach. /healthz and /readyz answer fixed one-word JSON in
// both outcomes: they are unauthenticated, so a localized envelope would put
// translated text — and a shape that varies with the caller's Accept header —
// on the surface an operator's probe parses.
func TestProbeEndpointsKeepFixedOneWordBodies(t *testing.T) {
	app := newCSRFGuardTestApp(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept", "application/json")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("probe %s: %v", path, err)
			}
			defer func() { _ = response.Body.Close() }()

			body := mustReadAll(t, response)
			payload := map[string]any{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("%s must answer JSON, got %q: %v", path, body, err)
			}
			if _, ok := payload["error_detail"]; ok {
				t.Fatalf("%s must not carry the mapped error envelope, got %q", path, body)
			}
			if len(payload) != 1 {
				t.Fatalf("%s must answer a fixed one-word body, got %q", path, body)
			}
		})
	}
}

func deleteSessionWithoutCSRFToken(t *testing.T, app *fiber.App, headers map[string]string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", strings.NewReader(""))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("token-less mutation failed: %v", err)
	}
	return response
}

var csrfTokenMetaPattern = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// issueCSRFFormCredentials fetches a page and returns the token/cookie pair a
// real form submission carries, so a test can reach a handler that sits behind
// the CSRF middleware.
func issueCSRFFormCredentials(t *testing.T, app *fiber.App) (string, string) {
	t.Helper()

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/login", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login page request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	match := csrfTokenMetaPattern.FindStringSubmatch(string(mustReadAll(t, response)))
	if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
		t.Fatal("expected the login page to carry a csrf token meta tag")
	}

	for _, candidate := range response.Cookies() {
		if candidate.Name == "ovumcy_csrf" {
			return match[1], candidate.Name + "=" + candidate.Value
		}
	}
	t.Fatal("expected the csrf middleware to set the ovumcy_csrf cookie")
	return "", ""
}

func mustReadAll(t *testing.T, response *http.Response) []byte {
	t.Helper()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return body
}
