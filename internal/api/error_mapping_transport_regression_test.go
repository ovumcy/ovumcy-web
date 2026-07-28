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
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

// TestRespondMappedErrorGlobalHTMXReturnsLocalizedStatusMarkup covers apiError's
// SECOND localization branch: a spec key that has no entry in
// authErrorTranslationKeys but is itself a locale entry, which is how the
// day-cycle-start conflicts are translated. The probe deliberately uses such a
// key — it used to use "not found", which now has a mapping and so exercises the
// first branch instead, leaving this one untested.
func TestRespondMappedErrorGlobalHTMXReturnsLocalizedStatusMarkup(t *testing.T) {
	t.Parallel()

	app, _ := newErrorMappingTransportTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/test/htmx", nil)
	request.Header.Set("HX-Request", "true")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusConflict)

	body := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `class="status-error"`, message: "expected shared status-error wrapper for HTMX errors"},
		bodyStringMatch{fragment: "Localized cycle start conflict.", message: "expected HTMX branch to localize a spec key that is its own locale entry"},
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

// TestRequestBodyLimitGuardSkipsMethodsThatReachNoBodyReader pins the scope of
// the decode probe, which is the whole cost of the guard: probing IS
// decompressing, so running it on a route that reads nothing converts a small
// compressed body into a BodyLimit-sized allocation for free. A highly
// compressible payload sized to stay just inside the cap made every
// unauthenticated bodyless route — /healthz, /readyz, /login, /favicon.ico —
// pay it, none of which sits behind a rate limiter.
//
// The over-cap payload is what makes "the guard did not run" observable without
// a clock: for a decoded size past the cap the probe cannot both run and stay
// silent — it answers the mapped 413 and the route never runs. So a bodyless
// method reaching its route at all proves nothing decompressed, and the same
// payload on a body-carrying method is the positive anchor that it really is
// over the cap and the guard really is live.
func TestRequestBodyLimitGuardSkipsMethodsThatReachNoBodyReader(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{BodyLimit: bodyLimitGuardTestLimit})
	app.Use(requestBodyLimitGuard)

	var observedRawBody []byte
	app.Get("/bodyless", func(c fiber.Ctx) error {
		observedRawBody = append([]byte(nil), c.Request().Body()...)
		return c.SendString("route ran")
	})
	app.Post("/reads-body", func(c fiber.Ctx) error {
		return c.SendString("route ran")
	})

	overCap := gzipTestBody(t, bytes.Repeat([]byte("a"), bodyLimitGuardTestLimit+1))

	anchor := httptest.NewRequest(http.MethodPost, "/reads-body", bytes.NewReader(overCap))
	anchor.Header.Set("Content-Encoding", "gzip")
	anchor.Header.Set("Accept", "application/json")

	anchorResponse := mustAppResponse(t, app, anchor)
	assertStatusCode(t, anchorResponse, fiber.StatusRequestEntityTooLarge)
	if body := mustReadBodyString(t, anchorResponse.Body); !strings.Contains(body, `"error":"request_too_large"`) {
		t.Fatalf("expected the same payload to be rejected on a body-carrying method, got %q", body)
	}

	request := httptest.NewRequest(http.MethodGet, "/bodyless", bytes.NewReader(overCap))
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	if response.StatusCode == fiber.StatusRequestEntityTooLarge {
		t.Fatalf("expected no decode probe on %s, which reaches no body reader; the 413 only the probe can produce means the compressed body was inflated anyway", http.MethodGet)
	}
	assertStatusCode(t, response, fiber.StatusOK)
	if body := mustReadBodyString(t, response.Body); body != "route ran" {
		t.Fatalf("expected the bodyless route to answer for itself, got %q", body)
	}
	// Without this the case would be vacuous: a request that carried no body at
	// all would also reach the route. The route sees the compressed bytes exactly
	// as they arrived, still gzip-framed.
	if len(observedRawBody) != len(overCap) {
		t.Fatalf("expected the route to receive all %d compressed bytes, got %d", len(overCap), len(observedRawBody))
	}
	if !bytes.HasPrefix(observedRawBody, []byte{0x1f, 0x8b}) {
		t.Fatalf("expected the request body to still carry the gzip header, got %x", observedRawBody[:2])
	}
}

