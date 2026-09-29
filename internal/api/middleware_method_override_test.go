package api

import (
	"bytes"
	"compress/gzip"
	"log"
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
	return newMethodOverrideProbeAppWithAudit(t, withCSRF, false)
}

func newMethodOverrideProbeAppWithAudit(t *testing.T, withCSRF bool, auditLogEnabled bool) *fiber.App {
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
	handler.auditLogEnabled = auditLogEnabled

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
	app.Post("/content-type", func(c fiber.Ctx) error { return c.SendString(c.Get(fiber.HeaderContentType)) })
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

func methodOverrideRawRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, methodOverrideProbePath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	return request
}

// The binder decodes a percent-encoded key and matches it to the tag without
// regard to case, so the pre-check that skips the bind must not be fooled by
// either: each body below names _method to the binder without containing the
// literal lower-case key.
func TestMethodOverrideHonoursTheFieldWhateverItsSpelling(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	cases := []struct {
		body       string
		wantStatus int
		wantBody   string
	}{
		{"%5Fmethod=DELETE", http.StatusOK, "ran DELETE"},
		{"%5fmethod=PATCH", http.StatusOK, "ran PATCH"},
		{"_m%65thod=DELETE", http.StatusOK, "ran DELETE"},
		{"%5F%6D%65%74%68%6F%64=PUT", http.StatusOK, "ran PUT"},
		{"_METHOD=DELETE", http.StatusOK, "ran DELETE"},
		{"_Method=put", http.StatusOK, "ran PUT"},
		{"name=x&_mEtHoD=DELETE", http.StatusOK, "ran DELETE"},
		{"%5Fmethod=GET", http.StatusBadRequest, ""},
		{"_m%65thod=", http.StatusBadRequest, ""},
		{"_METHOD=GET", http.StatusBadRequest, ""},
		{"%5Fmethod=DELETE&_method=DELETE", http.StatusBadRequest, ""},
		// The binder collapses keys that differ only in case or encoding into
		// one value; the middleware counts them itself, so each spelling of a
		// second field is a repeat whatever the values, in either order.
		{"%5Fmethod=DELETE&_METHOD=DELETE", http.StatusBadRequest, ""},
		{"_method=DELETE&_METHOD=GET", http.StatusBadRequest, ""},
		{"_method=GET&_METHOD=DELETE", http.StatusBadRequest, ""},
		{"_METHOD=DELETE&_method=GET", http.StatusBadRequest, ""},
		{"_m%65thod=PUT&_Method=PUT&x=1", http.StatusBadRequest, ""},
		// A key the binder cannot map (unmatched bracket) fails the bind, but the
		// body is a plain urlencoded form all the same: the field still decides.
		{"_method=GET&a[=1", http.StatusBadRequest, ""},
		{"a[=1&_METHOD=GET", http.StatusBadRequest, ""},
		{"_method=DELETE&a[=1", http.StatusOK, "ran DELETE"},
		{"a[=1&_m%65thod=PUT", http.StatusOK, "ran PUT"},
		{"a[=1", http.StatusOK, "ran POST"},
		{"a]=1&x=2", http.StatusOK, "ran POST"},
	}
	for _, testCase := range cases {
		status, body := probeAnswer(t, app, methodOverrideRawRequest(testCase.body))
		if status != testCase.wantStatus || (testCase.wantBody != "" && body != testCase.wantBody) {
			t.Errorf("body %q: got %d %q, want %d %q", testCase.body, status, body, testCase.wantStatus, testCase.wantBody)
		}
	}
}

func TestMethodOverrideSkipsTheBindForABodyThatCannotNameTheField(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"":                       false,
		"name=x&other=y":         false,
		"csrf_token=abc":         false,
		"_meth=DELETE":           false,
		"method=DELETE":          false,
		"_method=DELETE":         true,
		"x=1&_Method=DELETE":     true,
		"_METHOD[]=DELETE":       true,
		"%5Fmethod=DELETE":       true,
		"_m%65thod=DELETE":       true,
		"name=100%25":            true, // any percent escape forces the bind
		"name=a+b&_meth%6Fd=PUT": true,
		"name=Ã©&_method=x":      true,
	}
	for body, want := range cases {
		if got := methodOverrideBodyMayNameTheField([]byte(body)); got != want {
			t.Errorf("methodOverrideBodyMayNameTheField(%q) = %v, want %v", body, got, want)
		}
	}

	// Observable through the request: the form binder folds a mixed-case
	// Content-Type in place, so a body that was bound comes out lower-cased and
	// one that was skipped keeps the spelling it arrived with.
	app := newMethodOverrideProbeApp(t, false)
	const mixedCase = "Application/X-WWW-Form-URLEncoded"
	request := httptest.NewRequest(http.MethodPost, "/content-type", strings.NewReader("name=x"))
	request.Header.Set("Content-Type", mixedCase)
	if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != mixedCase {
		t.Errorf("a body without the field was bound: got %d content-type %q, want it left as %q", status, body, mixedCase)
	}
	request = httptest.NewRequest(http.MethodPost, "/content-type", strings.NewReader("name=x&_method=POST"))
	request.Header.Set("Content-Type", mixedCase)
	if status, body := probeAnswer(t, app, request); status != http.StatusBadRequest {
		t.Errorf("_method=POST: got %d %q, want 400", status, body)
	}
}

