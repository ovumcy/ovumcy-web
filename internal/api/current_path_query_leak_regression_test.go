package api

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The shared layout renders the current request path twice on every page: into
// the language switcher's hidden `next` field, and into the outgoing
// `/privacy?back=…` link. Whatever the caller put in the query therefore lands
// in the markup of the page and, through the footer link, in a new outgoing URL
// — browser history, Referer, and the /privacy access log. A crafted link such
// as /login?email=victim@example.com is enough to plant it. The receiving sides
// sanitize the value, so this is not an open redirect; the defect is that the
// value is echoed at all, against the "no email or error codes in URLs" rule.

const (
	// currentPathLeakLocalPart is the recognisable half of the planted address;
	// it survives every encoding the layout can apply, including the double
	// encoding the footer link produces.
	currentPathLeakLocalPart = "query-source"
	currentPathLeakAddress   = currentPathLeakLocalPart + "@example.com"
	currentPathLeakErrorCode = "invalid credentials"
)

var (
	currentPathNextFieldPattern   = regexp.MustCompile(`name="next" value="([^"]*)"`)
	currentPathPrivacyLinkPattern = regexp.MustCompile(`href="/privacy\?back=([^"]*)"`)
)

// currentPathLeakForms lists every spelling the planted values can reach the
// markup in. The percent-encoded form is the one that actually appears when the
// query is built with url.Values.Encode, so a guard checking only the raw form
// does not see this defect at all; the doubly encoded form is what the footer
// link produces once the layout escapes the path again.
func currentPathLeakForms() []string {
	return []string{
		currentPathLeakAddress,
		url.QueryEscape(currentPathLeakAddress),
		url.QueryEscape(url.QueryEscape(currentPathLeakAddress)),
		currentPathLeakLocalPart,
		url.QueryEscape(currentPathLeakErrorCode),
		url.QueryEscape(url.QueryEscape(currentPathLeakErrorCode)),
		strings.ReplaceAll(currentPathLeakErrorCode, " ", "%20"),
	}
}

func assertNoCurrentPathLeak(t *testing.T, label string, body string) {
	t.Helper()

	for _, form := range currentPathLeakForms() {
		if strings.Contains(body, form) {
			t.Fatalf("%s: rendered page carries the caller-supplied query value %q", label, form)
		}
	}
}

func currentPathNextField(t *testing.T, label string, body string) string {
	t.Helper()

	match := currentPathNextFieldPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("%s: expected the language switcher's next field to render", label)
	}
	return match[1]
}

func currentPathPrivacyBack(t *testing.T, label string, body string) string {
	t.Helper()

	match := currentPathPrivacyLinkPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("%s: expected the footer privacy link to render", label)
	}
	return match[1]
}

// TestPublicPageDropsUnlistedQueryParametersFromTheRenderedPath drives the real
// app: a crafted login link must not put the address it carries into the page.
// Both spellings of the query are requested, because a raw `@` survives the
// request line untouched while url.Values.Encode sends `%40`.
func TestPublicPageDropsUnlistedQueryParametersFromTheRenderedPath(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	encoded := url.Values{"email": {currentPathLeakAddress}, "error": {currentPathLeakErrorCode}}.Encode()
	spellings := map[string]string{
		"percent-encoded query": "/login?" + encoded,
		"raw query":             "/login?email=" + currentPathLeakAddress + "&error=invalid+credentials",
	}

	for label, target := range spellings {
		body := smokeGET(t, app, "", target, http.StatusOK)

		assertNoCurrentPathLeak(t, label, body)
		if got := currentPathNextField(t, label, body); got != "/login" {
			t.Fatalf("%s: expected the rendered path to be /login, got %q", label, got)
		}
		if got := currentPathPrivacyBack(t, label, body); got != url.QueryEscape("/login") {
			t.Fatalf("%s: expected the privacy link to carry /login, got %q", label, got)
		}
	}
}

