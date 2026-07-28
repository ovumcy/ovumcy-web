package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// The rate-limit refusals were the last family answering outside the app-wide
// envelope. Their JSON arm built a body of its own — the stable key plus
// retry_after_seconds, with no error_detail — so one refusal reached a client in
// two shapes depending on which layer produced it: the edge limiter's stripped
// body on POST /api/v1/sessions, and the service-level attempt budget's full
// envelope from the same endpoint a moment earlier.
//
// The tests below sweep the limiter surfaces rather than the two known bad
// ones. The surface table is checked against the real number of limiter
// registrations in configureFiberMiddleware, so a limiter added later cannot be
// left out of the sweep by omission.

// htmlArm is the shape a limiter refusal must take for a plain browser request
// — no HX-Request, no JSON Accept. It is not one answer for the whole app: an
// auth or settings form is a page flow and gets its flash redirect, an /api
// route without an Accept header is an API client and gets the envelope, and a
// page form with no HTMX behind it must get markup a browser can render.
type htmlArm int

const (
	htmlArmRedirect htmlArm = iota
	htmlArmEnvelope
	htmlArmFragment
)

// rateLimitSurface is one limiter registration in configureFiberMiddleware,
// with the request that trips it and the answer it owes.
type rateLimitSurface struct {
	name         string
	method       string
	path         string
	key          string
	detailTarget string
	html         htmlArm
	htmlLocation string
}

// rateLimitSurfaces enumerates every limiter the composition root registers.
// Kept in registration order so it reads against server.go, and pinned to the
// real registration count by TestRateLimitSurfaceTableCoversEveryLimiter.
var rateLimitSurfaces = []rateLimitSurface{
	{
		name:         "logout",
		method:       http.MethodDelete,
		path:         "/api/v1/sessions/current",
		key:          "too_many_logout_attempts",
		detailTarget: "auth_form",
		html:         htmlArmRedirect,
		htmlLocation: "/login",
	},
	{
		name:         "login",
		method:       http.MethodPost,
		path:         "/api/v1/sessions",
		key:          "too_many_login_attempts",
		detailTarget: "auth_form",
		html:         htmlArmRedirect,
		htmlLocation: "/login",
	},
	{
		name:         "register",
		method:       http.MethodPost,
		path:         "/api/v1/users",
		key:          "too_many_register_attempts",
		detailTarget: "auth_form",
		html:         htmlArmRedirect,
		htmlLocation: "/register",
	},
	{
		name:         "forgot password",
		method:       http.MethodPost,
		path:         "/api/v1/password-resets",
		key:          "too_many_forgot_password_attempts",
		detailTarget: "auth_form",
		html:         htmlArmRedirect,
		htmlLocation: "/forgot-password",
	},
	{
		name:         "sso",
		method:       http.MethodGet,
		path:         "/auth/oidc/start",
		key:          "too_many_sso_attempts",
		detailTarget: "auth_form",
		html:         htmlArmRedirect,
		htmlLocation: "/login",
	},
	{
		// The only public form in the app with no HTMX and no JavaScript behind
		// it, which is why its plain-HTML arm renders a fragment: a refused
		// language switch is a full-page navigation, and the envelope arm painted
		// raw JSON into the browser window.
		name:         "language switch",
		method:       http.MethodPost,
		path:         api.LanguageSwitchPath,
		key:          "too many requests",
		detailTarget: "global",
		html:         htmlArmFragment,
	},
	{
		name:         "api catch-all",
		method:       http.MethodGet,
		path:         "/api/v1/stats/overview",
		key:          "too many requests",
		detailTarget: "global",
		html:         htmlArmEnvelope,
	},
	{
		// A machine subscription surface: no page behind it, so it never takes
		// the fragment arm — but it answered a bodyless 429 until this change,
		// which is the same split in the other direction.
		name:         "calendar feed",
		method:       http.MethodGet,
		path:         api.CalendarFeedRateLimitPrefix + "/ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP12.ics",
		key:          "too many requests",
		detailTarget: "global",
		html:         htmlArmEnvelope,
	},
}

// newRateLimitEnvelopeTestApp builds the REAL app — fiberConfig plus
// configureFiberMiddleware plus the real route table — with every budget spent
// after a single request, so the second request to any surface is refused by
// that surface's own limiter. A fresh app per surface keeps the buckets from
// leaking between them: several limiters count the same /api request, and a
// shared app would let one surface's exhaustion answer another's probe.
func newRateLimitEnvelopeTestApp(t *testing.T, handler *api.Handler) *fiber.App {
	t.Helper()

	return newFiberApp(runtimeConfig{
		Location:        time.UTC,
		DefaultLanguage: "en",
		RateLimits: rateLimitSettings{
			LoginMax:             1,
			LoginWindow:          time.Minute,
			ForgotPasswordMax:    1,
			ForgotPasswordWindow: time.Minute,
			RegisterMax:          1,
			RegisterWindow:       time.Minute,
			LogoutMax:            1,
			LogoutWindow:         time.Minute,
			APIMax:               1,
			APIWindow:            time.Minute,
			CalendarFeedMax:      1,
			CalendarFeedWindow:   time.Minute,
		},
	}, handler)
}

