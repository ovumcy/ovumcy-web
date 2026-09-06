package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// secFetchHeaders is one browser's account of who started a request. The shapes
// below are the ones that decide the guard: a page load the owner performed on
// this origin, a first-party client fetching the published next_path, the same
// load started from someone else's page, an embed, and a speculative load.
type secFetchHeaders struct {
	site    string
	mode    string
	dest    string
	purpose string
}

var (
	sameOriginNavigation = secFetchHeaders{site: "same-origin", mode: "navigate", dest: "document"}
	crossSiteNavigation  = secFetchHeaders{site: "cross-site", mode: "navigate", dest: "document"}
	sameOriginFetch      = secFetchHeaders{site: "same-origin", mode: "cors", dest: "empty"}
	sameOriginImage      = secFetchHeaders{site: "same-origin", mode: "no-cors", dest: "image"}
	// A speculative load wears the navigation's own clothes and is separated
	// from it by Sec-Purpose alone.
	sameOriginPrefetch = secFetchHeaders{site: "same-origin", mode: "navigate", dest: "document", purpose: "prefetch;prerender"}
	// A proxy that forwards part of the family and strips Sec-Fetch-Site. Each
	// check reads its own header, so what survives still decides.
	strippedSiteEmbed    = secFetchHeaders{mode: "no-cors", dest: "image"}
	strippedSitePrefetch = secFetchHeaders{mode: "navigate", dest: "document", purpose: "prefetch;prerender"}
)

// applyTo writes only the fields the case names, so a case can model a request
// that reached the app with part of the family stripped on the way.
func (headers secFetchHeaders) applyTo(request *http.Request) {
	if headers.site != "" {
		request.Header.Set(headerSecFetchSite, headers.site)
	}
	if headers.mode != "" {
		request.Header.Set("Sec-Fetch-Mode", headers.mode)
	}
	if headers.dest != "" {
		request.Header.Set(headerSecFetchDest, headers.dest)
	}
	if headers.purpose != "" {
		request.Header.Set(headerSecPurpose, headers.purpose)
	}
}

// TestFirstPartyGuardedRoutesAreExactlyTheDeclaredSet is the class half of the
// guard. Nothing in the route table says which GET mutates state, so the set is
// declared in routes.go and pinned here in BOTH directions: a guard dropped from
// a route it is named for reddens, and a route that quietly acquires one without
// being named reddens too.
func TestFirstPartyGuardedRoutesAreExactlyTheDeclaredSet(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	marker := firstPartyGuardClosureMarker() + ".func"

	guarded := []string{}
	for _, route := range app.GetRoutes() {
		if route.Method != fiber.MethodGet {
			continue
		}
		for _, routeHandler := range route.Handlers {
			name := runtime.FuncForPC(reflect.ValueOf(routeHandler).Pointer()).Name()
			if strings.Contains(name, marker) {
				guarded = append(guarded, route.Method+" "+route.Path)
				break
			}
		}
	}

	declared := append([]string{}, firstPartyGuardedRoutes...)
	sort.Strings(guarded)
	sort.Strings(declared)
	if !reflect.DeepEqual(guarded, declared) {
		t.Fatalf("navigation-guarded routes drifted from the declared set:\n  routed:   %v\n  declared: %v", guarded, declared)
	}
	if len(declared) == 0 {
		t.Fatal("expected at least one declared navigation-guarded route; recheck route discovery")
	}
}