// TestPrivacyPageKeepsItsAllowlistedBackParameter is the positive anchor for the
// public half: the page's own parameter must survive the filter, so the fix
// cannot be "drop the query".
func TestPrivacyPageKeepsItsAllowlistedBackParameter(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	label := "privacy back parameter"

	body := smokeGET(t, app, "", "/privacy?back=%2Fdashboard&email="+url.QueryEscape(currentPathLeakAddress), http.StatusOK)

	assertNoCurrentPathLeak(t, label, body)
	if got := currentPathNextField(t, label, body); got != "/privacy?back=%2Fdashboard" {
		t.Fatalf("%s: expected the allowlisted back parameter to survive, got %q", label, got)
	}
}

// TestPublicPageDropsAHostileValueUnderAnAllowlistedKey pins the other half of
// the allowlist: the names are not enough on their own. `step` is a key the
// pages really read, so a key-only filter waves `?step=victim@example.com`
// straight through and renders exactly what `?email=` would have.
func TestPublicPageDropsAHostileValueUnderAnAllowlistedKey(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	for label, target := range map[string]string{
		"hostile step":     "/login?step=" + currentPathLeakAddress,
		"hostile month":    "/login?month=" + currentPathLeakAddress,
		"hostile selected": "/login?selected=" + currentPathLeakAddress,
		"hostile edit":     "/login?edit=" + currentPathLeakAddress,
		"hostile back":     "/login?back=" + currentPathLeakAddress,
		// A fragment is deliberately not exercised here: a browser never sends
		// "#" to the server, and httptest does, so the request 404s instead of
		// rendering a page. It is reachable only inside a `back` value, which
		// TestPrivacyPageFiltersEveryCarrierReachedThroughItsBackParameter drives.
	} {
		body := smokeGET(t, app, "", target, http.StatusOK)

		assertNoCurrentPathLeak(t, label, body)
		if got := currentPathNextField(t, label, body); got != "/login" {
			t.Fatalf("%s: expected the rendered path to be /login, got %q", label, got)
		}
	}
}

// TestPrivacyPageFiltersTheQuerySmuggledInsideItsBackParameter closes the same
// leak one layer down: `back` is allowlisted, but its value is itself a path
// that can carry a query, so a name-only allowlist would wave the address
// through inside an allowed value.
func TestPrivacyPageFiltersTheQuerySmuggledInsideItsBackParameter(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)
	label := "back parameter carrying its own query"

	smuggled := "/login?email=" + currentPathLeakAddress
	body := smokeGET(t, app, "", "/privacy?back="+url.QueryEscape(smuggled), http.StatusOK)

	assertNoCurrentPathLeak(t, label, body)
	if got := currentPathNextField(t, label, body); got != "/privacy?back=%2Flogin" {
		t.Fatalf("%s: expected the smuggled query to be filtered out of back, got %q", label, got)
	}
}

// TestPrivacyPageFiltersEveryCarrierReachedThroughItsBackParameter proves the
// nested level gets the same treatment as the top one and not a weaker one:
// a fragment, a hostile value under an allowlisted key, and a back value that is
// not a local route at all must each be filtered inside `back` exactly as they
// are outside it.
func TestPrivacyPageFiltersEveryCarrierReachedThroughItsBackParameter(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	for label, carrier := range map[string]struct{ smuggled, want string }{
		"fragment inside back":                  {"/login#email=" + currentPathLeakAddress, "/privacy?back=%2Flogin"},
		"hostile allowlisted key inside back":   {"/calendar?month=" + currentPathLeakAddress, "/privacy?back=%2Fcalendar"},
		"address as the whole back value":       {currentPathLeakAddress, "/privacy"},
		"address in the back path itself":       {"/" + currentPathLeakAddress, "/privacy"},
		"absolute url as the back value":        {"https://evil.example/" + currentPathLeakAddress, "/privacy"},
		"protocol-relative url as back":         {"//evil.example/" + currentPathLeakAddress, "/privacy"},
		"legitimate month survives inside back": {"/calendar?month=2026-08", "/privacy?back=%2Fcalendar%3Fmonth%3D2026-08"},
	} {
		body := smokeGET(t, app, "", "/privacy?back="+url.QueryEscape(carrier.smuggled), http.StatusOK)

		assertNoCurrentPathLeak(t, label, body)
		if got := currentPathNextField(t, label, body); got != carrier.want {
			t.Fatalf("%s: expected rendered path %q, got %q", label, carrier.want, got)
		}
	}
}