// spendBudgetAndProbe burns the single allowed request on a surface and returns
// the response to the one that follows it, which the limiter must refuse.
func spendBudgetAndProbe(t *testing.T, app *fiber.App, surface rateLimitSurface, headers map[string]string) *http.Response {
	t.Helper()

	for attempt := 1; attempt <= 2; attempt++ {
		request := httptest.NewRequest(surface.method, surface.path, strings.NewReader(""))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for name, value := range headers {
			request.Header.Set(name, value)
		}

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("%s request %d failed: %v", surface.name, attempt, err)
		}
		if attempt == 2 {
			return response
		}
		_ = response.Body.Close()
	}
	return nil
}

// TestEveryRateLimiterAnswersThroughTheSharedEnvelope is the JSON half: a
// refusal from any limiter carries {error, error_detail} with the surface's
// stable key, plus retry_after_seconds as an EXTENSION member rather than in
// place of the envelope. The stable keys are asserted per surface because they
// are already in the operator contract — one status keeps one key, and this
// change must not renumber them.
func TestEveryRateLimiterAnswersThroughTheSharedEnvelope(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	for _, surface := range rateLimitSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			app := newRateLimitEnvelopeTestApp(t, handler)
			response := spendBudgetAndProbe(t, app, surface, map[string]string{"Accept": "application/json"})
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusTooManyRequests {
				t.Fatalf("%s past its budget: status = %d, want 429", surface.name, response.StatusCode)
			}
			body := mustReadAll(t, response)

			payload := map[string]any{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("%s must answer the shared JSON envelope, got %q: %v", surface.name, body, err)
			}
			if payload["error"] != surface.key {
				t.Fatalf("%s error key = %v, want %q — the stable keys are already in the operator contract", surface.name, payload["error"], surface.key)
			}
			detail, ok := payload["error_detail"].(map[string]any)
			if !ok {
				t.Fatalf("%s answered without error_detail: %q — a rate-limit refusal is enveloped like every other rejection", surface.name, body)
			}
			if detail["key"] != surface.key || detail["category"] != "rate_limited" || detail["target"] != surface.detailTarget {
				t.Fatalf("%s error_detail = %v, want key=%q category=rate_limited target=%q", surface.name, detail, surface.key, surface.detailTarget)
			}

			// The extension member survives the move onto the shared envelope:
			// clients already depend on it, and it stays bounded by the
			// Retry-After header it is derived from.
			retryAfter, ok := payload["retry_after_seconds"].(float64)
			if !ok {
				t.Fatalf("%s dropped retry_after_seconds from the envelope: %q", surface.name, body)
			}
			header := strings.TrimSpace(response.Header.Get("Retry-After"))
			if header == "" {
				t.Fatalf("%s answered without a Retry-After header", surface.name)
			}
			if retryAfter < 1 || retryAfter > 60 {
				t.Fatalf("%s retry_after_seconds = %v, want integer seconds inside the 60s window", surface.name, retryAfter)
			}
			if header != strconv.Itoa(int(retryAfter)) {
				t.Fatalf("%s retry_after_seconds %v disagrees with Retry-After %q — the body must echo the header, not a second view of the timer", surface.name, retryAfter, header)
			}
		})
	}
}

