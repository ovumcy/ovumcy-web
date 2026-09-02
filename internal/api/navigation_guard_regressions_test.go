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
)

// secFetchHeaders is one browser's account of who started a request. The three
// shapes below are the ones that decide the guard: a top-level navigation the
// owner performed on this origin, the same navigation started from someone
// else's page, and a subresource load that is not a navigation at all.
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
	// A speculative load wears the navigation's own clothes and is separated
	// from it by Sec-Purpose alone.
	sameOriginPrefetch = secFetchHeaders{site: "same-origin", mode: "navigate", dest: "document", purpose: "prefetch;prerender"}
)

func (headers secFetchHeaders) applyTo(request *http.Request) {
	request.Header.Set(headerSecFetchSite, headers.site)
	request.Header.Set(headerSecFetchMode, headers.mode)
	request.Header.Set(headerSecFetchDest, headers.dest)
	if headers.purpose != "" {
		request.Header.Set(headerSecPurpose, headers.purpose)
	}
}

// TestNavigationGuardedRoutesAreExactlyTheDeclaredSet is the class half of the
// guard. Nothing in the route table says which GET mutates state, so the set is
// declared in routes.go and pinned here in BOTH directions: a guard dropped from
// a route it is named for reddens, and a route that quietly acquires one without
// being named reddens too.
func TestNavigationGuardedRoutesAreExactlyTheDeclaredSet(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	marker := navigationGuardClosureMarker() + ".func"

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

	declared := append([]string{}, navigationGuardedRoutes...)
	sort.Strings(guarded)
	sort.Strings(declared)
	if !reflect.DeepEqual(guarded, declared) {
		t.Fatalf("navigation-guarded routes drifted from the declared set:\n  routed:   %v\n  declared: %v", guarded, declared)
	}
	if len(declared) == 0 {
		t.Fatal("expected at least one declared navigation-guarded route; recheck route discovery")
	}
}

// TestRegisterPickupRefusesForeignNavigationWithoutSpendingTheNonce holds the
// property that matters: a refusal must not cost the owner the thing the forged
// request was after. The nonce is single-use, so the second half of each case —
// the owner's own navigation still completing — is what separates a guard from a
// denial of service.
func TestRegisterPickupRefusesForeignNavigationWithoutSpendingTheNonce(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		headers secFetchHeaders
	}{
		{name: "cross-site navigation", headers: crossSiteNavigation},
		{name: "same-origin fetch", headers: sameOriginFetch},
		{name: "same-origin prefetch", headers: sameOriginPrefetch},
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

// TestCalendarFeedRevealRefusesForeignNavigationWithoutSpendingTheMark is the
// same property on the other guarded route. The reveal is claimed by a
// compare-and-set that never resets, so a refusal that burned the mark — or
// retracted the sealed cookie — would hand the forged request exactly the
// outcome the guard exists to deny.
func TestCalendarFeedRevealRefusesForeignNavigationWithoutSpendingTheMark(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		headers secFetchHeaders
	}{
		{name: "cross-site navigation", headers: crossSiteNavigation},
		{name: "same-origin fetch", headers: sameOriginFetch},
		{name: "same-origin prefetch", headers: sameOriginPrefetch},
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
			assertHealthEgressAudited(t, logOutput, "settings.calendar_feed_reveal", "denied", "calendar_feed")

			accepted, acceptedBody, _ := revealCalendarFeedWithHeaders(t, ctx.app, ctx.authCookie, sealed.Value, sameOriginNavigation)
			defer func() { _ = accepted.Body.Close() }()
			assertStatusCode(t, accepted, http.StatusOK)
			if !strings.Contains(acceptedBody, "/calendar/feed/") {
				t.Fatal("expected the owner's own navigation to still reveal the subscribe URL")
			}
		})
	}
}

// TestRefusedCalendarFeedNavigationIsAuditedOnlyWhenSomethingCouldBeSpent keeps
// the denied line meaningful. The guard runs before the handler that would have
// discovered nothing is armed, so auditing every refusal would fill the stream
// with stray cross-site links and prefetches on sessions with no feed — and an
// operator could not tell those from the one case the line exists to report.
func TestRefusedCalendarFeedNavigationIsAuditedOnlyWhenSomethingCouldBeSpent(t *testing.T) {
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

// TestNavigationGuardAdmitsAClientThatSpeaksNoFetchMetadata pins the monotone
// half of the guard: a request carrying none of the header family lands where it
// landed before the guard existed. It is what keeps a browser too old to send
// Sec-Fetch — and every non-browser client — from losing a route outright, and
// it is why the rest of the suite can go on issuing bare requests.
func TestNavigationGuardAdmitsAClientThatSpeaksNoFetchMetadata(t *testing.T) {
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

// navigationGuardClosureMarker reads the guard factory's own name back off a
// closure it produced. The factory is small enough that the compiler inlines it
// into every call site, so each registration carries its own generated closure
// with its own code pointer and only the factory's name survives in it —
// comparing pointers finds nothing. Deriving the marker from the function rather
// than spelling it out keeps a rename from turning this sweep silently empty.
func navigationGuardClosureMarker() string {
	name := runtime.FuncForPC(reflect.ValueOf(requireSameOriginNavigation(nil)).Pointer()).Name()
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
