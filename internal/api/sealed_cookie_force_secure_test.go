package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// sealed_cookie_force_secure_test.go pins the cookie attribute no test touched:
// sealedCookieSpec.forceSecure.
//
// The OIDC transport cookies are declared SameSite=None because the provider
// posts the callback back cross-site, and browsers refuse a SameSite=None cookie
// that is not also Secure — so writeSealedCookie sets
// `Secure: handler.cookieSecure || spec.forceSecure`. SECURITY.md claims those
// cookies "require Secure=true to issue", but the cited tests only construct
// their handler with cookieSecure: true, which is a fixture, not an assertion,
// and the word forceSecure appeared in no test at all.
//
// TWO layers are pinned here, because the emitted attribute alone cannot see our
// intent: Fiber v3 independently forces Secure on any SameSite=None cookie, so
// deleting `|| spec.forceSecure` still emits a Secure cookie today. That makes the
// emitted-attribute case a browser-facing outcome guard (a cross-site sealed
// cookie is never issued without Secure, whichever layer provides it), and the
// spec-declaration case the guard on our own intent, which would otherwise rest
// entirely on a third-party default quietly staying put.
//
// Both directions of the outcome matter, hence the same-site case: a spec WITHOUT
// forceSecure must still follow cookieSecure, or "always Secure" would satisfy the
// forced cases and break plain-HTTP local deployments instead.
func TestSealedCookieCrossSiteSpecsDeclareForceSecure(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]sealedCookieSpec{
		"oidc state":  oidcStateCookieSpec,
		"oidc stepup": oidcStepupCookieSpec,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if spec.sameSite != "None" {
				t.Fatalf("%s: sameSite=%q, want None (the provider posts the callback cross-site)", spec.name, spec.sameSite)
			}
			if !spec.forceSecure {
				t.Fatalf("%s: forceSecure=false, but a SameSite=None cookie must never be written without Secure", spec.name)
			}
		})
	}
}

func TestWriteSealedCookieForcesSecureForCrossSiteSpecs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		spec            sealedCookieSpec
		wantSecure      bool
		wantSameSiteRaw http.SameSite
	}{
		"oidc state (cross-site, forced)":  {spec: oidcStateCookieSpec, wantSecure: true, wantSameSiteRaw: http.SameSiteNoneMode},
		"oidc stepup (cross-site, forced)": {spec: oidcStepupCookieSpec, wantSecure: true, wantSameSiteRaw: http.SameSiteNoneMode},
		"auth (same-site, not forced)":     {spec: authCookieSpec, wantSecure: false, wantSameSiteRaw: http.SameSiteLaxMode},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// cookieSecure=false is the whole point: on a deployment without
			// COOKIE_SECURE, a forced spec must still go out Secure.
			handler := &Handler{
				secretKey:    []byte("0123456789abcdef0123456789abcdef"),
				cookieSecure: false,
			}

			app := fiber.New()
			app.Get("/seal", func(c fiber.Ctx) error {
				if err := handler.writeSealedCookie(c, testCase.spec, []byte(`{"probe":true}`), time.Now().Add(time.Minute)); err != nil {
					t.Fatalf("writeSealedCookie: %v", err)
				}
				return c.SendStatus(fiber.StatusNoContent)
			})

			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal", nil), testConfigNoTimeout)
			if err != nil {
				t.Fatalf("seal request: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			cookie := responseCookie(response.Cookies(), testCase.spec.name)
			if cookie == nil {
				t.Fatalf("expected a %s cookie to be written", testCase.spec.name)
			}
			if cookie.Secure != testCase.wantSecure {
				t.Fatalf("%s: Secure=%v, want %v (cookieSecure=false, forceSecure=%v)",
					testCase.spec.name, cookie.Secure, testCase.wantSecure, testCase.spec.forceSecure)
			}
			if cookie.SameSite != testCase.wantSameSiteRaw {
				t.Fatalf("%s: SameSite=%v, want %v", testCase.spec.name, cookie.SameSite, testCase.wantSameSiteRaw)
			}
			if !cookie.HttpOnly {
				t.Fatalf("%s: expected HttpOnly", testCase.spec.name)
			}
		})
	}
}

