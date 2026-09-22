package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// Mutation-kill test for the tz-cookie normalization guard, moved by WEB-35
// out of LanguageMiddleware into resolveOwnerRequestTimezone
// (middleware_language_helpers.go):
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
//
// resolveOwnerRequestTimezone only runs once AuthRequired has verified a
// session (see TestUnauthenticatedRequestNeverResolvesTheTimezoneHeader in
// request_timezone_owner_gate_regression_test.go), so the probe route here
// calls it directly rather than through a session — it is the guard itself
// under test, not the auth gate in front of it.
func newTimezoneResolutionTestApp(t *testing.T) *fiber.App {
	t.Helper()

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler := &Handler{i18n: i18nManager, location: time.UTC}

	// A bare fiber.New() is deliberate here, where a wiring test would need the
	// shipped config: every path below is already spelled canonically, so the
	// router's CaseSensitive/StrictRouting flags cannot change which of them
	// reaches a route, and the assertions are about the cookie the resolver
	// writes before routing runs at all. The spellings the flags DO decide are
	// driven against the real chain in cmd/ovumcy.
	app := fiber.New()
	app.Get("/tzprobe", func(c fiber.Ctx) error {
		handler.resolveOwnerRequestTimezone(c)
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// TestResolveOwnerRequestTimezonePersistsCanonicalTimezoneCookie pins the
// "set" arm: a valid tz header with no matching cookie must emit a Set-Cookie
// for ovumcy_tz. Both operator negations suppress the write, so the cookie
// would be absent.
func TestResolveOwnerRequestTimezonePersistsCanonicalTimezoneCookie(t *testing.T) {
	app := newTimezoneResolutionTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/tzprobe", nil)
	req.Header.Set(timezoneHeaderName, "UTC")
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("tz persist probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookie := responseCookie(resp.Cookies(), timezoneCookieName)
	if cookie == nil {
		t.Fatal("expected resolveOwnerRequestTimezone to persist the canonical timezone cookie")
	}
	if cookie.Value != "UTC" {
		t.Fatalf("expected ovumcy_tz=UTC, got %q", cookie.Value)
	}
}

// TestResolveOwnerRequestTimezoneSkipsRewriteWhenCookieAlreadyCanonical pins
// the second-operator arm: when the existing cookie already equals the
// canonical header value, the resolver must NOT re-issue the cookie. Negating
// `... != timezoneCookieValue` to `==` would redundantly re-write it.
func TestResolveOwnerRequestTimezoneSkipsRewriteWhenCookieAlreadyCanonical(t *testing.T) {
	app := newTimezoneResolutionTestApp(t)

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
// return the calendar-feed prefix takes at the top of LanguageMiddleware:
// without it, the feed route would run language resolution meant for a
// session-bearing caller, on a route a real calendar client reaches with none
// of the signals that resolution reads (docs/SECURITY_INVARIANTS.md →
// Calendar feed subscription). The timezone half this used to also guard
// moved out under WEB-35 into resolveOwnerRequestTimezone, gated by
// AuthRequired — the feed route never reaches it regardless of this early
// return, since it carries no AuthRequired in its chain — so this test is
// narrowed to what LanguageMiddleware itself still decides: whether the
// request reaches the ordinary branch that resolves the language cookie and
// message catalogue at all.
func TestLanguageMiddlewareSkipsTheCookielessCalendarFeedEntirely(t *testing.T) {
	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	handler := &Handler{i18n: i18nManager, location: time.UTC}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get(CalendarFeedRateLimitPrefix+"/:token.ics", func(c fiber.Ctx) error {
		_, ok := c.Locals(contextMessagesKey).(map[string]string)
		return c.SendString(strconv.FormatBool(ok))
	})
	// A neighbour that shares the feed prefix's characters but not its
	// separator: hypothetical today; the control below keeps the early return
	// from ever claiming it.
	app.Get(CalendarFeedRateLimitPrefix+"back", func(c fiber.Ctx) error {
		_, ok := c.Locals(contextMessagesKey).(map[string]string)
		return c.SendString(strconv.FormatBool(ok))
	})

	req := httptest.NewRequest(http.MethodGet, CalendarFeedRateLimitPrefix+"/"+strings.Repeat("A", 48)+".ics", nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("feed language probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body := mustReadBodyString(t, resp.Body); body != "false" {
		t.Fatalf("expected the calendar feed route to skip language resolution entirely, got locals-set=%s", body)
	}

	// Control for the prefix boundary: a path that begins with the same
	// characters but is not under the feed's own subtree must still be served
	// by the middleware, or the early return has widened from the feed route
	// to everything that happens to start with its prefix.
	neighbour := httptest.NewRequest(http.MethodGet, CalendarFeedRateLimitPrefix+"back", nil)
	neighbourResp, err := app.Test(neighbour, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("neighbour language probe failed: %v", err)
	}
	defer func() { _ = neighbourResp.Body.Close() }()
	if body := mustReadBodyString(t, neighbourResp.Body); body != "true" {
		t.Fatalf("expected %sback to keep receiving language resolution — the feed skip must not over-match its prefix, got locals-set=%s", CalendarFeedRateLimitPrefix, body)
	}
}
