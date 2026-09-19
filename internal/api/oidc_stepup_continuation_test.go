package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// The cross-site bounce exists because a provider on another site posts the
// callback cross-site, where SameSite=Lax withholds the session cookie. What
// follows pins the properties that make the bounce safe rather than merely
// working: it refuses a state that does not match before parking anything, it
// commits nothing on the cross-site leg, its hand-off is single-use, and the
// same-site leg still refuses a session that is not the one that started the
// step-up. The end-to-end round trip itself is covered by the opt-in
// cross-site e2e lane (e2e/auth-oidc-cross-site.spec.ts).

func crossSiteStepupCallback(t *testing.T, fixture *oidcStepupFixture, stepupCookieHeader, state, code string) *http.Response {
	t.Helper()
	form := url.Values{"state": {state}, "code": {code}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookieHeader))
	crossSiteNavigation.applyTo(request)
	return mustAppResponse(t, fixture.app, request)
}

func continueRequestWithCookies(t *testing.T, fixture *oidcStepupFixture, cookieHeader string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, oidcCallbackContinuePath, nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", cookieHeader)
	crossSiteNavigation.applyTo(request)
	return mustAppResponse(t, fixture.app, request)
}

func TestCrossSiteStepupCallbackRefusesAStateThatDoesNotMatchBeforeParkingAnything(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-state-mismatch@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	response := crossSiteStepupCallback(t, fixture, stepupCookie, "not-the-sealed-state", "callback-code")
	defer func() { _ = response.Body.Close() }()

	// The refusal is the ordinary settings redirect, and — the point of the
	// test — nothing was parked for a later leg to spend.
	assertStatusCode(t, response, http.StatusSeeOther)
	if continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); continuation != nil && strings.TrimSpace(continuation.Value) != "" {
		t.Fatal("a callback whose state does not match must not seal a continuation")
	}
}

func TestCrossSiteStepupContinuationIsSingleUse(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-continuation-replay@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	assertStatusCode(t, bounce, http.StatusSeeOther)
	continuation := responseCookie(bounce.Cookies(), oidcStepupContinuationCookieName)
	if continuation == nil || strings.TrimSpace(continuation.Value) == "" {
		t.Fatal("expected the cross-site callback to seal a continuation")
	}

	first := continueRequestWithCookies(t, fixture, joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	defer func() { _ = first.Body.Close() }()
	assertStatusCode(t, first, http.StatusOK)
	if cleared := responseCookie(first.Cookies(), oidcStepupContinuationCookieName); cleared == nil || strings.TrimSpace(cleared.Value) != "" {
		t.Fatal("expected the continue leg to expire the continuation it spent")
	}

	// Replaying the same sealed value — a browser that kept it, a log that
	// captured it — must not complete the action a second time. The step-up
	// cookie is gone by now, so there is nothing left to re-mint it from.
	replay := continueRequestWithCookies(t, fixture, joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	defer func() { _ = replay.Body.Close() }()
	if replay.StatusCode == http.StatusOK {
		t.Fatal("a replayed continuation must not complete the step-up again")
	}
}

func TestStepupContinuationRefusesASessionThatDidNotStartIt(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-continuation-owner@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	continuation := responseCookie(bounce.Cookies(), oidcStepupContinuationCookieName)
	if continuation == nil {
		t.Fatal("expected the cross-site callback to seal a continuation")
	}

	// The whole point of the bounce is that the session is read on THIS leg.
	// Presenting the continuation without one must refuse: a sealed payload
	// naming an owner is never authority on its own.
	response := continueRequestWithCookies(t, fixture, cookiePair(continuation))
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusOK {
		t.Fatal("the continue leg must not complete a step-up for an unidentified session")
	}
}

func TestOIDCStepupContinuationRefusesAPayloadItCannotComplete(t *testing.T) {
	t.Parallel()

	valid, err := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, 7, "argon2id$hash")
	if err != nil {
		t.Fatalf("build step-up state: %v", err)
	}

	for name, testCase := range map[string]struct {
		stepup oidcStepupState
		code   string
	}{
		// No code means nothing to redeem at the token endpoint: parking it
		// would hand the continue leg a hand-off that can only fail there,
		// after the step-up cookie has already been spent.
		"empty code":         {stepup: valid, code: "   "},
		"incomplete step-up": {stepup: oidcStepupState{}, code: "authorization-code"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := newOIDCStepupContinuation(time.Now(), testCase.stepup, testCase.code); err == nil {
				t.Fatal("expected the continuation constructor to refuse this payload")
			}
		})
	}

	// A zero clock means "now", not the zero instant — otherwise every
	// continuation would be minted already expired.
	continuation, err := newOIDCStepupContinuation(time.Time{}, valid, "authorization-code")
	if err != nil {
		t.Fatalf("expected a zero clock to default to now, got %v", err)
	}
	if !continuation.validAt(time.Time{}) {
		t.Fatal("a continuation minted on the default clock must be valid on the default clock")
	}
	if continuation.validAt(time.Now().Add(2 * oidcStepupContinuationTTL)) {
		t.Fatal("a continuation must not outlive its TTL")
	}
}