// TestRegisterPickupRefusesAForeignRequestWithoutSpendingTheNonce holds the
// property that matters: a refusal must not cost the owner the thing the forged
// request was after. The nonce is single-use, so the second half of each case —
// the owner's own navigation still completing — is what separates a guard from a
// denial of service.
func TestRegisterPickupRefusesAForeignRequestWithoutSpendingTheNonce(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		headers secFetchHeaders
	}{
		{name: "cross-site navigation", headers: crossSiteNavigation},
		{name: "same-origin embed", headers: sameOriginImage},
		{name: "same-origin prefetch", headers: sameOriginPrefetch},
		{name: "embed with the site header stripped", headers: strippedSiteEmbed},
		{name: "prefetch with the site header stripped", headers: strippedSitePrefetch},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, _ := newOnboardingTestApp(t)
			pickupCookie := registerAndExtractPickupCookie(t, app, "pickup-guard-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com")

			refused := pickupRegisterWithHeaders(t, app, pickupCookie, testCase.headers)
			assertStatusCode(t, refused, http.StatusSeeOther)
			if location := refused.Header.Get("Location"); location != "/login" {
				t.Fatalf("expected the refusal to land on /login, got %q", location)
			}
			if authCookie := responseCookie(refused.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
				t.Fatalf("expected a refused pickup to mint no session, got %#v", authCookie)
			}

			if retracted := responseCookie(refused.Cookies(), registerPickupCookieName); retracted != nil && strings.TrimSpace(retracted.Value) == "" {
				t.Fatal("a refused navigation must not retract the owner's pickup cookie")
			}

			accepted := pickupRegisterWithHeaders(t, app, pickupCookie, sameOriginNavigation)
			assertStatusCode(t, accepted, http.StatusSeeOther)
			if location := accepted.Header.Get("Location"); location != "/register" {
				t.Fatalf("expected the owner's own navigation to complete the pickup, got %q", location)
			}
			if authCookie := responseCookie(accepted.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
				t.Fatalf("expected the owner's own navigation to mint a session, got %#v", authCookie)
			}
		})
	}
}

// TestCalendarFeedRevealRefusesAForeignRequestWithoutSpendingTheMark is the
// same property on the other guarded route. The reveal is claimed by a
// compare-and-set that never resets, so a refusal that burned the mark — or
// retracted the sealed cookie — would hand the forged request exactly the
// outcome the guard exists to deny.
func TestCalendarFeedRevealRefusesAForeignRequestWithoutSpendingTheMark(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		headers    secFetchHeaders
		wantReason string
	}{
		{name: "cross-site navigation", headers: crossSiteNavigation, wantReason: firstPartyRefusedOffOriginInitiator},
		{name: "same-origin embed", headers: sameOriginImage, wantReason: firstPartyRefusedUnsupportedDest},
		{name: "same-origin prefetch", headers: sameOriginPrefetch, wantReason: firstPartyRefusedSpeculativeLoad},
		{name: "embed with the site header stripped", headers: strippedSiteEmbed, wantReason: firstPartyRefusedUnsupportedDest},
		{name: "prefetch with the site header stripped", headers: strippedSitePrefetch, wantReason: firstPartyRefusedSpeculativeLoad},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := newSettingsSecurityTestContextWithOptions(t, "feed-reveal-guard-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com",
				onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

			generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
			defer func() { _ = generated.Body.Close() }()
			assertStatusCode(t, generated, http.StatusSeeOther)
			sealed := responseCookie(generated.Cookies(), calendarFeedRevealCookieName)
			if sealed == nil || strings.TrimSpace(sealed.Value) == "" {
				t.Fatal("expected a sealed reveal cookie on the generate response")
			}

			refused, body, logOutput := revealCalendarFeedWithHeaders(t, ctx.app, ctx.authCookie, sealed.Value, testCase.headers)
			defer func() { _ = refused.Body.Close() }()
			assertStatusCode(t, refused, http.StatusSeeOther)
			if location := refused.Header.Get("Location"); location != "/settings" {
				t.Fatalf("expected the refusal to land on /settings, got %q", location)
			}
			if strings.Contains(body, "/calendar/feed/") {
				t.Fatal("a refused reveal must not carry the subscribe URL")
			}
			if retracted := responseCookie(refused.Cookies(), calendarFeedRevealCookieName); retracted != nil && strings.TrimSpace(retracted.Value) == "" {
				t.Fatal("a refused navigation must not retract the owner's reveal cookie")
			}
			// The reason must name the check that actually fired. A prefetch
			// reported as a cross-origin attempt sends an operator after an
			// attacker who was never there.
			deniedLine := assertHealthEgressAudited(t, logOutput, "settings.calendar_feed_reveal", "denied", "calendar_feed")
			if !strings.Contains(deniedLine, `reason="`+testCase.wantReason+`"`) {
				t.Fatalf("expected the refusal to be audited as %q, got %q", testCase.wantReason, deniedLine)
			}

			accepted, acceptedBody, _ := revealCalendarFeedWithHeaders(t, ctx.app, ctx.authCookie, sealed.Value, sameOriginNavigation)
			defer func() { _ = accepted.Body.Close() }()
			assertStatusCode(t, accepted, http.StatusOK)
			if !strings.Contains(acceptedBody, "/calendar/feed/") {
				t.Fatal("expected the owner's own navigation to still reveal the subscribe URL")
			}
		})
	}
}

