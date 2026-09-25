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
)

type languageSwitchRefusal struct {
	status      int
	contentType string
	body        string
}

func sendLanguageSwitchRefusal(t *testing.T, app *fiber.App, method string, form url.Values, headers map[string]string) languageSwitchRefusal {
	t.Helper()
	request := httptest.NewRequest(method, api.LanguageSwitchPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, api.LanguageSwitchPath, err)
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
	faulty := fiber.New(fiberConfig(proxySettings{}))
	faulty.Use(recover.New())
	faulty.Post(api.LanguageSwitchPath, func(fiber.Ctx) error {
		panic("language switch fault")
	})

	slow := fiber.New(fiberConfig(proxySettings{}))
	slow.Post(api.LanguageSwitchPath, api.RequestDeadlineGuard(time.Millisecond), func(c fiber.Ctx) error {
		<-c.Context().Done()
		return nil
	})

	form := url.Values{"lang": {"ru"}, "next": {"/calendar"}}

	requireLanguageSwitchPage(t, "recovered panic", sendLanguageSwitchRefusal(t, faulty, http.MethodPost, form, nil), http.StatusInternalServerError, "/calendar")
	requireLanguageSwitchPage(t, "expired budget", sendLanguageSwitchRefusal(t, slow, http.MethodPost, form, nil), http.StatusServiceUnavailable, "/calendar")

	offsite := url.Values{"lang": {"ru"}, "next": {"//elsewhere.example/phish"}}
	requireLanguageSwitchPage(t, "off-site next", sendLanguageSwitchRefusal(t, faulty, http.MethodPost, offsite, nil), http.StatusInternalServerError, "/")

	answer := sendLanguageSwitchRefusal(t, faulty, http.MethodPost, form, map[string]string{"Accept": fiber.MIMEApplicationJSON})
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
			app := fiber.New(fiberConfig(proxySettings{}))
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
