package api

import (
	"bytes"
	"compress/gzip"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

const methodOverrideProbePath = "/probe"

// newMethodOverrideProbeApp mounts MethodOverride first, the way the
// composition root does, optionally followed by CSRF, in front of one path
// registered under every verb a form could reach. Each route answers with the
// verb that ran it, so a test reads the routing decision straight off the body.
func newMethodOverrideProbeApp(t *testing.T, withCSRF bool) *fiber.App {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "method-override.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler, err := NewHandler(testAppSecretKey, time.UTC, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New(fiber.Config{BodyLimit: 4 * 1024})
	app.Use(MethodOverride(handler))
	if withCSRF {
		app.Use(csrf.New(testCSRFMiddlewareConfig(false, handler)))
	}
	app.Get("/token", func(c fiber.Ctx) error {
		return c.SendString(csrf.TokenFromContext(c))
	})
	answer := func(c fiber.Ctx) error { return c.SendString("ran " + c.Method()) }
	app.Post(methodOverrideProbePath, answer)
	app.Put(methodOverrideProbePath, answer)
	app.Patch(methodOverrideProbePath, answer)
	app.Delete(methodOverrideProbePath, answer)
	app.Delete("/delete-only", answer)
	app.Post(security.OIDCCallbackPath, answer)
	return app
}

func methodOverrideFormRequest(target string, form url.Values, contentType string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	return request
}

func probeAnswer(t *testing.T, app *fiber.App, request *http.Request) (int, string) {
	t.Helper()
	response := mustAppResponse(t, app, request)
	return response.StatusCode, mustReadBodyString(t, response.Body)
}

func TestMethodOverrideRoutesAFormPostAsTheAllowlistedVerb(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	cases := map[string]string{
		"DELETE":   "DELETE",
		"delete":   "DELETE",
		" Put ":    "PUT",
		"patch":    "PATCH",
		"PATCH":    "PATCH",
		"dElEtE\t": "DELETE",
	}
	for field, want := range cases {
		request := methodOverrideFormRequest(methodOverrideProbePath, url.Values{"_method": {field}}, "application/x-www-form-urlencoded")
		status, body := probeAnswer(t, app, request)
		if status != http.StatusOK || body != "ran "+want {
			t.Errorf("_method=%q: got %d %q, want 200 %q", field, status, body, "ran "+want)
		}
	}

	// A route registered ONLY under the overridden verb is reached: routing
	// happens after the override, not before it.
	request := methodOverrideFormRequest("/delete-only", url.Values{"_method": {"DELETE"}}, "application/x-www-form-urlencoded")
	if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran DELETE" {
		t.Fatalf("delete-only route: got %d %q, want 200 \"ran DELETE\"", status, body)
	}
	// And the 405 a wrong verb earns is judged against the overridden verb.
	request = methodOverrideFormRequest("/delete-only", url.Values{"_method": {"PUT"}}, "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != "DELETE" {
		t.Fatalf("PUT override on a DELETE-only path: got %d Allow=%q, want 405 Allow=DELETE", response.StatusCode, response.Header.Get("Allow"))
	}
}

func TestMethodOverrideRefusesAVerbOutsideTheAllowlist(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	for _, field := range []string{"GET", "get", "HEAD", "OPTIONS", "CONNECT", "TRACE", "POST", "QUERY", "", "   ", "DELETE PUT", "PURGE", "DEL\x00ETE"} {
		request := methodOverrideFormRequest(methodOverrideProbePath, url.Values{"_method": {field}}, "application/x-www-form-urlencoded")
		status, body := probeAnswer(t, app, request)
		if status != http.StatusBadRequest {
			t.Errorf("_method=%q: got %d %q, want 400", field, status, body)
		}
		if strings.HasPrefix(body, "ran ") {
			t.Errorf("_method=%q reached a route: %q", field, body)
		}
		if !strings.Contains(body, `"error"`) {
			t.Errorf("_method=%q: want the app's error envelope, got %q", field, body)
		}
	}

	ambiguous := methodOverrideFormRequest(methodOverrideProbePath, url.Values{"_method": {"DELETE", "DELETE"}}, "application/x-www-form-urlencoded")
	if status, body := probeAnswer(t, app, ambiguous); status != http.StatusBadRequest {
		t.Fatalf("repeated _method: got %d %q, want 400", status, body)
	}
}

