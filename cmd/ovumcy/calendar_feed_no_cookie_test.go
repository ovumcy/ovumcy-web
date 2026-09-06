package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// newCalendarFeedTestApp builds the app exactly as the production composition
// root does (newFiberApp: full middleware chain, the real route table, the
// NotFound catch-all) over the given handler, with every rate limit except the
// feed's own set high enough that the probes in this file cannot trip one and
// be mistaken for the cookie defect this file exists to catch. feedMax lets a
// 429 case share this same builder instead of duplicating the whole config.
func newCalendarFeedTestApp(t *testing.T, handler *api.Handler, feedMax int) *fiber.App {
	t.Helper()

	config := runtimeConfig{
		Location:        time.UTC,
		DefaultLanguage: "en",
		CookieSecure:    false,
		RateLimits: rateLimitSettings{
			LoginMax:             100000,
			LoginWindow:          time.Hour,
			ForgotPasswordMax:    100000,
			ForgotPasswordWindow: time.Hour,
			RegisterMax:          100000,
			RegisterWindow:       time.Hour,
			LogoutMax:            100000,
			LogoutWindow:         time.Hour,
			APIMax:               100000,
			APIWindow:            time.Hour,
			CalendarFeedMax:      feedMax,
			CalendarFeedWindow:   time.Hour,
		},
	}
	return newFiberApp(config, handler)
}

// newCalendarFeedNoCookieTestApp is newCalendarFeedTestApp over a fresh
// handler, with the feed's own limiter budget also set high — for the probes
// in this file that drive several requests without meaning to exercise 429.
func newCalendarFeedNoCookieTestApp(t *testing.T) *fiber.App {
	t.Helper()

	return newCalendarFeedTestApp(t, newRateLimitTestHandler(t), 100000)
}

