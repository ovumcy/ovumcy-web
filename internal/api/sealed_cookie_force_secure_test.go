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
