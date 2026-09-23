package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestOpenAPILanguageSwitchDeclaresTheAccountWriteFailureItAnswers pins the 500
// POST /lang answers when a signed-in owner switches to a shipped language and
// the account write fails, against what docs/openapi.yaml declares for it. The
// spec published no 500 for the route, while persistSwitchedLanguage refuses
// the switch with settingsInterfaceUpdateErrorSpec rather than degrading it to
// a cookie-only change.
//
// The answer is produced, not restated: the package test app with CSRF on, a live owner
// session, and the write failure injected by dropping the column the save
// targets, as TestAuthenticatedLanguageSwitchReportsAFailedAccountWrite does.
// The route answers a JSON caller the envelope and both browser callers — the
// plain form and HTMX — the localized fragment, so each is driven and the
// description has to name both carriers.
func TestOpenAPILanguageSwitchDeclaresTheAccountWriteFailureItAnswers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	failed := openAPIYAMLBlock(t, string(data), "paths", LanguageSwitchPath, "post", "responses", "'500'")

	ctx := newSettingsSecurityTestContext(t, "lang-switch-spec-500@example.com")
	if err := ctx.database.Exec("ALTER TABLE users DROP COLUMN interface_language").Error; err != nil {
		t.Fatalf("drop interface_language column: %v", err)
	}
	send := func(name string, headers map[string]string) languageSwitchAnswer {
		response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, LanguageSwitchPath, url.Values{
			"lang": {"ru"},
			"next": {"/dashboard"},
		}, headers)
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("%s: read body: %v", name, err)
		}
		if response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s: a failed account write answered %d, want 500", name, response.StatusCode)
		}
		if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
			t.Errorf("%s: the failed switch still set %s: %#v", name, languageCookieName, cookie)
		}
		return languageSwitchAnswer{
			status:      response.StatusCode,
			contentType: response.Header.Get(fiber.HeaderContentType),
			body:        body,
		}
	}
	requireFragment := func(name string, answer languageSwitchAnswer) {
		body := string(answer.body)
		if !strings.HasPrefix(answer.contentType, fiber.MIMETextHTML) || json.Valid(answer.body) ||
			!strings.Contains(body, `data-flash-key="failed to update interface settings"`) {
			t.Errorf("%s: answered 500 as %q (%q), want the text/html status fragment carrying the key",
				name, answer.contentType, body)
		}
	}

	answer := send("JSON caller", map[string]string{"Accept": fiber.MIMEApplicationJSON})
	if !strings.HasPrefix(answer.contentType, fiber.MIMEApplicationJSON) {
		t.Fatalf("JSON caller: answered 500 as %q, not application/json", answer.contentType)
	}
	var envelope struct {
		Error       string `json:"error"`
		ErrorDetail struct {
			Key      string `json:"key"`
			Category string `json:"category"`
			Target   string `json:"target"`
		} `json:"error_detail"`
	}
	if err := json.Unmarshal(answer.body, &envelope); err != nil || envelope.Error == "" || envelope.Error != envelope.ErrorDetail.Key {
		t.Fatalf("JSON caller: answered 500 without the shared error envelope: %q (%v)", answer.body, err)
	}
	requireSpecLine(t, failed, "schema: { $ref: '#/components/schemas/ApiError' }", "POST /lang 500")
	requireSpecLine(t, failed, fmt.Sprintf("error: %q", envelope.Error), "POST /lang 500")
	requireSpecLine(t, failed, fmt.Sprintf("error_detail: { key: %q, category: %q, target: %q }",
		envelope.ErrorDetail.Key, envelope.ErrorDetail.Category, envelope.ErrorDetail.Target), "POST /lang 500")

	requireFragment("form submission", send("form submission", nil))
	requireFragment("HTMX request", send("HTMX request", map[string]string{"HX-Request": "true", "Accept": fiber.MIMEApplicationJSON}))

	for _, phrase := range []string{"`HX-Request: true`", "`text/html`", "plain form submission", "`ovumcy_lang`"} {
		requireSpecMentions(t, failed, phrase, "POST /lang 500")
	}
}