// armCalendarFeedToken mints a real calendar-feed token and saves it for
// userID against database, using the same secret key newRateLimitTestHandler
// builds its handler with (rateLimitTestSecretKey) — so a request bearing the
// returned token resolves through that handler's own CalendarFeedService.
func armCalendarFeedToken(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()

	token, columns, err := services.GenerateCalendarFeedToken([]byte(rateLimitTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if err := db.NewRepositories(database).Users.SaveCalendarFeedToken(context.Background(), userID, columns); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}
	return token
}

// TestCalendarFeedRouteSetsNoCookieOnTheProductionStack pins
// docs/SECURITY_INVARIANTS.md's "Calendar feed subscription" claim that the
// feed carries no `Set-Cookie` on any outcome: internal/api/handlers_calendar_feed.go
// documents the same thing on ServeCalendarFeed ("It never sets a cookie"),
// but that promise is the HANDLER's; two app-wide middlewares mounted ahead of
// it in configureFiberMiddleware — csrf.New and LanguageMiddleware — run for
// EVERY safe-method request that lacks a matching cookie, calendar clients
// included, and each mints one of its own regardless of what the handler
// later returns.
//
// The probe token is well-formed (16-char selector + 32-char verifier, see
// calendarFeedTokenLength in internal/services) but resolves no user, so every
// case answers the bare 404 the feed gives every unknown/malformed/revoked
// token. That is deliberate, not a shortcut taken to avoid arming a real feed:
// both cookies are written by middleware mounted AHEAD of ServeCalendarFeed, so
// their presence or absence is decided before the handler runs and is
// identical whether it goes on to answer 200, 404 or 500 — the 404 path here
// exercises the exact same middleware pass a 200 would.
func TestCalendarFeedRouteSetsNoCookieOnTheProductionStack(t *testing.T) {
	app := newCalendarFeedNoCookieTestApp(t)
	feedTarget := "/calendar/feed/" + strings.Repeat("A", 48) + ".ics"

	cases := []struct {
		name      string
		method    string // empty = GET
		path      string // empty = feedTarget
		configure func(*http.Request)
	}{
		{
			name:      "no headers",
			configure: func(*http.Request) {},
		},
		{
			// The header a real calendar client never sends, but which the CSRF
			// exemption below must not depend on the client omitting: it also
			// drives LanguageMiddleware's setTimezoneCookie side effect.
			name: "with X-Ovumcy-Timezone header",
			configure: func(r *http.Request) {
				r.Header.Set("X-Ovumcy-Timezone", "Europe/Berlin")
			},
		},
		{
			name: "with Accept-Language header",
			configure: func(r *http.Request) {
				r.Header.Set("Accept-Language", "de")
			},
		},
		{
			// A stale ovumcy_tz cookie with no header: LanguageMiddleware's own
			// normalization guard would not rewrite this cookie either way, so
			// this case isolates the CSRF cookie from the timezone one.
			name: "with a pre-existing ovumcy_tz cookie and no header",
			configure: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "ovumcy_tz", Value: "Europe/Berlin"})
			},
		},
		// The four spellings below are exactly routableSpellings(feedTarget)
		// (rate_limit_scope_guard_test.go): fiberConfig ships with CaseSensitive
		// and StrictRouting both off, so the router folds case and trailing
		// slashes away before matching, and a predicate comparing c.Path() raw
		// claims fewer spellings than the router actually dispatches here.
		{
			name:      "uppercase spelling",
			path:      strings.ToUpper(feedTarget),
			configure: func(*http.Request) {},
		},
		{
			name:      "trailing slash",
			path:      feedTarget + "/",
			configure: func(*http.Request) {},
		},
		{
			name:      "uppercase spelling with trailing slash",
			path:      strings.ToUpper(feedTarget) + "/",
			configure: func(*http.Request) {},
		},
		{
			name:      "double trailing slash",
			path:      feedTarget + "//",
			configure: func(*http.Request) {},
		},
		{
			name:      "uppercase route prefix, lowercase token",
			path:      "/CALENDAR/FEED/" + strings.Repeat("A", 48) + ".ics",
			configure: func(*http.Request) {},
		},
		{
			// app.Get also registers HEAD; the CSRF Next clause historically
			// checked only GET, so a HEAD request minted a CSRF cookie the GET
			// cases above never revealed.
			name:      "HEAD instead of GET",
			method:    http.MethodHead,
			configure: func(*http.Request) {},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			method := testCase.method
			if method == "" {
				method = http.MethodGet
			}
			target := testCase.path
			if target == "" {
				target = feedTarget
			}
			request := httptest.NewRequest(method, target, nil)
			testCase.configure(request)

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("feed request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("expected the feed's bare 404 for an unresolvable token, got %d — fix the probe before trusting the cookie assertion below", response.StatusCode)
			}
			if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 0 {
				t.Fatalf("calendar feed route must never set a cookie on any outcome, got Set-Cookie: %v", cookies)
			}
		})
	}

	// Positive anchor: every case above is a negative assertion, which would
	// pass just as well if the CSRF/timezone-cookie machinery were dead
	// app-wide rather than specifically excluded for the feed. Prove it is
	// alive, on the SAME app instance, against an ordinary unauthenticated
	// page that carries no such exclusion.
	t.Run("control: an ordinary page still gets both cookies", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
		request.Header.Set("X-Ovumcy-Timezone", "Europe/Berlin")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("control page request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if testResponseCookie(response.Cookies(), "ovumcy_csrf") == nil {
			t.Fatal("expected /privacy to mint a CSRF cookie — if it doesn't, the feed's cookieless cases above prove nothing")
		}
		if cookie := testResponseCookie(response.Cookies(), "ovumcy_tz"); cookie == nil || cookie.Value != "Europe/Berlin" {
			t.Fatal("expected /privacy to persist the timezone cookie — if it doesn't, the feed's cookieless cases above prove nothing")
		}
	})

	// Boundary of the exclusion, on the same production stack: a path that
	// continues every character of the feed prefix without its separator is
	// not the feed. No route answers it, so the NotFound catch-all does — and
	// that 404 must still carry both cookies, or the exclusion has widened
	// from the feed route to whatever happens to start with its prefix.
	t.Run("control: a neighbour continuing the prefix's characters still gets both cookies", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/calendar/feedback", nil)
		request.Header.Set("X-Ovumcy-Timezone", "Europe/Berlin")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("neighbour request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("expected the catch-all 404 for a path no route registers, got %d — fix the probe before trusting the cookie assertions below", response.StatusCode)
		}
		if testResponseCookie(response.Cookies(), "ovumcy_csrf") == nil {
			t.Fatal("expected /calendar/feedback to mint a CSRF cookie — the feed's CSRF skip must not over-match its prefix")
		}
		if cookie := testResponseCookie(response.Cookies(), "ovumcy_tz"); cookie == nil || cookie.Value != "Europe/Berlin" {
			t.Fatal("expected /calendar/feedback to persist the timezone cookie — LanguageMiddleware's feed skip must not over-match its prefix")
		}
	})
}

// TestCalendarFeedRouteSetsNoCookieForAnArmedFeed is the 200 leg
// TestCalendarFeedRouteSetsNoCookieOnTheProductionStack's own doc comment
// says the SECURITY.md claim ("no Set-Cookie on any outcome — 200, 404, or
// 429") only had 404 coverage: both cookies are written by middleware mounted
// AHEAD of ServeCalendarFeed, so this behaves identically to the 404 cases —
// this test exists to make the SECURITY.md citation true, not because a real
// gap was found on the success path.
func TestCalendarFeedRouteSetsNoCookieForAnArmedFeed(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-armed@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)
	app := newCalendarFeedTestApp(t, handler, 100000)

	request := httptest.NewRequest(http.MethodGet, "/calendar/feed/"+token+".ics", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("armed feed request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		body := mustReadAll(t, response)
		t.Fatalf("expected 200 for a freshly armed feed, got %d (body %q) — fix the probe before trusting the cookie assertion below", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/calendar") {
		t.Fatalf("expected a text/calendar body from ServeCalendarFeed, got Content-Type %q — fix the probe before trusting the cookie assertion below", contentType)
	}
	if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("an armed calendar feed must never set a cookie on its 200, got Set-Cookie: %v", cookies)
	}
}

