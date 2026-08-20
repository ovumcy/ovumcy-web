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