// TestFirstPartyGuardAdmitsTheFetchOfThePublishedNextPath holds the
// half of the contract the refusals must not eat. Both guarded routes ARE the
// `next_path` docs/openapi.yaml tells a JSON client to follow, and such a client
// fetches rather than navigates — Sec-Fetch-Dest: empty. Refusing it would make
// the guard narrow the published API, and it would buy nothing against injected
// same-origin script, which reaches the same value through a window it opens.
func TestFirstPartyGuardAdmitsTheFetchOfThePublishedNextPath(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	pickupCookie := registerAndExtractPickupCookie(t, app, "pickup-guard-next-path@example.com")

	accepted := pickupRegisterWithHeaders(t, app, pickupCookie, sameOriginFetch)
	assertStatusCode(t, accepted, http.StatusSeeOther)
	if location := accepted.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected a first-party fetch of next_path to complete the pickup, got %q", location)
	}
	if authCookie := responseCookie(accepted.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatalf("expected a first-party fetch of next_path to mint a session, got %#v", authCookie)
	}
}

// TestRecoveryCodeRevealRefusesAForeignRequestWithoutSpendingTheMark covers the
// third and fourth sites of the class. The recovery-code reveal is claimed from
// GET /recovery-code and from the inline block on GET /register, and neither
// route can carry the guard — /register is also the anonymous signup page, where
// a cross-site inbound link is an ordinary way to arrive. The rule therefore sits
// on the claim, and this is what proves it does.
func TestRecoveryCodeRevealRefusesAForeignRequestWithoutSpendingTheMark(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		headers secFetchHeaders
	}{
		{name: "cross-site navigation", headers: crossSiteNavigation},
		{name: "same-origin embed", headers: sameOriginImage},
		{name: "same-origin prefetch", headers: sameOriginPrefetch},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			email := "recovery-reveal-guard-" + strings.ReplaceAll(testCase.name, " ", "-") + "@example.com"
			user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
			recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
			resetCookie := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

			redeemed := redeemResetCookieAsBrowser(t, app, resetCookie, "EvenStronger2")
			assertStatusCode(t, redeemed, http.StatusSeeOther)
			authCookie := responseCookie(redeemed.Cookies(), authCookieName)
			revealCookie := responseCookie(redeemed.Cookies(), recoveryCodeCookieName)
			if authCookie == nil || revealCookie == nil || strings.TrimSpace(revealCookie.Value) == "" {
				t.Fatalf("expected the reset to hand back a session and a reveal cookie, got %#v / %#v", authCookie, revealCookie)
			}
			cookies := joinCookieHeader(authCookieName+"="+authCookie.Value, cookiePair(revealCookie))

			refused := recoveryCodePageWithHeaders(t, app, cookies, testCase.headers)
			assertStatusCode(t, refused, http.StatusSeeOther)
			if location := refused.Header.Get("Location"); location != "/dashboard" {
				t.Fatalf("expected the refusal to take the page's own exit, got %q", location)
			}
			if body := mustReadBodyString(t, refused.Body); strings.Contains(body, "OVUM-") {
				t.Fatal("a refused reveal must not carry the recovery code")
			}

			accepted := recoveryCodePageWithHeaders(t, app, cookies, sameOriginNavigation)
			assertStatusCode(t, accepted, http.StatusOK)
			if body := mustReadBodyString(t, accepted.Body); !strings.Contains(body, "OVUM-") {
				t.Fatal("expected the owner's own visit to still reveal the recovery code")
			}
		})
	}
}

// redeemResetCookieAsBrowser is the HTML arm of the redeem: the JSON one answers
// 200 with the code in the body, which is not the surface this reveal guard is
// about.
func redeemResetCookieAsBrowser(t *testing.T, app *fiber.App, resetCookieValue string, password string) *http.Response {
	t.Helper()

	form := url.Values{"password": {password}, "confirm_password": {password}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)
	return mustAppResponse(t, app, request)
}