// TestCalendarFeedRouteSetsNoCookieOn429 is the 429 leg — see
// TestCalendarFeedRouteSetsNoCookieForAnArmedFeed's doc comment for why this
// is a coverage addition rather than a currently-reachable defect: the
// limiter is mounted ahead of both cookie-minting middlewares in
// configureFiberMiddleware and answers the 429 without ever calling c.Next(),
// so neither one runs once the budget is spent.
func TestCalendarFeedRouteSetsNoCookieOn429(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-limited@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)
	app := newCalendarFeedTestApp(t, handler, 1)
	feedTarget := "/calendar/feed/" + token + ".ics"

	first := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	firstResponse, err := app.Test(first, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("first feed request failed: %v", err)
	}
	_ = firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the first request inside the budget of 1 to succeed, got %d", firstResponse.StatusCode)
	}

	second := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	secondResponse, err := app.Test(second, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("second feed request failed: %v", err)
	}
	defer func() { _ = secondResponse.Body.Close() }()

	if secondResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the per-IP budget (1) is spent, got %d — fix the probe before trusting the cookie assertion below", secondResponse.StatusCode)
	}
	if cookies := secondResponse.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("the calendar feed's rate limiter must never set a cookie on its 429, got Set-Cookie: %v", cookies)
	}
}

// TestCalendarFeedRateLimiterScopedToTheRouteShapeNotTheBarePrefix covers the
// limiter half of the same predicate class: app.Use(api.CalendarFeedRateLimitPrefix, ...)
// is prefix-matched and method-agnostic, so without a Next scoped to
// api.IsCalendarFeedRequest it would also spend this small, bcrypt-sized
// budget on a bare "/calendar/feed/" that reaches no bcrypt compare at all.
func TestCalendarFeedRateLimiterScopedToTheRouteShapeNotTheBarePrefix(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-rl-scope@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)
	app := newCalendarFeedTestApp(t, handler, 1)

	const overMatchPath = "/calendar/feed/"
	for i := range 3 {
		request := httptest.NewRequest(http.MethodGet, overMatchPath, nil)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("over-match request %d failed: %v", i, err)
		}
		_ = response.Body.Close()
		if response.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("over-match request %d to %s was rate-limited by the feed's own limiter — its scope predicate has widened to the bare prefix", i, overMatchPath)
		}
	}

	// Positive anchor, same app, same tiny budget: a REAL feed URL must still
	// be capped, or the assertion above would pass just as well with the
	// limiter dead rather than correctly scoped.
	feedTarget := "/calendar/feed/" + token + ".ics"
	first := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	firstResponse, err := app.Test(first, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("first canonical request failed: %v", err)
	}
	_ = firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the first canonical request inside the budget of 1 to succeed, got %d", firstResponse.StatusCode)
	}
	second := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	secondResponse, err := app.Test(second, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("second canonical request failed: %v", err)
	}
	defer func() { _ = secondResponse.Body.Close() }()
	if secondResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the canonical feed URL to still be capped at 429 once its own budget (1) is spent, got %d — if it doesn't, the over-match case above proves nothing", secondResponse.StatusCode)
	}
}

// TestCalendarFeedOverMatchPathsKeepLanguageCatalogueAndCSRFSupport covers the
// OTHER half of the same predicate defect: a path that shares the feed's
// prefix but not its route shape (no route answers it, so it falls to the
// NotFound catch-all) must be treated as an ORDINARY page, not swept into the
// feed's middleware exclusion. Over-matching here does not leak a cookie (the
// feed's contract does not apply to a non-feed path), but it silently drops
// this page's language catalogue (the raw i18n key renders literally) and its
// CSRF token (the language-switch form ships an empty one, so submitting it
// answers 403).
func TestCalendarFeedOverMatchPathsKeepLanguageCatalogueAndCSRFSupport(t *testing.T) {
	app := newCalendarFeedNoCookieTestApp(t)

	// The raw key rendered as TEXT CONTENT, not the static data-title-key
	// attribute the template also carries (which spells this same string
	// unconditionally, translated or not — a substring search alone would
	// match that attribute on every render and prove nothing).
	const rawTitleAsText = ">not_found.title<"

	cases := []string{
		"/calendar/feed/",
		"/calendar/feed",
		"/calendar/feed/a/b.ics",
		"/calendar/feed/.ics",
		// Control: a genuine neighbour that shares no route with the feed. If
		// THIS case also failed, the assertions below would be worthless — it
		// would mean the not_found page never carries a catalogue or a CSRF
		// token on this app, feed-adjacent or not.
		"/calendar/feedback",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("expected the catch-all 404 for %s, got %d — fix the probe before trusting the assertions below", path, response.StatusCode)
			}
			body := string(mustReadAll(t, response))

			if strings.Contains(body, rawTitleAsText) {
				t.Errorf("%s rendered the raw i18n key as its title text instead of a resolved catalogue entry — LanguageMiddleware's calendar-feed skip over-matched this path", path)
			}
			if !csrfTokenMetaPattern.MatchString(body) {
				t.Errorf("%s rendered no non-empty csrf-token meta tag — the CSRF middleware's calendar-feed skip over-matched this path", path)
			}
		})
	}
}
