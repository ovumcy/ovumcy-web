package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
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

	settings := uniformRateLimits(t, 100000, time.Hour)
	settings.CalendarFeedMax = feedMax
	config := runtimeConfig{
		Location:        time.UTC,
		DefaultLanguage: "en",
		CookieSecure:    false,
		RateLimits:      settings,
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

// assertNoSetCookie fails the test if response carries a Set-Cookie header.
// outcome names the case under test ("any outcome", "its 200", "its 429",
// "the plain request") so every call site in this file keeps its own
// diagnostic without repeating the header lookup and format string.
func assertNoSetCookie(t *testing.T, response *http.Response, outcome string) {
	t.Helper()

	if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("calendar feed route must never set a cookie on %s, got Set-Cookie: %v", outcome, cookies)
	}
}

// calendarFeedNoCookieCase is one probe TestCalendarFeedRouteSetsNoCookieOnTheProductionStack
// drives against the production stack.
type calendarFeedNoCookieCase struct {
	name      string
	method    string // empty = GET
	path      string // empty = feedTarget
	configure func(*http.Request)
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
	feedTarget := api.CalendarFeedRateLimitPrefix + "/" + strings.Repeat("A", 48) + ".ics"

	cases := []calendarFeedNoCookieCase{
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
		{
			name:      "uppercase route prefix, lowercase token",
			path:      "/CALENDAR/FEED/" + strings.Repeat("A", 48) + ".ics",
			configure: func(*http.Request) {},
		},
		{
			// Every GET route is served on HEAD too; the CSRF Next clause
			// historically checked only GET, so a HEAD request minted a CSRF
			// cookie the GET cases above never revealed. This case proves no
			// Set-Cookie on HEAD, and it now measures the same handler the GET
			// cases measure: api.RegisterRoutes registers the HEAD twin of
			// every GET route ahead of the terminal NotFound catch-all, so a
			// HEAD to this path reaches ServeCalendarFeed and answers this
			// unresolvable token with the feed's own 404 — the same status the
			// catch-all used to give it, for a different reason.
			name:      "HEAD instead of GET",
			method:    http.MethodHead,
			configure: func(*http.Request) {},
		},
	}
	// fiberConfig ships with CaseSensitive and StrictRouting both off, so the
	// router folds case and trailing slashes away before matching a route, and
	// a predicate comparing c.Path() raw claims fewer spellings than the
	// router actually dispatches here — the exact set routableSpellings
	// (rate_limit_scope_guard_test.go) returns.
	for _, spelling := range routableSpellings(feedTarget) {
		cases = append(cases, calendarFeedNoCookieCase{
			name:      "routable spelling " + spelling,
			path:      spelling,
			configure: func(*http.Request) {},
		})
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
			assertNoSetCookie(t, response, "any outcome")
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
		neighbour := api.CalendarFeedRateLimitPrefix + "back"
		request := httptest.NewRequest(http.MethodGet, neighbour, nil)
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
			t.Fatalf("expected %s to mint a CSRF cookie — the feed's CSRF skip must not over-match its prefix", neighbour)
		}
		if cookie := testResponseCookie(response.Cookies(), "ovumcy_tz"); cookie == nil || cookie.Value != "Europe/Berlin" {
			t.Fatalf("expected %s to persist the timezone cookie — LanguageMiddleware's feed skip must not over-match its prefix", neighbour)
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

	request := httptest.NewRequest(http.MethodGet, api.CalendarFeedRateLimitPrefix+"/"+token+".ics", nil)
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
	assertNoSetCookie(t, response, "its 200")
}

// TestCalendarFeedRouteSetsNoCookieOn429 is the 429 leg — see
// TestCalendarFeedRouteSetsNoCookieForAnArmedFeed's doc comment for why this
// is a coverage addition rather than a currently-reachable defect: the
// limiter is mounted ahead of both cookie-minting middlewares in
// configureFiberMiddleware and answers the 429 without ever calling c.Next(),
// so neither one runs once the budget is spent.
//
// This drives two explicit requests rather than the shared spendBudgetAndProbe
// helper: that helper is indifferent to the first request's outcome — it
// closes the body and returns only the second response — which would hide
// exactly what this test has to prove, that the limiter's budget of 1 is
// spent by a REAL, successful hit on the armed feed and not by a request
// something ahead of the limiter had already refused for an unrelated reason.
func TestCalendarFeedRouteSetsNoCookieOn429(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-limited@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)
	app := newCalendarFeedTestApp(t, handler, 1)
	feedTarget := api.CalendarFeedRateLimitPrefix + "/" + token + ".ics"

	first := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	firstResponse, err := app.Test(first, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("first feed request failed: %v", err)
	}
	defer func() { _ = firstResponse.Body.Close() }()
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the first request inside the budget of 1 to succeed, got %d — fix the probe before trusting the cookie assertion below", firstResponse.StatusCode)
	}
	assertNoSetCookie(t, firstResponse, "its 200")

	second := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	secondResponse, err := app.Test(second, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("second feed request failed: %v", err)
	}
	defer func() { _ = secondResponse.Body.Close() }()
	if secondResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the per-IP budget (1) is spent, got %d — fix the probe before trusting the cookie assertion below", secondResponse.StatusCode)
	}
	assertNoSetCookie(t, secondResponse, "its 429")
}