func recoveryCodePageWithHeaders(t *testing.T, app *fiber.App, cookies string, headers secFetchHeaders) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookies)
	headers.applyTo(request)
	return mustAppResponse(t, app, request)
}

// TestRegisterPageStaysReachableFromAnotherSite is the reason the recovery-code
// rule sits on the claim and not on the route. /register is the anonymous signup
// page, so an inbound link from a blog, a forum or a chat is an ordinary way to
// arrive; mounting the guard on the route would refuse every one of them. That
// decision is stated in a comment and in the invariants — this is what enforces
// it, because the declared-set sweep would be edited in the same change that
// broke it.
func TestRegisterPageStaysReachableFromAnotherSite(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")
	crossSiteNavigation.applyTo(request)
	response := mustAppResponse(t, app, request)

	assertStatusCode(t, response, http.StatusOK)
	if body := mustReadBodyString(t, response.Body); !strings.Contains(body, "register-form") {
		t.Fatal("expected a cross-site visitor to still be served the signup form")
	}
}

// TestRefusedPickupRequestLeavesNoFlashWhenNothingWasPresented holds the other
// half of the same rule: a refusal must not leave state behind for a request the
// owner never made. A prefetch discards the body and keeps the Set-Cookie, so an
// unconditional flash would greet her with an error about a registration she
// never started.
func TestRefusedPickupRequestLeavesNoFlashWhenNothingWasPresented(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, registerPickupNextPath, nil)
	crossSiteNavigation.applyTo(request)
	response := mustAppResponse(t, app, request)

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the refusal to land on /login, got %q", location)
	}
	if flash := responseCookie(response.Cookies(), flashCookieName); flash != nil && strings.TrimSpace(flash.Value) != "" {
		t.Fatalf("a refusal with no pickup cookie must leave no flash behind, got %#v", flash)
	}
}

// TestRefusedCalendarFeedRequestIsAuditedOnlyWhenSomethingCouldBeSpent keeps
// the denied line meaningful. The guard runs before the handler that would have
// discovered nothing is armed, so auditing every refusal would fill the stream
// with stray cross-site links and prefetches on sessions with no feed — and an
// operator could not tell those from the one case the line exists to report.
func TestRefusedCalendarFeedRequestIsAuditedOnlyWhenSomethingCouldBeSpent(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "feed-reveal-guard-unarmed@example.com",
		onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	request := httptest.NewRequest(http.MethodGet, calendarFeedRevealPath, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	crossSiteNavigation.applyTo(request)
	response, logOutput := captureAuditedRequest(t, ctx.app, request)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected the refusal to land on /settings, got %q", location)
	}
	if strings.Contains(logOutput, "settings.calendar_feed_reveal") {
		t.Fatalf("a refusal with no reveal cookie had nothing to spend and must not be audited, got %q", logOutput)
	}
}

// TestFirstPartyGuardAdmitsAPartiallyStrippedFetchMetadataFamily applies the
// monotone rule per header rather than to the family as a whole. A proxy or
// security appliance that forwards Sec-Fetch-Site and drops Sec-Fetch-Dest is
// ordinary in that tier, and a guard that read the absent destination as "not
// one I allow" would refuse every request forever — locking the owner out of her
// recovery code, which is the failure this design explicitly rejects.
func TestFirstPartyGuardAdmitsAPartiallyStrippedFetchMetadataFamily(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	pickupCookie := registerAndExtractPickupCookie(t, app, "pickup-guard-partial-family@example.com")

	accepted := pickupRegisterWithHeaders(t, app, pickupCookie, secFetchHeaders{site: "same-origin"})
	assertStatusCode(t, accepted, http.StatusSeeOther)
	if location := accepted.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected a stripped-destination request to complete the pickup, got %q", location)
	}
	if authCookie := responseCookie(accepted.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatalf("expected a stripped-destination request to mint a session, got %#v", authCookie)
	}
}