// TestRequestBodyLimitGuardCoversEveryRegisteredRouteThatCanCarryAReadBody is
// the forward-looking half of the scope contract, and the reason narrowing the
// guard cannot quietly drop a route: it walks the real route table and requires
// the mapped 413 on every registered method that can reach a body reader, with
// no allowlist to keep in sync. A route added later inherits the assertion the
// moment it is registered. Routes on the excluded methods are required not to
// answer 413 at all — the same "the probe did not run" observable as above,
// swept across the whole table rather than one hand-built route.
//
// The guard runs ahead of authentication, so no session is needed: an
// over-limit compressed body is rejected identically whether or not the caller
// could have used the endpoint.
func TestRequestBodyLimitGuardCoversEveryRegisteredRouteThatCanCarryAReadBody(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{bodyLimit: bodyLimitGuardTestLimit})
	overCap := gzipTestBody(t, bytes.Repeat([]byte("a"), bodyLimitGuardTestLimit+1))

	guarded, skipped := 0, 0
	// filterUseOption=true drops middleware/Use entries (the guard itself, the
	// group-level AuthRequired/OwnerOnly, the NotFound catch-all), which are not
	// endpoints and register under every method at their prefix.
	for _, route := range app.GetRoutes(true) {
		path := concreteRoutePathForBodyLimitProbe(route.Path)
		expectRejection := requestMethodCanCarryAReadBody(route.Method)
		if expectRejection {
			guarded++
		} else {
			skipped++
		}

		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			request := httptest.NewRequest(route.Method, path, bytes.NewReader(overCap))
			request.Header.Set("Content-Encoding", "gzip")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json")

			response := mustAppResponse(t, app, request)
			if !expectRejection {
				if response.StatusCode == fiber.StatusRequestEntityTooLarge {
					t.Fatalf("expected no decode probe on %s, which reaches no body reader, got the 413 only the probe can produce", route.Method)
				}
				return
			}
			assertStatusCode(t, response, fiber.StatusRequestEntityTooLarge)
			if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"request_too_large"`) {
				t.Fatalf("expected the mapped request_too_large envelope, got %q", body)
			}
		})
	}

	if guarded == 0 || skipped == 0 {
		t.Fatalf("expected the route table to hold both guarded and skipped methods, got %d guarded and %d skipped; recheck route discovery", guarded, skipped)
	}
}

// concreteRoutePathForBodyLimitProbe turns a registered route pattern into a
// requestable path. Any ":param" segment becomes a placeholder — keeping a
// trailing literal suffix such as the calendar feed's ".ics", which is part of
// the pattern rather than of the parameter — so a route added later with a
// parameter this file has never seen still resolves.
func concreteRoutePathForBodyLimitProbe(routePath string) string {
	segments := strings.Split(routePath, "/")
	for index, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		_, suffix, hasSuffix := strings.Cut(segment, ".")
		if hasSuffix {
			segments[index] = "probe." + suffix
			continue
		}
		segments[index] = "probe"
	}
	return strings.Join(segments, "/")
}

// TestRequestBodyLimitGuardAnswersTheMappedEnvelopeOverAnUpstream413Stamp pins
// the order of the guard's two status checks. The probe's only signal is the
// status fiber stamps while decoding, and the guard used to read that signal as
// "the status changed", which is a different question: with a 413 already on the
// response the status does not change, the equality short-circuits, and the
// over-limit body — fiber's substituted error string, not the payload — is
// handed to the handler, exactly the failure the guard exists to prevent. It now
// tests for the 413 itself first and so fails closed regardless of what a future
// middleware upstream of it leaves on the response.
func TestRequestBodyLimitGuardAnswersTheMappedEnvelopeOverAnUpstream413Stamp(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{BodyLimit: bodyLimitGuardTestLimit})
	app.Use(func(c fiber.Ctx) error {
		c.Status(fiber.StatusRequestEntityTooLarge)
		return c.Next()
	})
	app.Use(requestBodyLimitGuard)

	routeReached := false
	var observedBody string
	app.Post("/probe", func(c fiber.Ctx) error {
		routeReached = true
		observedBody = string(c.Body())
		return nil
	})

	request := httptest.NewRequest(http.MethodPost, "/probe", bytes.NewReader(gzipTestBody(t, bytes.Repeat([]byte("a"), bodyLimitGuardTestLimit+1))))
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	if routeReached {
		t.Fatalf("expected the over-limit request to be rejected before the route ran; it ran and read %q, the framework's substituted string standing in for the payload", observedBody)
	}
	assertStatusCode(t, response, fiber.StatusRequestEntityTooLarge)
	if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"request_too_large"`) {
		t.Fatalf("expected the mapped envelope to win over an upstream 413 stamp, got %q", body)
	}
}

