package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

type bodyStringMatch struct {
	fragment string
	message  string
}

func mustAppResponse(t *testing.T, app *fiber.App, request *http.Request) *http.Response {
	t.Helper()

	response, err := app.Test(request, testConfigNoTimeout)
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

// assertNoSetCookie fails if response carries any Set-Cookie header. It gates
// on the RAW header values, never the parsed Cookies(): a malformed Set-Cookie
// value is dropped silently by Go's cookie parser, so a check gated on the
// parsed slice would miss the exact case this diagnostic exists to catch.
// label is the full description of what must not have set a cookie; the raw
// values are appended for diagnosis.
func assertNoSetCookie(t *testing.T, response *http.Response, label string) {
	t.Helper()

	if values := response.Header.Values("Set-Cookie"); len(values) != 0 {
		t.Fatalf("%s, got %q", label, values)
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

// assertSealedCookieCleared fails unless cookies carries a clearing Set-Cookie
// for name: an empty value, an Expires timestamp in the past (clearSealedCookie
// backdates by an hour), and a Path matching the one the cookie was issued
// under. Observing these attributes on the response itself is the point —
// inferring "cleared" from a later re-open returning empty only proves the
// reader rejects the value, never that the server actually told the browser
// to drop it.
func assertSealedCookieCleared(t *testing.T, cookies []*http.Cookie, name string, wantPath string) {
	t.Helper()

	cookie := responseCookie(cookies, name)
	if cookie == nil {
		t.Fatalf("expected a clearing Set-Cookie for %s", name)
	}
	if cookie.Value != "" {
		t.Fatalf("expected %s cleared with an empty value, got %q", name, cookie.Value)
	}
	// A zero Expires (no attribute at all, or one net/http failed to parse) must
	// not satisfy "cleared": require either an explicit negative Max-Age (net/http
	// parses Max-Age=0 as MaxAge -1, so MaxAge<0 covers both spellings) or a
	// non-zero Expires actually in the past. `!cookie.Expires.Before(time.Now())`
	// alone accepts the zero value, since the zero time.Time is always "before
	// now" — that hole is exactly what dropping the Expires line produces.
	if cookie.MaxAge >= 0 && (cookie.Expires.IsZero() || !cookie.Expires.Before(time.Now())) {
		t.Fatalf("expected %s cleared with an Expires in the past or a negative Max-Age, got Expires=%s MaxAge=%d", name, cookie.Expires, cookie.MaxAge)
	}
	if wantPath == "" {
		t.Fatal("assertSealedCookieCleared: wantPath must not be empty; every clearing cookie is issued on a specific path")
	}
	if cookie.Path != wantPath {
		t.Fatalf("expected %s cleared on path %q, got %q", name, wantPath, cookie.Path)
	}
}

func readAPIError(t *testing.T, body io.Reader) string {
	t.Helper()

	payload := struct {
		Error string `json:"error"`
	}{}
	bytes, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return payload.Error
}