func TestOIDCStepupContinuationCookieRefusesInsecureTransport(t *testing.T) {
	t.Parallel()

	stepup, err := newOIDCIdentityLinkStepupState(time.Now(), 9)
	if err != nil {
		t.Fatalf("build step-up state: %v", err)
	}
	continuation, err := newOIDCStepupContinuation(time.Now(), stepup, "authorization-code")
	if err != nil {
		t.Fatalf("build continuation: %v", err)
	}

	handler := newSealedExpirySweepHandler()
	handler.cookieSecure = false

	// The cookie is Secure by construction, so a deployment that is not on
	// secure transport must refuse to mint it rather than write one the
	// browser drops — the same rule its two sibling OIDC cookies follow.
	app := fiber.New()
	app.Get("/mint", func(c fiber.Ctx) error {
		if err := handler.setOIDCStepupContinuationCookie(c, continuation); err == nil {
			t.Error("expected an insecure deployment to refuse the continuation cookie")
		}
		if err := handler.setOIDCStepupContinuationCookie(c, oidcStepupContinuation{}); err == nil {
			t.Error("expected an empty payload to be refused")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/mint", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("mint request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if cookie := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); cookie != nil {
		t.Fatal("a refused mint must write no cookie at all")
	}
}

func TestCrossSiteStepupCallbackWithoutACodeParksNothing(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-no-code@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	// A provider that posts a matching state with neither a code nor an error
	// leaves nothing to redeem; the bounce must refuse rather than park a
	// hand-off whose only possible outcome is a failure one leg later.
	response := crossSiteStepupCallback(t, fixture, stepupCookie, state, "")
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); continuation != nil && strings.TrimSpace(continuation.Value) != "" {
		t.Fatal("a callback carrying no authorization code must not seal a continuation")
	}
}

func TestCrossSiteStepupCallbackRefusesAProviderError(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-provider-error@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	form := url.Values{"state": {state}, "error": {"access_denied"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookie))
	crossSiteNavigation.applyTo(request)

	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); continuation != nil && strings.TrimSpace(continuation.Value) != "" {
		t.Fatal("a provider error must not be parked for completion")
	}
}

func TestSameSiteStepupCallbackStillCompletesDirectly(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "samesite-direct-callback@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	// No cross-site Fetch Metadata: a provider on the app's own site, and the
	// path the bounce must leave untouched.
	response := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = response.Body.Close() }()

	if continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); continuation != nil && strings.TrimSpace(continuation.Value) != "" {
		t.Fatal("a same-site callback must complete directly, without bouncing")
	}
	if reveal := responseCookie(response.Cookies(), recoveryCodeCookieName); reveal == nil || strings.TrimSpace(reveal.Value) == "" {
		t.Fatal("expected the same-site callback to complete the local-password setup and mint the reveal")
	}
}
