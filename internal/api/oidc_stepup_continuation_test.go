package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The cross-site bounce exists because a provider on another site posts the
// callback cross-site, where SameSite=Lax withholds the session cookie. What
// follows pins the properties that make the bounce safe rather than merely
// working: it refuses a state that does not match before parking anything and
// without spending the step-up cookie, it commits nothing on the cross-site
// leg, it hands over through a same-origin document so the continue route can
// carry the first-party guard, its hand-off is single-use, and that leg still
// refuses a session that is not the one that started the step-up. The
// end-to-end round trip is covered by the opt-in cross-site e2e lane
// (e2e/auth-oidc-cross-site.spec.ts).

func crossSiteStepupCallback(t *testing.T, fixture *oidcStepupFixture, stepupCookieHeader, state, code string) *http.Response {
	t.Helper()
	return crossSiteStepupCallbackForm(t, fixture, stepupCookieHeader, url.Values{"state": {state}, "code": {code}})
}

// continueLeg replays what the interstitial's own navigation looks like:
// same-origin, because the document that started it is served by this app.
func continueLeg(t *testing.T, fixture *oidcStepupFixture, cookieHeader string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, oidcCallbackContinuePath, nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", cookieHeader)
	sameOriginNavigation.applyTo(request)
	return mustAppResponse(t, fixture.app, request)
}

func continuationFromBounce(t *testing.T, response *http.Response) *http.Cookie {
	t.Helper()
	continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName)
	if continuation == nil || strings.TrimSpace(continuation.Value) == "" {
		t.Fatal("expected the cross-site callback to seal a step-up continuation")
	}
	// The attributes ARE the containment, and only the emitted header shows
	// them: SameSite=None would hand the completion leg to any site that can
	// reach the route, and a wider path would send the hand-off along with
	// every other request until it is spent.
	if continuation.SameSite != http.SameSiteLaxMode {
		t.Fatalf("continuation SameSite=%v, want Lax", continuation.SameSite)
	}
	if continuation.Path != oidcCallbackContinuePath {
		t.Fatalf("continuation Path=%q, want %q", continuation.Path, oidcCallbackContinuePath)
	}
	if !continuation.HttpOnly || !continuation.Secure {
		t.Fatalf("continuation HttpOnly=%v Secure=%v, want both", continuation.HttpOnly, continuation.Secure)
	}
	return continuation
}

func TestCrossSiteStepupCallbackRefusesAStateThatDoesNotMatchWithoutSpendingTheCookie(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-state-mismatch@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	response := crossSiteStepupCallback(t, fixture, stepupCookie, "not-the-sealed-state", "callback-code")
	defer func() { _ = response.Body.Close() }()

	assertCrossSiteStepupRefusal(t, response)
	// The step-up cookie is SameSite=None, so any site can cause a request
	// carrying it. Spending it on a state that does not match would let a
	// stranger cancel a step-up the owner is in the middle of.
	if spent := responseCookie(response.Cookies(), oidcStepupCookieName); spent != nil && strings.TrimSpace(spent.Value) == "" {
		t.Fatal("a mismatching callback must not expire the owner's step-up cookie")
	}

	// And the flow still completes afterwards with the real state, proving the
	// cookie above survived rather than merely not being re-sent.
	state := extractStepupCallbackState(t, fixture)
	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	assertStatusCode(t, bounce, http.StatusOK)
	_ = continuationFromBounce(t, bounce)
}

func TestCrossSiteStepupCallbackHandsOverThroughASameOriginDocument(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-handoff-shape@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()

	// Not a 303: Sec-Fetch-Site describes the whole redirect chain, so a
	// redirect from here would reach the continue route still labelled
	// cross-site and its first-party guard would refuse the owner's own
	// return. A document served by this origin makes that navigation
	// same-origin in fact.
	assertStatusCode(t, bounce, http.StatusOK)
	if location := bounce.Header.Get("Location"); location != "" {
		t.Fatalf("the bounce must not redirect into the guarded continue route; got Location %q", location)
	}
	body := mustReadBodyString(t, bounce.Body)
	if !strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, oidcCallbackContinuePath) {
		t.Fatalf("expected a meta-refresh hand-off to %s, got %q", oidcCallbackContinuePath, body)
	}
	_ = continuationFromBounce(t, bounce)
}