// cookieCaptureCtx wraps a fiber.Ctx and intercepts Cookie() calls, recording
// the *fiber.Cookie exactly as the caller constructed it, before delegating
// to the real Ctx. This is required to pin clearSealedCookie's own Secure
// computation: Fiber's Res.Cookie() (res.go) forces Secure=true on any
// SameSite=None cookie unconditionally, before the Set-Cookie header is ever
// built, so the wire-level response cannot tell "our code computed
// Secure=true" apart from "Fiber overrode it on our behalf" — confirmed by
// running this suite's naive response-only version against the unfixed
// clearSealedCookie: it stayed green. Capturing pre-delegation observes the
// value the production code itself set.
type cookieCaptureCtx struct {
	fiber.Ctx
	captured *fiber.Cookie
}

func (c *cookieCaptureCtx) Cookie(cookie *fiber.Cookie) {
	// Snapshot a copy before delegating: Fiber's real Res.Cookie() mutates
	// this same *fiber.Cookie in place (cookie.Secure = true for
	// SameSite=None), so storing the pointer and reading it after
	// delegation would observe Fiber's post-mutation value instead of the
	// one clearSealedCookie computed.
	snapshot := *cookie
	c.captured = &snapshot
	c.Ctx.Cookie(cookie)
}

// TestClearSealedCookieForcesSecureForCrossSiteSpecs mirrors
// TestWriteSealedCookieForcesSecureForCrossSiteSpecs above, for
// clearSealedCookie. The write path sets
// `Secure: handler.cookieSecure || spec.forceSecure`; clearSealedCookie set
// only `Secure: handler.cookieSecure`, so under COOKIE_SECURE=false clearing
// a forced (SameSite=None) cookie would compute Secure=false — a clear
// instruction a browser drops instead of honoring, leaving the stale
// cross-site cookie in place. See the cookieCaptureCtx doc above for why
// this must assert on the captured pre-Fiber value, not just the emitted
// response.
func TestClearSealedCookieForcesSecureForCrossSiteSpecs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		spec       sealedCookieSpec
		wantSecure bool
	}{
		"oidc state (cross-site, forced)":  {spec: oidcStateCookieSpec, wantSecure: true},
		"oidc stepup (cross-site, forced)": {spec: oidcStepupCookieSpec, wantSecure: true},
		"auth (same-site, not forced)":     {spec: authCookieSpec, wantSecure: false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// cookieSecure=false is the whole point: on a deployment without
			// COOKIE_SECURE, clearing a forced spec must still compute
			// Secure, or the browser drops the SameSite=None clear cookie
			// outright, leaving the stale cookie in place instead of expiring it.
			handler := &Handler{
				secretKey:    []byte("0123456789abcdef0123456789abcdef"),
				cookieSecure: false,
			}

			var captured *fiber.Cookie
			app := fiber.New()
			app.Get("/clear", func(c fiber.Ctx) error {
				capture := &cookieCaptureCtx{Ctx: c}
				handler.clearSealedCookie(capture, testCase.spec)
				captured = capture.captured
				return c.SendStatus(fiber.StatusNoContent)
			})

			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/clear", nil), testConfigNoTimeout)
			if err != nil {
				t.Fatalf("clear request: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if captured == nil {
				t.Fatalf("expected clearSealedCookie to call c.Cookie for %s", testCase.spec.name)
			}
			// The load-bearing assertion: the Secure value clearSealedCookie
			// itself computed, observed before Fiber's SameSite=None
			// enforcement can mask a missing `|| spec.forceSecure`.
			if captured.Secure != testCase.wantSecure {
				t.Fatalf("%s: clearSealedCookie set Secure=%v, want %v (cookieSecure=false, forceSecure=%v)",
					testCase.spec.name, captured.Secure, testCase.wantSecure, testCase.spec.forceSecure)
			}

			// The browser-facing outcome, pinned alongside the mechanism:
			// a cleared cross-site sealed cookie is never emitted without
			// Secure.
			cookie := responseCookie(response.Cookies(), testCase.spec.name)
			if cookie == nil {
				t.Fatalf("expected a %s cookie to be written", testCase.spec.name)
			}
			if cookie.Secure != testCase.wantSecure {
				t.Fatalf("%s: emitted Secure=%v, want %v", testCase.spec.name, cookie.Secure, testCase.wantSecure)
			}
		})
	}
}