// TestTransportErrorSpecForStatusIsTotal pins the mapping the top-level
// ErrorHandler applies to every explicit *fiber.Error. Two properties matter and
// neither is observable from a single status: each listed status carries its own
// stable key (so a client can branch on the key rather than re-deriving meaning
// from the status), and an UNLISTED status still resolves to a spec — the reason
// nothing can fall through to the framework's bare text the way every status
// except 413/431 used to.
//
// The out-of-range rows are the guard on the fallback's own arithmetic: 399 and
// 600 are not error statuses at all, and honouring them would ship a body
// claiming failure under a status that claims otherwise.
func TestTransportErrorSpecForStatusIsTotal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		status int
		want   APIErrorSpec
	}{
		{name: "mapped 400", status: fiber.StatusBadRequest, want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "bad_request")},
		{name: "mapped 401 shares the auth guard's key", status: fiber.StatusUnauthorized, want: unauthorizedErrorSpec()},
		{name: "mapped 403", status: fiber.StatusForbidden, want: globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "forbidden")},
		{name: "mapped 404 shares the route-level key", status: fiber.StatusNotFound, want: notFoundErrorSpec()},
		{name: "mapped 405", status: fiber.StatusMethodNotAllowed, want: globalErrorSpec(fiber.StatusMethodNotAllowed, APIErrorCategoryValidation, "method_not_allowed")},
		{name: "mapped 413 keeps the pre-routing key", status: fiber.StatusRequestEntityTooLarge, want: globalErrorSpec(fiber.StatusRequestEntityTooLarge, APIErrorCategoryTooLarge, "request_too_large")},
		{name: "mapped 415", status: fiber.StatusUnsupportedMediaType, want: globalErrorSpec(fiber.StatusUnsupportedMediaType, APIErrorCategoryValidation, "unsupported_media_type")},
		{name: "mapped 429 shares the limiter key", status: fiber.StatusTooManyRequests, want: globalRateLimitErrorSpec()},
		{name: "mapped 431 keeps the pre-routing key", status: fiber.StatusRequestHeaderFieldsTooLarge, want: globalErrorSpec(fiber.StatusRequestHeaderFieldsTooLarge, APIErrorCategoryTooLarge, "request_headers_too_large")},
		{name: "mapped 500", status: fiber.StatusInternalServerError, want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "internal_error")},
		{name: "mapped 503 is not the deadline guard's key", status: fiber.StatusServiceUnavailable, want: globalErrorSpec(fiber.StatusServiceUnavailable, APIErrorCategoryInternal, "service_unavailable")},
		{name: "unlisted 4xx falls back to its class", status: fiber.StatusTeapot, want: globalErrorSpec(fiber.StatusTeapot, APIErrorCategoryValidation, "request_rejected")},
		{name: "unlisted 4xx upper boundary", status: 499, want: globalErrorSpec(499, APIErrorCategoryValidation, "request_rejected")},
		{name: "unlisted 5xx falls back to its class", status: fiber.StatusInsufficientStorage, want: globalErrorSpec(fiber.StatusInsufficientStorage, APIErrorCategoryInternal, "internal_error")},
		{name: "unlisted 5xx upper boundary", status: 599, want: globalErrorSpec(599, APIErrorCategoryInternal, "internal_error")},
		{name: "below the error range becomes 500", status: 399, want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "internal_error")},
		{name: "above the error range becomes 500", status: 600, want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "internal_error")},
	}

	seenKeys := map[string]int{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := transportErrorSpecForStatus(testCase.status); got != testCase.want {
				t.Fatalf("transportErrorSpecForStatus(%d) = %#v, want %#v", testCase.status, got, testCase.want)
			}
		})
		seenKeys[transportErrorSpecForStatus(testCase.status).Key]++
	}

	// A table that answered every status with one generic key would satisfy every
	// row above while destroying the point of a machine key.
	if len(seenKeys) < 10 {
		t.Fatalf("expected the mapped statuses to carry distinct keys, got %d distinct keys across %d statuses: %v", len(seenKeys), len(testCases), seenKeys)
	}
}

