package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// Mutation-kill test for the tz-cookie normalization guard in LanguageMiddleware
// (middleware_language_helpers.go L16):
//
//	if timezoneCookieValue != "" && strings.TrimSpace(c.Cookies(...)) != timezoneCookieValue {
//	    handler.setTimezoneCookie(c, timezoneCookieValue)
//	}
//
// The guard persists a canonical timezone (derived from the request HEADER) into
// the ovumcy_tz cookie exactly when it is non-empty AND differs from the current
// cookie. The line carries two comparison operators; negating either
// (CONDITIONALS_NEGATION) either stops persisting a new zone or re-writes the
// cookie when it is already correct. "UTC" is used as the header value because it
// loads cross-platform without a tzdata database.
func newLanguageMiddlewareTestApp(t *testing.T) *fiber.App {
	t.Helper()

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler := &Handler{i18n: i18nManager, location: time.UTC}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/tzprobe", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	return app
}

// TestLanguageMiddlewarePersistsCanonicalTimezoneCookie pins the "set" arm: a
// valid tz header with no matching cookie must emit a Set-Cookie for ovumcy_tz.
// Both operator negations suppress the write, so the cookie would be absent.
func TestLanguageMiddlewarePersistsCanonicalTimezoneCookie(t *testing.T) {
	app := newLanguageMiddlewareTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/tzprobe", nil)
	req.Header.Set(timezoneHeaderName, "UTC")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("tz persist probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookie := responseCookie(resp.Cookies(), timezoneCookieName)
	if cookie == nil {
		t.Fatal("expected LanguageMiddleware to persist the canonical timezone cookie")
	}
	if cookie.Value != "UTC" {
		t.Fatalf("expected ovumcy_tz=UTC, got %q", cookie.Value)
	}
}

// TestLanguageMiddlewareSkipsRewriteWhenCookieAlreadyCanonical pins the
// second-operator arm: when the existing cookie already equals the canonical
// header value, the middleware must NOT re-issue the cookie. Negating
// `... != timezoneCookieValue` to `==` would redundantly re-write it.
func TestLanguageMiddlewareSkipsRewriteWhenCookieAlreadyCanonical(t *testing.T) {
	app := newLanguageMiddlewareTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/tzprobe", nil)
	req.Header.Set(timezoneHeaderName, "UTC")
	req.Header.Set("Cookie", timezoneCookieName+"=UTC")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("tz no-rewrite probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if cookie := responseCookie(resp.Cookies(), timezoneCookieName); cookie != nil {
		t.Fatalf("expected no ovumcy_tz re-write when cookie already canonical, got %q", cookie.Value)
	}
}
