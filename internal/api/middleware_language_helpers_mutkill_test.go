package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	app.Get(CalendarFeedRateLimitPrefix+"/:token.ics", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	// A neighbour that shares the feed prefix's characters but not its
	// separator: hypothetical today; the control below keeps the early return
	// from ever claiming it.
	app.Get(CalendarFeedRateLimitPrefix+"back", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
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

// TestLanguageMiddlewareSkipsTheCookielessCalendarFeedEntirely pins the early
// return the calendar-feed prefix takes at the top of LanguageMiddleware.
// Without it, the exact header that proves the "set" arm above (a valid tz
// header with no matching cookie) would just as reliably persist ovumcy_tz on
// the feed route — a Set-Cookie the cookieless feed's own contract forbids on
// every outcome (docs/SECURITY_INVARIANTS.md → Calendar feed subscription). This is
// a mutation-kill for the early-return guard itself: deleting it, or scoping
// it to the wrong path, leaves this red while the two tests above stay green,
// since neither of them drives a request against the feed prefix.
func TestLanguageMiddlewareSkipsTheCookielessCalendarFeedEntirely(t *testing.T) {
	app := newLanguageMiddlewareTestApp(t)

	req := httptest.NewRequest(http.MethodGet, CalendarFeedRateLimitPrefix+"/"+strings.Repeat("A", 48)+".ics", nil)
	req.Header.Set(timezoneHeaderName, "UTC")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("feed tz probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if cookie := responseCookie(resp.Cookies(), timezoneCookieName); cookie != nil {
		t.Fatalf("expected the calendar feed route to never receive a timezone cookie, got %q", cookie.Value)
	}

	// Control for the prefix boundary: a path that begins with the same
	// characters but is not under the feed's own subtree must still be served
	// by the middleware, or the early return has widened from the feed route
	// to everything that happens to start with its prefix.
	neighbour := httptest.NewRequest(http.MethodGet, CalendarFeedRateLimitPrefix+"back", nil)
	neighbour.Header.Set(timezoneHeaderName, "UTC")
	neighbourResp, err := app.Test(neighbour, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("neighbour tz probe failed: %v", err)
	}
	defer func() { _ = neighbourResp.Body.Close() }()
	if cookie := responseCookie(neighbourResp.Cookies(), timezoneCookieName); cookie == nil || cookie.Value != "UTC" {
		t.Fatalf("expected %sback to keep receiving the timezone cookie — the feed skip must not over-match its prefix, got %#v", CalendarFeedRateLimitPrefix, cookie)
	}
}