func TestMethodOverrideLeavesEverythingButAFormPostAlone(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	t.Run("no field is a plain POST", func(t *testing.T) {
		request := methodOverrideFormRequest(methodOverrideProbePath, url.Values{"name": {"x"}}, "application/x-www-form-urlencoded")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("query string is never read", func(t *testing.T) {
		request := methodOverrideFormRequest(methodOverrideProbePath+"?_method=DELETE", url.Values{"name": {"x"}}, "application/x-www-form-urlencoded")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
		request = methodOverrideFormRequest(methodOverrideProbePath+"?_method=GET", url.Values{}, "application/x-www-form-urlencoded")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("query _method=GET: got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("JSON body", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, methodOverrideProbePath, strings.NewReader(`{"_method":"DELETE"}`))
		request.Header.Set("Content-Type", "application/json")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("multipart body", func(t *testing.T) {
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		_ = writer.WriteField("_method", "DELETE")
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, methodOverrideProbePath, &payload)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("content-encoded form body is not decoded", func(t *testing.T) {
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		_, _ = gz.Write([]byte("_method=DELETE"))
		_ = gz.Close()
		request := httptest.NewRequest(http.MethodPost, methodOverrideProbePath, &compressed)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Content-Encoding", "gzip")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("a look-alike media type", func(t *testing.T) {
		request := methodOverrideFormRequest(methodOverrideProbePath, url.Values{"_method": {"DELETE"}}, "application/x-www-form-urlencodedx")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran POST" {
			t.Fatalf("got %d %q, want 200 \"ran POST\"", status, body)
		}
	})

	t.Run("real verbs keep their verb", func(t *testing.T) {
		for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
			request := httptest.NewRequest(method, methodOverrideProbePath, strings.NewReader("_method=POST"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != "ran "+method {
				t.Fatalf("%s with _method=POST: got %d %q, want 200 \"ran %s\"", method, status, body, method)
			}
		}
	})
}

// csrfProbePair mints a CSRF cookie and its token from the probe app.
func csrfProbePair(t *testing.T, app *fiber.App) (string, string) {
	t.Helper()
	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/token", nil))
	token := mustReadBodyString(t, response.Body)
	cookie := responseCookie(response.Cookies(), "ovumcy_csrf")
	if cookie == nil || token == "" {
		t.Fatal("probe app minted no CSRF pair")
	}
	return cookiePair(cookie), token
}

func TestMethodOverrideIsValidatedByCSRFAsTheOverriddenVerb(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, true)
	cookie, token := csrfProbePair(t, app)

	send := func(target string, form url.Values, contentType string, cookieHeader string) (int, string) {
		request := methodOverrideFormRequest(target, form, contentType)
		if cookieHeader != "" {
			request.Header.Set("Cookie", cookieHeader)
		}
		return probeAnswer(t, app, request)
	}

	if status, body := send(methodOverrideProbePath, url.Values{"_method": {"DELETE"}, "csrf_token": {token}}, "application/x-www-form-urlencoded", cookie); status != http.StatusOK || body != "ran DELETE" {
		t.Fatalf("with a valid pair: got %d %q, want 200 \"ran DELETE\"", status, body)
	}
	if status, body := send(methodOverrideProbePath, url.Values{"_method": {"DELETE"}}, "application/x-www-form-urlencoded", cookie); status != http.StatusForbidden {
		t.Fatalf("without a token: got %d %q, want 403", status, body)
	}
	if status, body := send(methodOverrideProbePath, url.Values{"_method": {"DELETE"}, "csrf_token": {token + "x"}}, "application/x-www-form-urlencoded", cookie); status != http.StatusForbidden {
		t.Fatalf("with a mismatched token: got %d %q, want 403", status, body)
	}
	if status, body := send(methodOverrideProbePath, url.Values{"_method": {"DELETE"}, "csrf_token": {token}}, "application/x-www-form-urlencoded", ""); status != http.StatusForbidden {
		t.Fatalf("without the cookie: got %d %q, want 403", status, body)
	}

	// The OIDC callback's POST exemption must not carry over to a request the
	// override turned into something else: CSRF answers first, not the router.
	if status, body := send(security.OIDCCallbackPath, url.Values{"_method": {"DELETE"}}, "application/x-www-form-urlencoded", ""); status != http.StatusForbidden {
		t.Fatalf("OIDC callback POST with _method=DELETE: got %d %q, want 403", status, body)
	}

	// A mixed-case Content-Type is still a form for both the override and the
	// CSRF extractor: reading _method must not leave the body parsed as empty.
	if status, body := send(methodOverrideProbePath, url.Values{"_method": {"DELETE"}, "csrf_token": {token}}, "Application/X-WWW-Form-URLEncoded; charset=UTF-8", cookie); status != http.StatusOK || body != "ran DELETE" {
		t.Fatalf("mixed-case form type: got %d %q, want 200 \"ran DELETE\"", status, body)
	}
}

func TestMethodOverrideReadsNoBodyPastTheBodyLimit(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	form := url.Values{"_method": {"DELETE"}, "pad": {strings.Repeat("a", 8*1024)}}
	request := methodOverrideFormRequest(methodOverrideProbePath, form, "application/x-www-form-urlencoded")
	// fasthttp refuses the over-limit wire body while reading the request, so
	// the override never sees it: app.Test surfaces that refusal as its error.
	response, err := app.Test(request, testConfigNoTimeout)
	if err == nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("over-limit form: got %d, want the request refused before routing", response.StatusCode)
		}
		return
	}
	if !strings.Contains(err.Error(), "body size exceeds") {
		t.Fatalf("over-limit form: unexpected error %v", err)
	}
}
