package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The step-up that enrolls a local password on an OIDC-only account ends at the
// recovery-code reveal, and that reveal spends the account's one-time mark, so
// it is guarded on Fetch Metadata: only a same-origin initiator may claim it
// (TestRecoveryCodeRevealRefusesAForeignRequestWithoutSpendingTheMark pins the
// refusal). Sec-Fetch-Site describes the whole redirect CHAIN, and the callback
// this flow finishes on is a cross-site POST the provider makes, so a 303 from
// here reached /recovery-code still labelled off-origin: the guard refused, the
// owner was sent to her dashboard, and the code she had just minted was gone
// with no way back to it. The lane that caught it is opt-in and skipped in
// default CI, which is why the refusal survived a green pipeline.
//
// The fix is a same-origin document, not an exemption — the interstitial's own
// navigation is initiated by this origin, so the label the guard reads becomes
// true rather than excused, and an attacker's page still cannot produce one.
// What this test pins is the shape that makes that possible: the callback must
// not hand the browser a redirect it would follow while the chain is still
// off-origin.
func TestOIDCCompleteLocalPasswordSetupHandsTheRevealOverSameOrigin(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-reveal-handoff@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	// The provider's form_post lands here as a cross-site top-level navigation.
	// The callback handler reads no Fetch Metadata itself, so these headers do
	// not decide the assertions below — they state the condition the assertions
	// are only meaningful under, which is the one the live lane reproduced.
	form := url.Values{"state": {state}, "code": {"callback-code"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookie))
	crossSiteNavigation.applyTo(request)

	callbackResponse := mustAppResponse(t, fixture.app, request)
	defer func() { _ = callbackResponse.Body.Close() }()

	assertStatusCode(t, callbackResponse, http.StatusOK)
	if location := callbackResponse.Header.Get("Location"); location != "" {
		t.Fatalf("the callback must not redirect into the guarded reveal from an off-origin chain; got Location %q", location)
	}
	if contentType := callbackResponse.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("expected a same-origin html handoff, got content-type %q", contentType)
	}
	body := mustReadBodyString(t, callbackResponse.Body)
	if !strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatalf("expected a meta-refresh handoff, got %q", body)
	}
	if !strings.Contains(body, "/recovery-code") {
		t.Fatalf("expected the handoff to target the reveal surface, got %q", body)
	}

	// The reveal itself still has to ride the response, and the session with it:
	// finalizing bumps AuthSessionVersion, so the handoff navigation carries the
	// re-minted auth cookie or lands on /login instead.
	revealCookie := responseCookie(callbackResponse.Cookies(), recoveryCodeCookieName)
	if revealCookie == nil || strings.TrimSpace(revealCookie.Value) == "" {
		t.Fatal("expected the callback to seal the one-time reveal cookie")
	}
	authCookie := responseCookie(callbackResponse.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected the callback to re-mint the auth cookie the handoff navigation carries")
	}

	// And the navigation the handoff produces — same-origin, because this
	// origin's own document started it — reveals the code. This half is a
	// completeness check on the handoff, not the regression: the request is
	// hand-built here, so only the shape asserted above separates the fixed
	// callback from the broken one.
	revealed := recoveryCodePageWithHeaders(
		t,
		fixture.app,
		joinCookieHeader(cookiePair(authCookie), cookiePair(revealCookie)),
		sameOriginNavigation,
	)
	defer func() { _ = revealed.Body.Close() }()
	assertStatusCode(t, revealed, http.StatusOK)
	if page := mustReadBodyString(t, revealed.Body); !strings.Contains(page, "OVUM-") {
		t.Fatal("expected the owner's freshly minted recovery code to be revealed")
	}
}