// Not parallel: it swaps the process-wide log writer to read the audit line.
func TestMethodOverrideEmitsTheAppliedEventOnlyWhenItOverrides(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	app := newMethodOverrideProbeAppWithAudit(t, false, true)

	if status, body := probeAnswer(t, app, methodOverrideRawRequest("_method=delete")); status != http.StatusOK || body != "ran DELETE" {
		t.Fatalf("override: got %d %q, want 200 \"ran DELETE\"", status, body)
	}
	const applied = `security event: action="method_override" outcome="applied" method="DELETE"`
	if line := output.String(); !strings.Contains(line, applied) || !strings.Contains(line, `verb="DELETE"`) {
		t.Fatalf("applied override logged %q, want a line with %s and verb=\"DELETE\"", line, applied)
	}

	for name, request := range map[string]*http.Request{
		"no field":       methodOverrideRawRequest("name=x"),
		"encoded absent": methodOverrideRawRequest("name=100%25"),
		"real DELETE": func() *http.Request {
			request := httptest.NewRequest(http.MethodDelete, methodOverrideProbePath, strings.NewReader("_method=PUT"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return request
		}(),
	} {
		output.Reset()
		if status, body := probeAnswer(t, app, request); status != http.StatusOK {
			t.Fatalf("%s: got %d %q, want 200", name, status, body)
		}
		if strings.Contains(output.String(), `outcome="applied"`) {
			t.Errorf("%s logged an applied override: %q", name, output.String())
		}
	}

	output.Reset()
	if status, _ := probeAnswer(t, app, methodOverrideRawRequest("_method=GET")); status != http.StatusBadRequest {
		t.Fatalf("refused override: want 400, got %d", status)
	}
	if line := output.String(); !strings.Contains(line, `outcome="denied"`) || strings.Contains(line, `outcome="applied"`) {
		t.Errorf("refused override logged %q, want denied and never applied", line)
	}
}

// The bind folds a mixed-case Content-Type in place, so the /content-type echo
// tells whether the middleware read a body at all: a body it must leave alone
// comes back with the spelling it arrived with. Reading the field from a gzip or
// multipart body answers "ran POST" either way (fiber restores the raw bytes
// after decoding, and multipart parts are not in PostArgs), so the answer alone
// cannot pin the guard.
func TestMethodOverrideDoesNotReadABodyItMustLeaveAlone(t *testing.T) {
	t.Parallel()
	app := newMethodOverrideProbeApp(t, false)

	t.Run("content-encoded form body", func(t *testing.T) {
		const mixedCase = "Application/X-WWW-Form-URLEncoded"
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		_, _ = gz.Write([]byte("_method=DELETE"))
		_ = gz.Close()
		request := httptest.NewRequest(http.MethodPost, "/content-type", &compressed)
		request.Header.Set("Content-Type", mixedCase)
		request.Header.Set("Content-Encoding", "gzip")
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != mixedCase {
			t.Fatalf("got %d content-type %q, want the body left unread with %q", status, body, mixedCase)
		}
	})

	t.Run("multipart body", func(t *testing.T) {
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		_ = writer.WriteField("_method", "DELETE")
		_ = writer.Close()
		mixedCase := "Multipart/Form-Data; boundary=" + writer.Boundary()
		request := httptest.NewRequest(http.MethodPost, "/content-type", &payload)
		request.Header.Set("Content-Type", mixedCase)
		if status, body := probeAnswer(t, app, request); status != http.StatusOK || body != mixedCase {
			t.Fatalf("got %d content-type %q, want the body left unread with %q", status, body, mixedCase)
		}
	})
}
