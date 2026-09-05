package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// newCalendarFeedNoCookieTestApp builds the app exactly as the production
// composition root does (newFiberApp: full middleware chain, the real route
// table, the NotFound catch-all), backed by a temp SQLite DB. Rate limits are
// set high so the handful of feed probes below cannot trip a limiter and be
// mistaken for the cookie defect this file exists to catch.
func newCalendarFeedNoCookieTestApp(t *testing.T) *fiber.App {
	t.Helper()

	handler := newRateLimitTestHandler(t)
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
			CalendarFeedMax:      100000,
			CalendarFeedWindow:   time.Hour,
		},
	}
	return newFiberApp(config, handler)
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
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, feedTarget, nil)
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

	// Positive anchor (security-testing.md): every case above is a negative
	// assertion, which would pass just as well if the CSRF/timezone-cookie
	// machinery were dead app-wide rather than specifically excluded for the
	// feed. Prove it is alive, on the SAME app instance, against an ordinary
	// unauthenticated page that carries no such exclusion.
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