// TestSignedInPageKeepsItsAllowlistedQueryParameters covers the authenticated
// half, where the layout renders the path into the outgoing privacy link and,
// on onboarding, into the switcher field: the calendar's month anchor and the
// onboarding step must survive, and the planted values must not.
func TestSignedInPageKeepsItsAllowlistedQueryParameters(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	hostile := "&email=" + url.QueryEscape(currentPathLeakAddress) + "&error=" + url.QueryEscape(currentPathLeakErrorCode)

	settled := createOnboardingTestUser(t, database, "current-path-settled@example.com", "StrongPass1", true)
	settledCookie := loginAndExtractAuthCookie(t, app, settled.Email, "StrongPass1")

	calendarLabel := "calendar month anchor"
	calendarBody := smokeGET(t, app, settledCookie, "/calendar?month=2026-02"+hostile, http.StatusOK)
	assertNoCurrentPathLeak(t, calendarLabel, calendarBody)
	if got := currentPathPrivacyBack(t, calendarLabel, calendarBody); got != url.QueryEscape("/calendar?month=2026-02") {
		t.Fatalf("%s: expected the allowlisted month to survive, got %q", calendarLabel, got)
	}

	starting := createOnboardingTestUser(t, database, "current-path-starting@example.com", "StrongPass1", false)
	startingCookie := loginAndExtractAuthCookie(t, app, starting.Email, "StrongPass1")

	onboardingLabel := "onboarding step"
	onboardingBody := smokeGET(t, app, startingCookie, "/onboarding?step=2"+hostile, http.StatusOK)
	assertNoCurrentPathLeak(t, onboardingLabel, onboardingBody)
	if got := currentPathNextField(t, onboardingLabel, onboardingBody); got != "/onboarding?step=2" {
		t.Fatalf("%s: expected the allowlisted step to survive, got %q", onboardingLabel, got)
	}
}

// TestNotFoundPageDropsTheCallerSuppliedPathFromTheRenderedAddress closes the
// same leak one level up. On a 404 the whole path is caller-chosen, not merely
// its query, and the browser branch renders the full layout — so
// /query-source@example.com/nowhere puts that address into the switcher field
// and into the outgoing /privacy?back=… link exactly as ?email= did on a routed
// page. The handler pins the rendered address itself, so nothing the caller
// wrote survives into the markup.
func TestNotFoundPageDropsTheCallerSuppliedPathFromTheRenderedAddress(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	target := "/" + currentPathLeakAddress + "/nowhere"

	// The two sessions render different halves of the layout — the switcher
	// field is signed-out only, the primary navigation signed-in only — so they
	// are separate subtests: a failure in one must not hide the other.
	t.Run("signed out", func(t *testing.T) {
		label := "signed-out not-found path"
		body := smokeGET(t, app, "", target, http.StatusNotFound)

		assertNoCurrentPathLeak(t, label, body)
		if got := currentPathNextField(t, label, body); got != notFoundRenderedPath {
			t.Fatalf("%s: expected the rendered path to be %q, got %q", label, notFoundRenderedPath, got)
		}
		if got := currentPathPrivacyBack(t, label, body); got != url.QueryEscape(notFoundRenderedPath) {
			t.Fatalf("%s: expected the privacy link to carry %q, got %q", label, notFoundRenderedPath, got)
		}
	})

	t.Run("signed in", func(t *testing.T) {
		label := "signed-in not-found path"
		owner := createOnboardingTestUser(t, database, "current-path-not-found@example.com", "StrongPass1", true)
		ownerCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")
		body := smokeGET(t, app, ownerCookie, target, http.StatusNotFound)

		assertNoCurrentPathLeak(t, label, body)
		if got := currentPathPrivacyBack(t, label, body); got != url.QueryEscape(notFoundRenderedPath) {
			t.Fatalf("%s: expected the privacy link to carry %q, got %q", label, notFoundRenderedPath, got)
		}
		// This session renders the primary navigation, and every active state in
		// it derives from the same rendered address. A substitute naming a real
		// section would light that section's tab and announce aria-current on a
		// page that is not it, so the address must match no navigation route.
		if strings.Contains(body, `aria-current="page"`) {
			t.Fatalf("%s: the rendered address lit a navigation tab on a page that is not it", label)
		}
	})
}