func TestStepupContinueRouteRefusesAnOffOriginNavigation(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-continue-offorigin@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	continuation := continuationFromBounce(t, bounce)

	// Lax sends both the session and the continuation on a top-level
	// navigation another site starts, so without the guard a page on that site
	// could complete an erasure or a link at a moment of its choosing.
	request := httptest.NewRequest(http.MethodGet, oidcCallbackContinuePath, nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	crossSiteNavigation.applyTo(request)

	refused := mustAppResponse(t, fixture.app, request)
	defer func() { _ = refused.Body.Close() }()
	assertStatusCode(t, refused, http.StatusSeeOther)
	if reveal := responseCookie(refused.Cookies(), recoveryCodeCookieName); reveal != nil && strings.TrimSpace(reveal.Value) != "" {
		t.Fatal("an off-origin navigation must not complete the step-up")
	}

	// The owner's own return still works: the refusal spent nothing.
	completed := continueLeg(t, fixture, joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	defer func() { _ = completed.Body.Close() }()
	assertStatusCode(t, completed, http.StatusOK)
}

func TestCrossSiteStepupContinuationIsSingleUse(t *testing.T) {
	t.Parallel()

	// identity_link rather than local-password setup: the password purpose
	// short-circuits on its second run because the account now HAS a local
	// password, which would let a replay look refused for the wrong reason.
	fixture := newOIDCStepupFixture(t, "crosssite-continuation-replay@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := postOIDCIdentityLinkStepupStart(t, fixture)
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	bounce := crossSiteStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = bounce.Body.Close() }()
	continuation := continuationFromBounce(t, bounce)

	first := continueLeg(t, fixture, joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	defer func() { _ = first.Body.Close() }()
	assertStatusCode(t, first, http.StatusSeeOther)
	if cleared := responseCookie(first.Cookies(), oidcStepupContinuationCookieName); cleared == nil || strings.TrimSpace(cleared.Value) != "" {
		t.Fatal("expected the continue leg to expire the continuation it spent")
	}

	// Replaying the sealed value — a browser that kept it, a log that captured
	// it — must not run the link a second time. Clearing the cookie above is
	// only an instruction to the client, so what actually stops the replay is
	// the authorization code the continuation carries: the provider redeemed
	// it on the leg that just ran and refuses it now. Model exactly that, or
	// the assertion below would be measuring a stub that never burns a code.
	fixture.oidcStub.identityLinkReauthErr = services.ErrOIDCLinkFailed

	replay := continueLeg(t, fixture, joinCookieHeader(fixture.authCookie, cookiePair(continuation)))
	defer func() { _ = replay.Body.Close() }()
	assertFlashRefusal(t, replay)
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
	continuation := continuationFromBounce(t, bounce)

	// The whole point of the bounce is that the session is read on THIS leg.
	// Presenting the continuation without one must refuse: a sealed payload
	// naming an owner is never authority on its own.
	response := continueLeg(t, fixture, cookiePair(continuation))
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
		// would hand the continue leg a payload that can only fail there,
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

func TestOIDCStepupContinuationCookieRefusesWhatItCannotMint(t *testing.T) {
	t.Parallel()

	stepup, err := newOIDCIdentityLinkStepupState(time.Now(), 9)
	if err != nil {
		t.Fatalf("build step-up state: %v", err)
	}
	continuation, err := newOIDCStepupContinuation(time.Now(), stepup, "authorization-code")
	if err != nil {
		t.Fatalf("build continuation: %v", err)
	}

	insecure := newSealedExpirySweepHandler()
	insecure.cookieSecure = false
	secure := newSealedExpirySweepHandler()

	expired := continuation
	expired.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)

	app := fiber.New()
	app.Get("/mint", func(c fiber.Ctx) error {
		// The cookie is Secure by construction, so a deployment not on secure
		// transport must refuse to mint it rather than write one the browser
		// drops — the rule its two sibling OIDC cookies already follow.
		if err := insecure.setOIDCStepupContinuationCookie(c, continuation); err == nil {
			t.Error("expected an insecure deployment to refuse the continuation cookie")
		}
		// Secure transport is not enough on its own: the payload also has to
		// be one the continue leg could still complete. Sealing an empty or
		// already-expired hand-off would answer the cross-site POST as if it
		// had worked, having spent the step-up cookie for a navigation that
		// can only refuse.
		if err := secure.setOIDCStepupContinuationCookie(c, oidcStepupContinuation{}); err == nil {
			t.Error("expected an empty payload to be refused")
		}
		if err := secure.setOIDCStepupContinuationCookie(c, expired); err == nil {
			t.Error("expected an expired payload to be refused")
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

// TestOIDCStepupContinuationCookieIgnoresAValueItCannotRead pins what the peek
// RETURNS for a value it cannot read: nothing at all, never a partially filled
// payload. What the response does with that value — retract it, so it stops
// being sent to the continue route — is the subject of
// TestEverySealedCookieReaderRetractsTheValueItRefuses; "ignores" here is about
// the return value, not about leaving the cookie alone.
func TestOIDCStepupContinuationCookieIgnoresAValueItCannotRead(t *testing.T) {
	t.Parallel()

	handler := newSealedExpirySweepHandler()
	// Sealed by this deployment's own key, so it opens — but the seal
	// authenticates bytes, it does not vouch for their shape.
	sealedNonJSON, err := handler.sealCookieValue(oidcStepupContinuationCookieName, []byte("not a continuation"))
	if err != nil {
		t.Fatalf("seal the probe value: %v", err)
	}

	for name, raw := range map[string]string{
		"value the seal refuses":        "tampered-value",
		"sealed but not a continuation": sealedNonJSON,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cookies := map[string]string{oidcStepupContinuationCookieName: raw}
			response := runSealedCookieProbeRequest(t, cookies, func(c fiber.Ctx) error {
				// Nothing at all, not a partially-filled payload: a
				// continuation the peek cannot read must not reach the
				// dispatcher carrying a purpose or an owner id.
				if continuation := handler.peekOIDCStepupContinuationCookie(c); continuation != (oidcStepupContinuation{}) {
					t.Errorf("expected an unreadable continuation to peek as nothing, got %+v", continuation)
				}
				return nil
			})
			defer func() { _ = response.Body.Close() }()
		})
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

	assertCrossSiteStepupRefusal(t, response)
}

func TestCrossSiteStepupCallbackRefusesAProviderError(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "crosssite-provider-error@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	response := crossSiteStepupCallbackForm(t, fixture, stepupCookie, url.Values{"state": {state}, "error": {"access_denied"}})
	defer func() { _ = response.Body.Close() }()

	assertCrossSiteStepupRefusal(t, response)
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

// assertFlashRefusal pins that a response is the settings refusal channel: a
// 303 back to /settings carrying an error flash, never a completion. The flash
// has to be OPENED, not merely counted: a completed step-up answers with the
// same 303 to the same path carrying a sealed success flash, so a refusal
// asserted by status and cookie presence alone is satisfied by the completion
// it means to rule out.
func assertFlashRefusal(t *testing.T, response *http.Response) {
	t.Helper()
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected the refusal to land on /settings, got %q", location)
	}
	flash := responseCookie(response.Cookies(), flashCookieName)
	if flash == nil || strings.TrimSpace(flash.Value) == "" {
		t.Fatal("expected the refusal to carry a flash the settings page renders")
	}
	payload := decodeFlashCookieForTest(t, flash.Value)
	if payload.SettingsError == "" && payload.AuthError == "" {
		t.Fatalf("expected an error flash, got %+v", payload)
	}
}

// crossSiteStepupCallbackForm posts an arbitrary callback body cross-site, so a
// case can model what a provider actually returned — a mismatching state, an
// error, a body with no code at all — rather than only the happy shape.
func crossSiteStepupCallbackForm(t *testing.T, fixture *oidcStepupFixture, stepupCookieHeader string, form url.Values) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookieHeader))
	crossSiteNavigation.applyTo(request)
	return mustAppResponse(t, fixture.app, request)
}

// assertCrossSiteStepupRefusal pins the shape a refusal on the CROSS-SITE leg
// has to have. Status and body alone cannot say it — the successful bounce is a
// 200 meta-refresh document too — so the target is checked as well, and the
// flash is opened rather than counted, the lesson the replay test already paid
// for.
func assertCrossSiteStepupRefusal(t *testing.T, response *http.Response) {
	t.Helper()
	assertStatusCode(t, response, http.StatusOK)
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("a refusal on the cross-site leg must not redirect: the chain stays cross-site and Lax withholds both the session and the flash from it; got Location %q", location)
	}
	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, `content="0; url=/settings"`) {
		t.Fatalf("expected a same-origin document navigating to /settings, got %q", body)
	}
	if strings.Contains(body, oidcCallbackContinuePath) {
		t.Fatalf("a refusal must not hand over to the continue route, got %q", body)
	}
	flash := responseCookie(response.Cookies(), flashCookieName)
	if flash == nil || strings.TrimSpace(flash.Value) == "" {
		t.Fatal("expected the refusal to carry a flash the settings page renders")
	}
	payload := decodeFlashCookieForTest(t, flash.Value)
	if payload.SettingsError == "" {
		t.Fatalf("expected the refusal on the settings error channel, got %+v", payload)
	}
	if payload.SettingsSuccess != "" {
		t.Fatalf("a refusal must not also flash a success, got %+v", payload)
	}
	if continuation := responseCookie(response.Cookies(), oidcStepupContinuationCookieName); continuation != nil && strings.TrimSpace(continuation.Value) != "" {
		t.Fatal("a refused cross-site callback must not park a continuation")
	}
}

// TestSameSiteStepupCallbackRefusalStillRedirects is the other side of that
// rule: the hand-off document is for the leg that needs it. A same-site
// callback carries its cookies on an ordinary 303 already, and the same
// refusal arm serves both legs.
func TestSameSiteStepupCallbackRefusalStillRedirects(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "samesite-refusal-redirect@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	response := postOIDCStepupCallback(t, fixture, stepupCookie, "not-the-sealed-state", "callback-code")
	defer func() { _ = response.Body.Close() }()

	assertFlashRefusal(t, response)
}