// TestRespondTransportErrorNegotiatesFormat pins that the app-wide entry point
// answers through the SAME negotiation as every mapped domain error rather than
// a second, transport-only format: the JSON envelope with error + error_detail
// for an API client, the shared status-error fragment carrying the stable key
// for an HTMX flow. 403 stands in for the whole table — it is the status the
// CSRF middleware raises on every mutating route, and the one that used to reach
// the client as fiber's bare "Forbidden".
func TestRespondTransportErrorNegotiatesFormat(t *testing.T) {
	t.Parallel()

	newApp := func() *fiber.App {
		app := fiber.New()
		app.Get("/probe", func(c fiber.Ctx) error {
			c.Locals(contextMessagesKey, map[string]string{"common.error.forbidden": "Localized refusal."})
			return RespondTransportError(c, fiber.StatusForbidden)
		})
		return app
	}

	t.Run("json envelope", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/probe", nil)
		request.Header.Set("Accept", "application/json")

		response := mustAppResponse(t, newApp(), request)
		assertStatusCode(t, response, http.StatusForbidden)

		payload := map[string]any{}
		if err := json.Unmarshal([]byte(mustReadBodyString(t, response.Body)), &payload); err != nil {
			t.Fatalf("unmarshal JSON envelope: %v", err)
		}
		if payload["error"] != "forbidden" {
			t.Fatalf("error key: got %v want %q", payload["error"], "forbidden")
		}
		detail, ok := payload["error_detail"].(map[string]any)
		if !ok {
			t.Fatalf("expected error_detail object, got %v", payload["error_detail"])
		}
		if detail["key"] != "forbidden" || detail["category"] != "forbidden" || detail["target"] != "global" {
			t.Fatalf("unexpected error_detail: %v", detail)
		}
	})

	t.Run("htmx status fragment", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/probe", nil)
		request.Header.Set("HX-Request", "true")

		response := mustAppResponse(t, newApp(), request)
		assertStatusCode(t, response, http.StatusForbidden)

		body := mustReadBodyString(t, response.Body)
		assertBodyContainsAll(t, body,
			bodyStringMatch{fragment: `class="status-error"`, message: "expected the shared status-error wrapper for an HTMX transport error"},
			bodyStringMatch{fragment: `data-flash-key="common.error.forbidden"`, message: "expected the stable flash key next to the localized copy"},
			bodyStringMatch{fragment: "Localized refusal.", message: "expected the transport key to resolve to localized copy"},
		)
		assertBodyNotContainsAll(t, body,
			bodyStringMatch{fragment: "<html", message: "did not expect full-page markup in an HTMX transport error"},
			bodyStringMatch{fragment: "Forbidden", message: "did not expect the framework's bare status text in the rendered fragment"},
		)
	})
}

// transportErrorSpecsOnTheUserSurface enumerates every spec the transport layer
// can hand a caller, DERIVED rather than listed, so the sweeps below have no
// allowlist to keep in sync and a spec added later is covered the day it lands.
//
// The status walk is what makes it total: transportErrorSpecForStatus is the
// single resolver behind RespondTransportError, RespondRequestEntityTooLarge and
// RespondRequestHeadersTooLarge, and walking the whole 4xx/5xx range picks up
// both the table entries and the two class fallbacks (request_rejected,
// internal_error) without naming either. requestTimeoutErrorSpec is added by
// hand because it is deliberately NOT in that table — 503 there means
// service_unavailable, a statement about the server, while an expired request
// budget is a statement about the caller's request — and that exclusion is
// exactly what let its key slip past every existing guard.
func transportErrorSpecsOnTheUserSurface() []APIErrorSpec {
	seen := map[string]bool{}
	specs := make([]APIErrorSpec, 0, len(transportErrorSpecsByStatus)+3)
	add := func(spec APIErrorSpec) {
		if seen[spec.Key] {
			return
		}
		seen[spec.Key] = true
		specs = append(specs, spec)
	}

	for status := 400; status < 600; status++ {
		add(transportErrorSpecForStatus(status))
	}
	add(requestTimeoutErrorSpec())

	return specs
}