// TestFirstPartyGuardAdmitsAClientThatSpeaksNoFetchMetadata pins the monotone
// half of the guard: a request carrying none of the header family lands where it
// landed before the guard existed. It is what keeps a browser too old to send
// Sec-Fetch — and every non-browser client — from losing a route outright, and
// it is why the rest of the suite can go on issuing bare requests.
func TestFirstPartyGuardAdmitsAClientThatSpeaksNoFetchMetadata(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	pickupCookie := registerAndExtractPickupCookie(t, app, "pickup-guard-headerless@example.com")

	request := httptest.NewRequest(http.MethodGet, registerPickupNextPath, nil)
	request.Header.Set("Cookie", registerPickupCookieName+"="+pickupCookie)
	response := mustAppResponse(t, app, request)

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected a headerless pickup to complete, got %q", location)
	}
}

// firstPartyGuardClosureMarker reads the guard factory's own name back off a
// closure it produced. The factory is small enough that the compiler inlines it
// into every call site, so each registration carries its own generated closure
// with its own code pointer and only the factory's name survives in it —
// comparing pointers finds nothing. Deriving the marker from the function rather
// than spelling it out keeps a rename from turning this sweep silently empty.
func firstPartyGuardClosureMarker() string {
	name := runtime.FuncForPC(reflect.ValueOf(requireFirstPartyRequest(nil)).Pointer()).Name()
	if index := strings.LastIndex(name, ".func"); index >= 0 {
		name = name[:index]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	return name
}

func registerAndExtractPickupCookie(t *testing.T, app *fiber.App, email string) string {
	t.Helper()

	form := url.Values{
		"email":            {email},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
		"consent":          {"1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	cookieValue := responseCookieValue(response.Cookies(), registerPickupCookieName)
	if cookieValue == "" {
		t.Fatal("expected a sealed register pickup cookie")
	}
	return cookieValue
}

func pickupRegisterWithHeaders(t *testing.T, app *fiber.App, pickupCookie string, headers secFetchHeaders) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, registerPickupNextPath, nil)
	request.Header.Set("Cookie", registerPickupCookieName+"="+pickupCookie)
	headers.applyTo(request)
	return mustAppResponse(t, app, request)
}

func revealCalendarFeedWithHeaders(t *testing.T, app *fiber.App, authCookie string, sealed string, headers secFetchHeaders) (*http.Response, string, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, calendarFeedRevealPath, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, calendarFeedRevealCookieName+"="+sealed))
	headers.applyTo(request)
	response, logOutput := captureAuditedRequest(t, app, request)
	return response, mustReadBodyString(t, response.Body), logOutput
}

// TestEveryGETRouteIsAlsoServedOnHEAD is the route-table half of HEAD parity:
// registerHEADTwins must leave no GET route without a HEAD route of its own, or
// a HEAD request to it falls through to whatever the composition root mounts
// after the routes — in the deployed app, the terminal NotFound catch-all.
//
// The chain comparison is what makes the count meaningful: a HEAD route
// answering something OTHER than its GET route would satisfy a presence check
// and still break parity. The one route that legitimately does is HEAD
// /api/v1/days/{date}, whose own question (does the day hold any data) is not
// what GET on that path answers — it is asserted here as the negative anchor,
// so a twin that started overwriting hand-written HEAD routes reddens.
func TestEveryGETRouteIsAlsoServedOnHEAD(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	headRoutesByPath := map[string][]fiber.Route{}
	for _, route := range app.GetRoutes(true) {
		if route.Method == fiber.MethodHead {
			headRoutesByPath[route.Path] = append(headRoutesByPath[route.Path], route)
		}
	}

	twins := 0
	ownChain := []string{}
	for _, route := range app.GetRoutes(true) {
		if route.Method != fiber.MethodGet {
			continue
		}
		heads := headRoutesByPath[route.Path]
		if len(heads) == 0 {
			t.Errorf("GET %s has no HEAD route: a HEAD request to it reaches whatever is mounted after the route table instead of its own handlers", route.Path)
			continue
		}
		if len(heads) > 1 {
			t.Errorf("GET %s has %d HEAD routes: the first one wins and the others are unreachable clutter", route.Path, len(heads))
			continue
		}
		if sameHandlerChain(route, heads[0]) {
			twins++
			continue
		}
		ownChain = append(ownChain, fiber.MethodHead+" "+route.Path)
	}

	if twins == 0 {
		t.Fatal("no GET route was mirrored by a HEAD route with the same chain; recheck route discovery before reading the comparison above")
	}
	want := []string{fiber.MethodHead + " /api/v1/days/:date"}
	sort.Strings(ownChain)
	if !reflect.DeepEqual(ownChain, want) {
		t.Fatalf("HEAD routes carrying a chain of their own drifted:\n  routed:   %v\n  expected: %v", ownChain, want)
	}
}

