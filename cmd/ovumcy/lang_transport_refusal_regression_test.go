package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

type languageSwitchRefusal struct {
	status      int
	contentType string
	body        string
}

func sendLanguageSwitchRefusal(t *testing.T, app *fiber.App, method string, form url.Values, headers map[string]string) languageSwitchRefusal {
	t.Helper()
	return sendPageFormRefusal(t, app, method, api.LanguageSwitchPath, form, headers)
}

func sendPageFormRefusal(t *testing.T, app *fiber.App, method string, path string, form url.Values, headers map[string]string) languageSwitchRefusal {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	return languageSwitchRefusal{
		status:      response.StatusCode,
		contentType: response.Header.Get(fiber.HeaderContentType),
		body:        string(mustReadAll(t, response)),
	}
}

func requireLanguageSwitchPage(t *testing.T, where string, answer languageSwitchRefusal, status int, backPath string) {
	t.Helper()
	if answer.status != status {
		t.Fatalf("%s: answered %d, want %d", where, answer.status, status)
	}
	if !strings.HasPrefix(answer.contentType, fiber.MIMETextHTML) || json.Valid([]byte(answer.body)) ||
		!strings.Contains(answer.body, `class="status-error"`) {
		t.Fatalf("%s: answered %d as %q (%q), want the text/html status fragment", where, answer.status, answer.contentType, answer.body)
	}
	if want := `<a href="` + backPath + `">`; !strings.Contains(answer.body, want) {
		t.Errorf("%s: the page carries no %s link back to the form: %q", where, want, answer.body)
	}
}

// TestLanguageSwitchTransportRefusalsAnswerThePage covers the refusals on
// POST /lang that no handler of the route answers itself: a recovered panic
// (500) and an expired request budget (503) reach the client through the
// app-wide negotiation, and a plain HTML navigation — the route's primary
// client — must get a page it can read and leave, not the JSON envelope painted
// into the window. A JSON caller keeps the envelope, and another method on the
// same path is an unrouted request like any other. WEB-71.
func TestLanguageSwitchTransportRefusalsAnswerThePage(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	faulty := fiber.New(fiberConfig(proxySettings{}, handler))
	faulty.Use(recover.New())
	faulty.Post(api.LanguageSwitchPath, func(fiber.Ctx) error {
		panic("language switch fault")
	})
	unrelatedFaultyPaths := []string{"/language", api.LanguageSwitchPath + "/extra", "/settings"}
	for _, path := range unrelatedFaultyPaths {
		faulty.Post(path, func(fiber.Ctx) error {
			panic("unrelated route fault")
		})
	}

	slow := fiber.New(fiberConfig(proxySettings{}, handler))
	slow.Post(api.LanguageSwitchPath, api.RequestDeadlineGuard(time.Millisecond, handler), func(c fiber.Ctx) error {
		<-c.Context().Done()
		return nil
	})

	form := url.Values{"lang": {"ru"}, "next": {"/calendar"}}

	requireLanguageSwitchPage(t, "recovered panic", sendLanguageSwitchRefusal(t, faulty, http.MethodPost, form, nil), http.StatusInternalServerError, "/calendar")
	requireLanguageSwitchPage(t, "expired budget", sendLanguageSwitchRefusal(t, slow, http.MethodPost, form, nil), http.StatusServiceUnavailable, "/calendar")

	offsite := url.Values{"lang": {"ru"}, "next": {"//elsewhere.example/phish"}}
	requireLanguageSwitchPage(t, "off-site next", sendLanguageSwitchRefusal(t, faulty, http.MethodPost, offsite, nil), http.StatusInternalServerError, "/")

	// The redirect sanitizer admits a same-origin path carrying `"` and `<`, so
	// the attribute escaping is what keeps a crafted next inside the href.
	breakout := url.Values{"lang": {"ru"}, "next": {`/"><x>`}}
	answer := sendLanguageSwitchRefusal(t, faulty, http.MethodPost, breakout, nil)
	requireLanguageSwitchPage(t, "markup in next", answer, http.StatusInternalServerError, "/&#34;&gt;&lt;x&gt;")
	if strings.Contains(answer.body, "<x>") {
		t.Fatalf("markup in next: the crafted path reached the page unescaped: %q", answer.body)
	}

	// Only POST /lang itself is the page form. A plain HTML navigation refused
	// on any other path, including one that merely starts with it, keeps the
	// app-wide envelope.
	for _, path := range unrelatedFaultyPaths {
		answer := sendPageFormRefusal(t, faulty, http.MethodPost, path, form, nil)
		if answer.status != http.StatusInternalServerError || strings.Contains(answer.body, "<a href=") {
			t.Fatalf("POST %s: answered %d as %q (%q), want the 500 envelope", path, answer.status, answer.contentType, answer.body)
		}
		assertTransportErrorEnvelope(t, []byte(answer.body), "internal_error", "internal")
	}

	answer = sendLanguageSwitchRefusal(t, faulty, http.MethodPost, form, map[string]string{"Accept": fiber.MIMEApplicationJSON})
	if answer.status != http.StatusInternalServerError {
		t.Fatalf("JSON caller: answered %d, want 500", answer.status)
	}
	assertTransportErrorEnvelope(t, []byte(answer.body), "internal_error", "internal")

	// The route and its limiter are POST-only; a browser GET of the same path is
	// not a refused language switch and keeps the app-wide answer.
	answer = sendLanguageSwitchRefusal(t, faulty, http.MethodGet, url.Values{}, nil)
	if answer.status != http.StatusMethodNotAllowed || !json.Valid([]byte(answer.body)) || strings.Contains(answer.body, "<a href=") {
		t.Fatalf("GET %s: answered %d as %q (%q), want the 405 JSON envelope every unrouted method gets", api.LanguageSwitchPath, answer.status, answer.contentType, answer.body)
	}
}

