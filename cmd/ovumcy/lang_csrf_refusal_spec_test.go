package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// openAPIResponseBlock returns the lines nested under paths → path → method →
// responses → status in docs/openapi.yaml, read by indentation at two-space
// steps. It fails when a level is missing, so a response the spec does not
// declare reddens by name.
func openAPIResponseBlock(t *testing.T, spec string, path string, method string, status string) []string {
	t.Helper()
	keys := []string{"paths", path, method, "responses", "'" + status + "'"}
	level := 0
	var block []string
	for _, raw := range strings.Split(spec, "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if level == len(keys) {
			if indent <= 2*(level-1) {
				return block
			}
			block = append(block, text)
			continue
		}
		if indent < 2*level {
			break
		}
		if indent == 2*level && text == keys[level]+":" {
			level++
		}
	}
	if level == len(keys) {
		return block
	}
	t.Fatalf("docs/openapi.yaml declares no %s", strings.Join(keys, " → "))
	return nil
}

// TestOpenAPILanguageSwitchDeclaresTheCSRFRefusalItAnswers pins the 403 the
// CSRF middleware answers on POST /lang against what docs/openapi.yaml declares
// for it. It lives here, not beside the route's other spec pins in
// internal/api, because the refusal is produced by csrfMiddlewareConfig and
// ovumcyErrorHandler, and a copy of either there would pin the spec to the copy.
//
// Three refusal causes are driven — no token, a token that does not match the
// cookie, and a valid token sent from another origin — through every caller the
// route has. Only HTMX gets the fragment; the plain form submission gets the
// JSON envelope like an API client, so the description has to say so.
func TestOpenAPILanguageSwitchDeclaresTheCSRFRefusalItAnswers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	forbidden := openAPIResponseBlock(t, string(data), "/lang", "post", "403")

	app := newCSRFGuardTestApp(t)
	csrfToken, csrfCookie := issueCSRFFormCredentials(t, app)
	type refusal struct {
		name   string
		token  string
		cookie string
		origin string
	}
	refusals := []refusal{
		{name: "no token"},
		{name: "mismatched token", token: "not-the-issued-token", cookie: csrfCookie},
		{name: "foreign origin", token: csrfToken, cookie: csrfCookie, origin: "http://elsewhere.example"},
	}
	clients := []struct {
		name     string
		headers  map[string]string
		fragment bool
	}{
		{name: "JSON caller", headers: map[string]string{"Accept": fiber.MIMEApplicationJSON}},
		{name: "form submission"},
		{name: "browser form", headers: map[string]string{"Accept": "text/html,application/xhtml+xml"}},
		{name: "HTMX request", headers: map[string]string{"HX-Request": "true", "Accept": fiber.MIMEApplicationJSON}, fragment: true},
	}

	// The same token and cookie with no Origin pass the check, so the foreign
	// origin row is refused for its Origin and nothing else.
	accepted := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(url.Values{"lang": {"ru"}, "csrf_token": {csrfToken}}.Encode()))
	accepted.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	accepted.Header.Set("Cookie", csrfCookie)
	acceptedResponse, err := app.Test(accepted, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("control: POST /lang: %v", err)
	}
	_ = acceptedResponse.Body.Close()
	if acceptedResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("control: the issued token with no Origin answered %d, want 303", acceptedResponse.StatusCode)
	}

	var envelopeSeen bool
	for _, cause := range refusals {
		for _, client := range clients {
			where := cause.name + ", " + client.name
			form := url.Values{"lang": {"ru"}}
			if cause.token != "" {
				form.Set("csrf_token", cause.token)
			}
			request := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if cause.cookie != "" {
				request.Header.Set("Cookie", cause.cookie)
			}
			if cause.origin != "" {
				request.Header.Set("Origin", cause.origin)
			}
			for name, value := range client.headers {
				request.Header.Set(name, value)
			}
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("%s: POST /lang: %v", where, err)
			}
			body := mustReadAll(t, response)
			_ = response.Body.Close()

			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s: answered %d, want the CSRF 403", where, response.StatusCode)
			}
			for _, cookie := range response.Cookies() {
				if cookie.Name == "ovumcy_lang" {
					t.Errorf("%s: the refused switch still set ovumcy_lang: %#v", where, cookie)
				}
			}
			contentType := response.Header.Get(fiber.HeaderContentType)
			if client.fragment {
				if !strings.HasPrefix(contentType, fiber.MIMETextHTML) || json.Valid(body) {
					t.Errorf("%s: answered 403 as %q (%q), want the text/html status fragment", where, contentType, body)
				}
				continue
			}
			if !strings.HasPrefix(contentType, fiber.MIMEApplicationJSON) {
				t.Fatalf("%s: answered 403 as %q (%q), want the JSON envelope", where, contentType, body)
			}
			assertTransportErrorEnvelope(t, body, "forbidden", "forbidden")
			envelopeSeen = true
		}
	}
	if !envelopeSeen {
		t.Fatal("no caller got the JSON envelope; the example lines below were checked against nothing")
	}

	for _, want := range []string{
		"schema: { $ref: '#/components/schemas/ApiError' }",
		`error: "forbidden"`,
		fmt.Sprintf("error_detail: { key: %q, category: %q, target: %q }", "forbidden", "forbidden", "global"),
	} {
		requireOpenAPILine(t, forbidden, want)
	}
	joined := strings.Join(forbidden, " ")
	for _, phrase := range []string{"the plain form submission included", "`HX-Request: true`", "`text/html`", "`Origin` that names"} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("POST /lang 403: docs/openapi.yaml never mentions %q — the server answers that way and the description has to say so:\n  %s",
				phrase, strings.Join(forbidden, "\n  "))
		}
	}
}

func requireOpenAPILine(t *testing.T, block []string, want string) {
	t.Helper()
	for _, line := range block {
		if line == want {
			return
		}
	}
	t.Errorf("POST /lang 403: docs/openapi.yaml does not carry %q — the spec no longer describes what the server answers there:\n  %s",
		want, strings.Join(block, "\n  "))
}