// TestEveryTransportErrorKeyRendersLocalizedCopyInEveryLocale closes the half of
// the i18n contract nothing checked. services.TestAuthErrorTranslationKeysResolveInEveryLocale
// walks the MAPPING and proves each entry resolves in every locale; it cannot
// see a spec key that never reached the map at all, and an unmapped key renders
// as itself — translateMessage answers an unknown key with the key. That is how
// request_timeout shipped as the literal text "request_timeout" on an HTMX flow
// that outlived its budget, in all six languages, next to a CSRF refusal on the
// same app that answered with a human sentence.
//
// The sweep walks the SPECS instead, so both halves are now pinned: every key
// the transport layer can produce has a mapping, and that mapping resolves to
// non-empty copy in every supported locale.
func TestEveryTransportErrorKeyRendersLocalizedCopyInEveryLocale(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	specs := transportErrorSpecsOnTheUserSurface()
	if len(specs) < len(transportErrorSpecsByStatus) {
		t.Fatalf("derived %d transport specs for a table of %d entries; recheck spec discovery", len(specs), len(transportErrorSpecsByStatus))
	}

	for _, spec := range specs {
		t.Run(spec.Key, func(t *testing.T) {
			translationKey := services.AuthErrorTranslationKey(spec.Key)
			if translationKey == "" {
				t.Fatalf("transport key %q (status %d) has no entry in services.authErrorTranslationKeys: it renders as the raw machine key to every user in every language", spec.Key, spec.Status)
			}
			for _, language := range languages {
				if value := strings.TrimSpace(manager.Messages(language)[translationKey]); value == "" {
					t.Errorf("transport key %q maps to %q, which locale %q does not define: the message would render as the raw key", spec.Key, translationKey, language)
				}
			}
		})
	}
}

// TestRequestTimeoutRendersLocalizedCopyToAnHTMXCaller is the instance the sweep
// above generalizes, driven through the real guard and the real locale
// catalogues rather than through the spec table: an HTMX flow that outlives its
// budget must get a sentence, not the machine key, and the stable key must ride
// next to it as data-flash-key the way every other transport rejection's does.
func TestRequestTimeoutRendersLocalizedCopyToAnHTMXCaller(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	for _, language := range []string{i18n.LangEN, i18n.LangRU} {
		t.Run(language, func(t *testing.T) {
			messages := manager.Messages(language)
			expected := strings.TrimSpace(messages["common.error.request_timeout"])
			if expected == "" {
				t.Fatalf("locale %q defines no common.error.request_timeout", language)
			}

			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				c.Locals(contextMessagesKey, messages)
				return c.Next()
			})
			app.Use(RequestDeadlineGuard(time.Millisecond))
			app.Get("/slow", func(c fiber.Ctx) error {
				<-c.Context().Done()
				return c.SendString("handler finished anyway")
			})

			request := httptest.NewRequest(http.MethodGet, "/slow", nil)
			request.Header.Set("HX-Request", "true")

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, fiber.StatusServiceUnavailable)

			body := mustReadBodyString(t, response.Body)
			assertBodyContainsAll(t, body,
				bodyStringMatch{fragment: `data-flash-key="common.error.request_timeout"`, message: "expected the resolved i18n key next to the copy"},
				bodyStringMatch{fragment: expected, message: "expected the localized request-timeout sentence"},
			)
			assertBodyNotContainsAll(t, body,
				bodyStringMatch{fragment: ">request_timeout<", message: "did not expect the raw machine key as the visible message"},
			)
		})
	}
}

func newErrorMappingTransportTestApp(t *testing.T) (*fiber.App, *Handler) {
	t.Helper()

	handler := &Handler{secretKey: []byte("test-error-mapping-secret")}
	app := fiber.New()

	app.Get("/api/test/global", func(c fiber.Ctx) error {
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"))
	})
	app.Get("/api/test/htmx", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, map[string]string{"cycle start replace required": "Localized cycle start conflict."})
		return respondGlobalMappedError(c, globalErrorSpec(fiber.StatusConflict, APIErrorCategoryConflict, "cycle start replace required"))
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