// The limiter half of the same predicate class — a scope widened onto the bare
// prefix, a nested path segment or a non-GET/HEAD verb — is
// TestCalendarFeedLimiterSpendsNoBudgetOnPathsThatReachNoFeed in
// rate_limit_scope_guard_test.go, beside the spelling sweep it shares this
// file's app builder with.

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
		api.CalendarFeedRateLimitPrefix + "/",
		api.CalendarFeedRateLimitPrefix,
		api.CalendarFeedRateLimitPrefix + "/a/b.ics",
		api.CalendarFeedRateLimitPrefix + "/.ics",
		// Control: a genuine neighbour that shares no route with the feed. If
		// THIS case also failed, the assertions below would be worthless — it
		// would mean the not_found page never carries a catalogue or a CSRF
		// token on this app, feed-adjacent or not.
		api.CalendarFeedRateLimitPrefix + "back",
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

// TestCalendarFeedBodyIgnoresRequestTimezoneSignals is the body-content half
// of TestCalendarFeedRouteSetsNoCookieOnTheProductionStack's "with
// X-Ovumcy-Timezone header" case: that test proves the header mints no cookie
// on the feed; this proves it changes nothing about what is served, for the
// one owner it could affect (see the early-return comment on
// LanguageMiddleware) — one whose users.timezone was never captured, where
// ResolveFeed's fallback location is whatever the transport layer would have
// resolved had it run for this route.
//
// The instance zone (Pacific/Pago_Pago, UTC-11) and the poller-claimed zone
// (Pacific/Kiritimati, UTC+14) are picked 25 hours apart on purpose: any two
// zones with an offset gap over 24h disagree about the calendar date on EVERY
// possible instant, so the mutation-kill below (temporarily letting
// LanguageMiddleware run for this route) does not depend on catching the real
// clock in the ~14-of-24-hours window a same-day pair such as UTC/Kiritimati
// would need. The seeded ~28-day period cadence anchors the current cycle so
// its next projected period lands exactly one day past the instance zone's
// "today" — the earliest date ProjectCycleStart can ever place a projection
// relative to whichever "today" resolved it — so a poller-claimed zone that
// actually reached the projector would move that date a further 28 days out,
// a difference no rendered .ics body could hide.
//
// Positive anchor: the plain/claimed equality above is worthless if the
// render can never depend on a timezone at all — it would pass just as well
// if ResolveFeed ignored location outright. So after that comparison, this
// test persists the SAME Kiritimati zone onto the owner's own users.timezone
// column (a signal ResolveFeed DOES honor — see resolveOwnerLocation) and
// re-issues a plain, header-free request: that is exactly the "poller-claimed
// zone actually reached the projector" case the paragraph above describes,
// so it must reproduce the same kind of shift and change the body. If it did
// not, the render would never depend on any zone, and the equality check
// above would be proving nothing.
func TestCalendarFeedBodyIgnoresRequestTimezoneSignals(t *testing.T) {
	instanceZone, err := time.LoadLocation("Pacific/Pago_Pago")
	if err != nil {
		t.Fatalf("load Pacific/Pago_Pago: %v", err)
	}

	handler, database := newRateLimitTestHandlerAndDBAtLocation(t, instanceZone)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-tz-signal@example.com", 14)
	// users.timezone stays "" (seedOwner never sets it) — the one condition
	// under which ResolveFeed's preference for the owner's own stored zone
	// falls through to whatever the transport layer hands it.

	today := services.DateAtLocation(time.Now(), instanceZone)
	starts := []time.Time{today.AddDate(0, 0, -83), today.AddDate(0, 0, -55), today.AddDate(0, 0, -27)}
	for _, start := range starts {
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: start, IsPeriod: true}).Error; err != nil {
			t.Fatalf("seed period log %s: %v", start.Format("2006-01-02"), err)
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).
		Update("last_period_start", starts[len(starts)-1]).Error; err != nil {
		t.Fatalf("set last_period_start: %v", err)
	}

	token := armCalendarFeedToken(t, database, user.ID)
	config := runtimeConfig{
		Location:        instanceZone,
		DefaultLanguage: "en",
		RateLimits:      uniformRateLimits(t, 100000, time.Hour),
	}
	app := newFiberApp(config, handler)
	feedTarget := api.CalendarFeedRateLimitPrefix + "/" + token + ".ics"

	plain := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	plainResponse, err := app.Test(plain, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("plain feed request failed: %v", err)
	}
	defer func() { _ = plainResponse.Body.Close() }()
	plainBody := mustReadAll(t, plainResponse)

	claimed := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	claimed.Header.Set("X-Ovumcy-Timezone", "Pacific/Kiritimati")
	claimed.AddCookie(&http.Cookie{Name: "ovumcy_tz", Value: "Pacific/Kiritimati"})
	claimedResponse, err := app.Test(claimed, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("zone-claiming feed request failed: %v", err)
	}
	defer func() { _ = claimedResponse.Body.Close() }()
	claimedBody := mustReadAll(t, claimedResponse)

	for name, response := range map[string]*http.Response{"plain": plainResponse, "zone-claiming": claimedResponse} {
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s request: expected 200 for the armed feed, got %d", name, response.StatusCode)
		}
		assertNoSetCookie(t, response, "the "+name+" request")
	}
	if !bytes.Equal(plainBody, claimedBody) {
		t.Fatalf("a poller-claimed timezone changed the feed body for an owner with no stored timezone:\nplain:         %q\nzone-claiming: %q", plainBody, claimedBody)
	}

	// Positive anchor (see doc comment): give this owner the persisted zone
	// ResolveFeed is actually documented to honor, then repeat the plain,
	// header-free request. A body that still matched plainBody would mean
	// the render never depended on any zone, and the equality assertion
	// above would not have proven that request signals specifically — as
	// opposed to every signal — are what gets ignored.
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).
		Update("timezone", "Pacific/Kiritimati").Error; err != nil {
		t.Fatalf("set owner timezone: %v", err)
	}

	ownerZoned := httptest.NewRequest(http.MethodGet, feedTarget, nil)
	ownerZonedResponse, err := app.Test(ownerZoned, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("owner-timezone feed request failed: %v", err)
	}
	defer func() { _ = ownerZonedResponse.Body.Close() }()
	if ownerZonedResponse.StatusCode != http.StatusOK {
		t.Fatalf("owner-timezone request: expected 200 for the armed feed, got %d", ownerZonedResponse.StatusCode)
	}
	assertNoSetCookie(t, ownerZonedResponse, "the owner-timezone request")
	ownerZonedBody := mustReadAll(t, ownerZonedResponse)

	if bytes.Equal(plainBody, ownerZonedBody) {
		t.Fatalf("test setup: the owner's own stored timezone must move the projected period date, otherwise this test cannot prove the body is zone-sensitive:\nplain:       %q\nowner-zoned: %q", plainBody, ownerZonedBody)
	}
}