// TestEveryRateLimiterAnswersABrowserWithoutRawJSON is the HTML half. A plain
// browser request carries no HX-Request and no JSON Accept, and each surface
// owes that request the shape its own flow needs: the auth forms keep their
// flash redirect, an /api path without an Accept header is an API client and
// keeps the envelope, and the language switch — the one public form with no
// HTMX behind it — must render markup instead of painting JSON into the window.
func TestEveryRateLimiterAnswersABrowserWithoutRawJSON(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	for _, surface := range rateLimitSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			app := newRateLimitEnvelopeTestApp(t, handler)
			response := spendBudgetAndProbe(t, app, surface, map[string]string{
				"Accept":          "text/html,application/xhtml+xml",
				"Accept-Language": "en",
			})
			defer func() { _ = response.Body.Close() }()

			body := string(mustReadAll(t, response))

			switch surface.html {
			case htmlArmRedirect:
				if response.StatusCode != http.StatusSeeOther {
					t.Fatalf("%s browser refusal: status = %d, want 303 back to the form", surface.name, response.StatusCode)
				}
				if location := response.Header.Get("Location"); location != surface.htmlLocation {
					t.Fatalf("%s browser refusal redirected to %q, want %q", surface.name, location, surface.htmlLocation)
				}
			case htmlArmEnvelope:
				if response.StatusCode != http.StatusTooManyRequests {
					t.Fatalf("%s browser refusal: status = %d, want 429", surface.name, response.StatusCode)
				}
				payload := map[string]any{}
				if err := json.Unmarshal([]byte(body), &payload); err != nil {
					t.Fatalf("%s must keep the envelope for a client that asked for no format, got %q", surface.name, body)
				}
				if _, ok := payload["error_detail"]; !ok {
					t.Fatalf("%s answered without error_detail: %q", surface.name, body)
				}
			case htmlArmFragment:
				if response.StatusCode != http.StatusTooManyRequests {
					t.Fatalf("%s browser refusal: status = %d, want 429", surface.name, response.StatusCode)
				}
				if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, fiber.MIMETextHTML) {
					t.Fatalf("%s browser refusal content type = %q, want text/html — markup labelled text/plain renders as tags", surface.name, contentType)
				}
				if strings.Contains(body, `"error_detail"`) {
					t.Fatalf("%s painted the JSON envelope into a full-page navigation: %q", surface.name, body)
				}
				if !strings.Contains(body, `class="status-error"`) {
					t.Fatalf("%s browser refusal is not the shared status fragment: %q", surface.name, body)
				}
				// The stable key rides next to the localized copy, so the assertion
				// never has to pin the copy itself.
				if !strings.Contains(body, `data-flash-key="common.error.too_many_requests"`) {
					t.Fatalf("%s browser refusal carries no stable flash key: %q", surface.name, body)
				}
			}
		})
	}
}

// TestRateLimitedHTMXFlowsRenderLocalizedCopy pins the browser-facing arm of the
// keys that had no copy at all. Three of the five auth limiters were mapped onto
// locale entries; the logout and registration keys were not, so an HTMX refusal
// rendered the machine key itself as the visible message in every language.
func TestRateLimitedHTMXFlowsRenderLocalizedCopy(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	cases := []struct {
		surface  rateLimitSurface
		flashKey string
	}{
		{surface: rateLimitSurfaces[0], flashKey: "auth.error.too_many_logout_attempts"},
		{surface: rateLimitSurfaces[1], flashKey: "auth.error.too_many_login_attempts"},
		{surface: rateLimitSurfaces[2], flashKey: "auth.error.too_many_register_attempts"},
		{surface: rateLimitSurfaces[3], flashKey: "auth.error.too_many_forgot_password_attempts"},
		{surface: rateLimitSurfaces[4], flashKey: "auth.error.too_many_sso_attempts"},
	}

	for _, testCase := range cases {
		t.Run(testCase.surface.name, func(t *testing.T) {
			app := newRateLimitEnvelopeTestApp(t, handler)
			response := spendBudgetAndProbe(t, app, testCase.surface, map[string]string{
				"HX-Request":      "true",
				"Accept-Language": "en",
			})
			defer func() { _ = response.Body.Close() }()

			body := string(mustReadAll(t, response))
			if !strings.Contains(body, `class="status-error"`) {
				t.Fatalf("%s htmx refusal is not the shared status fragment: %q", testCase.surface.name, body)
			}
			if !strings.Contains(body, `data-flash-key="`+testCase.flashKey+`"`) {
				t.Fatalf("%s htmx refusal carries no localized key, got %q", testCase.surface.name, body)
			}
			if strings.Contains(body, ">"+testCase.surface.key+"<") {
				t.Fatalf("%s rendered its machine key as the visible message: %q", testCase.surface.name, body)
			}
		})
	}
}

// TestRateLimitSurfaceTableCoversEveryLimiter is what makes the two sweeps above
// a sweep rather than a fix plus an allowlist: it counts the limiter
// registrations in the real configureFiberMiddleware and requires one table row
// per registration, so a limiter added later fails this test until it declares
// the answer it owes.
func TestRateLimitSurfaceTableCoversEveryLimiter(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("server.go"))
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}

	body := string(source)
	start := strings.Index(body, "func configureFiberMiddleware(")
	if start < 0 {
		t.Fatal("configureFiberMiddleware not found in server.go — update this guard alongside the rename")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of configureFiberMiddleware")
	}

	registrations := strings.Count(body[start:start+end], "limiter.New(")
	if registrations == 0 {
		t.Fatal("counted no limiter registrations — the guard is measuring the wrong thing")
	}
	if registrations != len(rateLimitSurfaces) {
		t.Fatalf("configureFiberMiddleware registers %d limiters but the surface table has %d rows; every limiter declares the answer it owes", registrations, len(rateLimitSurfaces))
	}
}
