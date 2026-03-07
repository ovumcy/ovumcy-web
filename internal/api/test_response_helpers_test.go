package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type bodyStringMatch struct {
	fragment string
	message  string
}

func mustAppResponse(t *testing.T, app *fiber.App, request *http.Request) *http.Response {
	t.Helper()

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func mustReadBodyString(t *testing.T, body io.Reader) string {
	t.Helper()

	bytes, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(bytes)
}

func assertStatusCode(t *testing.T, response *http.Response, expected int) {
	t.Helper()

	if response.StatusCode != expected {
		t.Fatalf("expected status %d, got %d", expected, response.StatusCode)
	}
}

func mustParseLocationHeader(t *testing.T, response *http.Response) *url.URL {
	t.Helper()

	location := response.Header.Get("Location")
	if location == "" {
		t.Fatal("expected redirect location")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	return parsed
}

func assertBodyContainsAll(t *testing.T, body string, matches ...bodyStringMatch) {
	t.Helper()

	for _, match := range matches {
		if !strings.Contains(body, match.fragment) {
			t.Fatal(match.message)
		}
	}
}

func assertBodyNotContainsAll(t *testing.T, body string, matches ...bodyStringMatch) {
	t.Helper()

	for _, match := range matches {
		if strings.Contains(body, match.fragment) {
			t.Fatal(match.message)
		}
	}
}

func responseCookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func responseCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func readAPIError(t *testing.T, body io.Reader) string {
	t.Helper()

	payload := map[string]string{}
	bytes, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return payload["error"]
}
