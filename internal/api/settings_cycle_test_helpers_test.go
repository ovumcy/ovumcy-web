package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func submitSettingsCycleUpdate(t *testing.T, app *fiber.App, authCookie string, csrfCookie *http.Cookie, csrfToken string, form url.Values) string {
	t.Helper()

	if strings.TrimSpace(csrfToken) != "" {
		form = cloneFormValues(form)
		form.Set("csrf_token", csrfToken)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")

	cookieHeader := authCookie
	if csrfCookie != nil {
		cookieHeader = joinCookieHeader(authCookie, cookiePair(csrfCookie))
	}
	request.Header.Set("Cookie", cookieHeader)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

func assertSettingsCycleHTMXSuccess(t *testing.T, body string) {
	t.Helper()

	if htmlElementByTagAndClass(mustParseHTMLDocument(t, body), "div", "status-ok") == nil {
		t.Fatalf("expected htmx success status markup")
	}
}

func cloneFormValues(source url.Values) url.Values {
	clone := url.Values{}
	for key, values := range source {
		for _, value := range values {
			clone.Add(key, value)
		}
	}
	return clone
}