// TestShownOnceGETRoutesAreExactlyTheDeclaredSet is the class half of the HEAD
// refusal, the same shape TestFirstPartyGuardedRoutesAreExactlyTheDeclaredSet
// has: nothing in the route table says which GET spends a one-time mark, so the
// set is declared in routes.go and pinned here in BOTH directions — a refusal
// dropped from a route named there reddens, and a route that acquires one
// without being named reddens too.
//
// It builds a query-mode app because GET /auth/oidc/callback is registered only
// under OIDC_RESPONSE_MODE=query, and that callback consumes the one-time state
// cookie together with the provider's single-use authorization code.
func TestShownOnceGETRoutesAreExactlyTheDeclaredSet(t *testing.T) {
	t.Parallel()

	// The name is read from the production method value rather than typed as a
	// string: a rename moves the expectation with it. The nil receiver is never
	// dereferenced — a method value only captures it.
	marker := handlerFuncName((*Handler)(nil).refuseHEADOnShownOnceSurface)
	if !strings.Contains(marker, "refuseHEAD") {
		t.Fatalf("resolved middleware name %q does not name the HEAD refusal; the chain comparison below would be meaningless", marker)
	}

	stub := newStubOIDCWorkflowService(true)
	stub.responseMode = security.OIDCResponseModeQuery
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{oidcService: stub})

	refusing := []string{}
	for _, route := range app.GetRoutes(true) {
		if route.Method != fiber.MethodGet {
			continue
		}
		for position, routeHandler := range route.Handlers {
			if handlerFuncName(routeHandler) != marker {
				continue
			}
			if position != 0 {
				// A refusal behind AuthRequired or the first-party guard answers
				// a HEAD with their redirect instead of the 404 an unknown path
				// gets, and runs whatever they do on the way.
				t.Errorf("GET %s carries %s at position %d: the refusal has to be the first link in the chain", route.Path, marker, position)
			}
			refusing = append(refusing, route.Method+" "+route.Path)
			break
		}
	}

	declared := append([]string{}, shownOnceGETRoutes...)
	sort.Strings(refusing)
	sort.Strings(declared)
	if !reflect.DeepEqual(refusing, declared) {
		t.Fatalf("routes refusing HEAD drifted from the declared set:\n  routed:   %v\n  declared: %v", refusing, declared)
	}
	if len(declared) == 0 {
		t.Fatal("expected at least one declared shown-once route; recheck route discovery")
	}
}

// TestHeadOnOIDCStartDoesNotMintStateCookieOrStartTheHandshake is the
// behavioral half of GET /auth/oidc/start joining shownOnceGETRoutes:
// StartOIDCLogin mints a fresh one-time state cookie and starts the provider
// handshake before its 307 redirect, and a HEAD can neither follow that
// redirect nor use the cookie — it would only overwrite whatever state a
// concurrent GET login already staged in the same cookie jar. Both must never
// happen on HEAD, not merely fail to reach the caller.
func TestHeadOnOIDCStartDoesNotMintStateCookieOrStartTheHandshake(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{oidcService: stub})

	response := mustAppResponse(t, app, httptest.NewRequest(http.MethodHead, "/auth/oidc/start", nil))
	assertStatusCode(t, response, http.StatusNotFound)
	if stub.lastStartState != "" {
		t.Fatalf("expected HEAD /auth/oidc/start to never reach the provider handshake, got state %q", stub.lastStartState)
	}
	if cookie := responseCookie(response.Cookies(), oidcStateCookieName); cookie != nil {
		t.Fatalf("expected HEAD /auth/oidc/start to mint no state cookie, got Set-Cookie %#v", cookie)
	}
}