// TestLanguageSwitchRefusalsReachTheRequestLog pins that the page answer does
// not cost the operator the reason: the handler's 400 and the CSRF 403 are
// returned as errors for the top-level handler to answer, so the request log's
// safe_error column still names them, as it does on every other route.
func TestLanguageSwitchRefusalsReachTheRequestLog(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	cases := []struct {
		name    string
		csrf    bool
		form    url.Values
		status  int
		logText string
	}{
		{name: "CSRF refusal", csrf: true, form: url.Values{"lang": {"ru"}, "next": {"/calendar"}}, status: http.StatusForbidden, logText: "Forbidden"},
		{name: "blank language", form: url.Values{"lang": {"  "}, "next": {"/calendar"}}, status: http.StatusBadRequest, logText: "Bad Request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged bytes.Buffer
			app := fiber.New(fiberConfig(proxySettings{}, handler))
			app.Use(newRequestLogger(&logged))
			app.Use(handler.LanguageMiddleware)
			if tc.csrf {
				app.Use(csrf.New(csrfMiddlewareConfig(false, handler)))
			}
			app.Post(api.LanguageSwitchPath, handler.SetLanguage)

			requireLanguageSwitchPage(t, tc.name, sendLanguageSwitchRefusal(t, app, http.MethodPost, tc.form, nil), tc.status, "/calendar")
			line := strings.TrimSpace(logged.String())
			if !strings.Contains(line, "| POST |") || !strings.HasSuffix(line, "| "+tc.logText) {
				t.Errorf("%s: request log line %q does not end with the refusal %q", tc.name, line, tc.logText)
			}
		})
	}
}

// TestLanguageSwitchOversizedBodyAnswersLocalizedRefusal is WEB-86: fasthttp
// enforces BodyLimit in its own wire-level reader (App.serverErrorHandler),
// on a context no app.Use middleware has touched yet — LanguageMiddleware
// included — so the rejection reaches the top-level ErrorHandler
// (newOvumcyErrorHandler -> handler.RespondTransportError) with no
// request-scoped messages in locals. The two markup arms of apiError must
// resolve the request's own locale catalogue themselves
// (ensureRequestMessages) rather than rendering the raw machine key.
//
// The in-memory app.Test transport cannot simulate the wire-level rejection
// itself — it surfaces fasthttp's read error to the Go caller rather than a
// routed response (see TestFiberAppEnforcesBodyLimit in main_test.go) — so,
// exactly as TestOvumcyErrorHandlerMapsBodyLimitTo413 does, the handler
// returns the same *fiber.Error the real rejection carries. Built on
// fiberConfig (this file's app-building helper) with NO LanguageMiddleware
// registered, which is precisely the shape of the early path: neither this
// nor the pre-routing 431 ever run behind it.
func TestLanguageSwitchOversizedBodyAnswersLocalizedRefusal(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	ruMessages := manager.Messages(i18n.LangRU)
	expectedCopy := strings.TrimSpace(ruMessages["common.error.request_too_large"])
	if expectedCopy == "" {
		t.Fatal("locale ru defines no common.error.request_too_large")
	}
	expectedBack := strings.TrimSpace(ruMessages["common.back"])
	if expectedBack == "" {
		t.Fatal("locale ru defines no common.back")
	}

	app := fiber.New(fiberConfig(proxySettings{}, handler))
	app.Post(api.LanguageSwitchPath, func(fiber.Ctx) error {
		return fiber.ErrRequestEntityTooLarge
	})

	send := func(t *testing.T, headers map[string]string) *http.Response {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, api.LanguageSwitchPath, strings.NewReader("lang=ru"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Cookie", "ovumcy_lang=ru")
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return response
	}

	t.Run("plain HTML navigation", func(t *testing.T) {
		response := send(t, map[string]string{"Accept": "text/html"})
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", response.StatusCode)
		}
		body := string(mustReadAll(t, response))
		if !strings.Contains(body, expectedCopy) {
			t.Fatalf("plain nav: expected the Russian request_too_large copy %q, got %q", expectedCopy, body)
		}
		if !strings.Contains(body, expectedBack) {
			t.Fatalf("plain nav: expected the Russian back label %q, got %q", expectedBack, body)
		}
		if strings.Contains(body, ">request_too_large<") || strings.Contains(body, ">common.back<") {
			t.Fatalf("plain nav: raw machine key leaked as visible text: %q", body)
		}
	})

	t.Run("htmx request", func(t *testing.T) {
		response := send(t, map[string]string{"HX-Request": "true"})
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", response.StatusCode)
		}
		body := string(mustReadAll(t, response))
		if !strings.Contains(body, `class="status-error"`) {
			t.Fatalf("htmx: expected the shared status-error fragment, got %q", body)
		}
		if !strings.Contains(body, expectedCopy) {
			t.Fatalf("htmx: expected the Russian request_too_large copy %q, got %q", expectedCopy, body)
		}
		if strings.Contains(body, ">request_too_large<") {
			t.Fatalf("htmx: raw machine key leaked as visible text: %q", body)
		}
	})

	// Positive control: the JSON envelope for the same oversized request still
	// carries the unlocalized machine key unchanged — this fix touches only the
	// two markup arms, never the JSON contract.
	t.Run("json client control", func(t *testing.T) {
		response := send(t, map[string]string{"Accept": "application/json"})
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", response.StatusCode)
		}
		body := mustReadAll(t, response)
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal JSON envelope %q: %v", body, err)
		}
		if payload["error"] != "request_too_large" {
			t.Fatalf("json control: error key = %v, want %q", payload["error"], "request_too_large")
		}
	})
}
