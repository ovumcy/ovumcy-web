package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Calendar-feed reveal — the server-verified half of the cookie's lifetime.
//
// A narrow sealed-cookie invariant, so it keeps its own file rather than
// joining the settings aggregator: what is under test is the payload contract,
// not the calendar-feed settings surface.
//
// calendarFeedRevealCookieTTL reaches the Set-Cookie `Expires` attribute, which
// is a hint the client is free to ignore, and the codec's open() has no notion
// of time. Without an expiry inside the sealed payload a retained value stays
// honored until the token is rotated or SECRET_KEY changes — months, for a
// cookie advertised as living twenty minutes. The consumption mark
// (users.calendar_feed_revealed_at, migration 036) closes a reveal that already
// happened; it says nothing about a mint whose reveal was never consumed, which
// is exactly the value these guards present.
//
// Both guards run their refusal on a SECOND owner, for the reason the recovery
// twin states: a reveal spends the presenting account's mark, so anchoring and
// refusing on one account would let the mark answer for the expiry and leave it
// untested.

// TestCalendarFeedRevealRefusesAnExpiredRevealCookie is the primary guard: a
// payload whose `expires_at` is already past is refused on the owner's OWN
// authenticated session — the case a browser hint cannot reach, since the
// client that kept the sealed value is the one deciding whether to send it.
//
// The refusal is asserted on three surfaces, because a page that redirects is
// not proof on its own: the landing (/settings, where an absent cookie lands),
// the body (no subscribe URL in it), and the audit stream (no
// settings.calendar_feed_reveal egress line — an operator counting reveals must
// count disclosures, and a refusal disclosed nothing).
func TestCalendarFeedRevealRefusesAnExpiredRevealCookie(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "feed-reveal-expired-anchor@example.com",
		onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	assertCalendarFeedRevealAnchorStillShowsTheURL(t, ctx)

	subject, subjectAuth, subjectURL := newCalendarFeedRevealSubject(t, ctx, "feed-reveal-expired-subject@example.com")
	expired := sealedCalendarFeedRevealCookieForTest(t, subject.ID, subjectURL, time.Now().Add(-time.Minute))
	assertCalendarFeedRevealRefusedAndUnaudited(t, ctx.app, subjectAuth, expired, subjectURL)
}

// TestCalendarFeedRevealRefusesARevealCookieCarryingNoExpiry covers the payload
// shape minted before the field existed: it opens, it names the right owner,
// and it says nothing about when it stops being honored. An absent bound is
// invalid input rather than permission to reveal, the same treatment a payload
// naming no owner gets — pinned here so a later tolerance for the legacy shape
// reddens instead of quietly restoring the unbounded window.
//
// The cost is bounded and one-sided: a cookie minted in the twenty minutes
// before this deploy dies, and its owner lands on /settings and rotates — the
// same place an absent cookie lands.
func TestCalendarFeedRevealRefusesARevealCookieCarryingNoExpiry(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "feed-reveal-unbounded-anchor@example.com",
		onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	assertCalendarFeedRevealAnchorStillShowsTheURL(t, ctx)

	subject, subjectAuth, subjectURL := newCalendarFeedRevealSubject(t, ctx, "feed-reveal-unbounded-subject@example.com")
	unbounded := sealedUnboundedCalendarFeedRevealCookieForTest(t, subject.ID, subjectURL)
	assertCalendarFeedRevealRefusedAndUnaudited(t, ctx.app, subjectAuth, unbounded, subjectURL)
}

// assertCalendarFeedRevealAnchorStillShowsTheURL is the positive anchor both
// guards need: it drives the REAL generate flow for the context's own owner and
// requires the reveal page to hand over the URL and audit the egress. Without
// it a reader that refuses everybody would satisfy every assertion below.
func assertCalendarFeedRevealAnchorStillShowsTheURL(t *testing.T, ctx settingsSecurityTestContext) {
	t.Helper()

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	defer func() { _ = generated.Body.Close() }()
	if generated.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on feed generate, got %d", generated.StatusCode)
	}
	sealed := responseCookie(generated.Cookies(), calendarFeedRevealCookieName)
	if sealed == nil || strings.TrimSpace(sealed.Value) == "" {
		t.Fatal("expected a sealed reveal cookie on the generate response")
	}

	response, body, logOutput := revealCalendarFeedWithSealedCookie(t, ctx.app, ctx.authCookie, sealed.Value)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected a freshly minted reveal cookie to still render the page, got %d", response.StatusCode)
	}
	if !strings.Contains(body, "/calendar/feed/") {
		t.Fatal("expected the anchor reveal to carry the subscribe URL")
	}
	_ = assertHealthEgressAudited(t, logOutput, "settings.calendar_feed_reveal", "success", "calendar_feed")
}

// assertCalendarFeedRevealRefusedAndUnaudited presents one sealed payload on
// the session it was minted for and requires the reveal page to refuse it
// whole: the /settings landing, no subscribe URL anywhere in the body, no
// egress line, and the cookie retracted the way every other refusal path in
// readCalendarFeedRevealState retracts it.
func assertCalendarFeedRevealRefusedAndUnaudited(t *testing.T, app *fiber.App, authCookie string, sealed string, refusedURL string) {
	t.Helper()

	response, body, logOutput := revealCalendarFeedWithSealedCookie(t, app, authCookie, sealed)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a reveal cookie past its own bound to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected the refusal to land on /settings, got %q", location)
	}
	if strings.Contains(body, "/calendar/feed/") || strings.Contains(body, refusedURL) {
		t.Fatal("a refused reveal must not carry the subscribe URL")
	}
	if strings.Contains(logOutput, "settings.calendar_feed_reveal") {
		t.Fatalf("a visit that revealed nothing must not log a reveal, got %q", logOutput)
	}
	assertRevealCookieCleared(t, response, calendarFeedRevealCookieName)
}

// revealCalendarFeedWithSealedCookie drives one authenticated GET of the reveal
// page carrying the given sealed cookie value, returning the response, its body
// and the security-event output the request produced.
func revealCalendarFeedWithSealedCookie(t *testing.T, app *fiber.App, authCookie string, sealed string) (*http.Response, string, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, calendarFeedRevealPath, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, calendarFeedRevealCookieName+"="+sealed))
	response, logOutput := captureAuditedRequest(t, app, request)
	return response, mustReadBodyString(t, response.Body), logOutput
}

// newCalendarFeedRevealSubject creates a second owner on the same instance,
// signs it in, and arms a real feed token for it. The subject is fresh, so its
// reveal mark is unclaimed: nothing but the payload's own bound stands between
// the sealed value and a disclosure.
func newCalendarFeedRevealSubject(t *testing.T, ctx settingsSecurityTestContext, email string) (models.User, string, string) {
	t.Helper()

	subject := createOnboardingTestUser(t, ctx.database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, ctx.app, subject.Email, "StrongPass1")
	token := armCalendarFeedForUser(t, ctx.database, subject.ID)
	return subject, authCookie, "https://ovumcy.example" + calendarFeedURL(token)
}

// sealedCalendarFeedRevealCookieForTest seals a reveal payload naming the
// moment it stops being honored. The JSON is built by hand rather than through
// the production struct so the guard states the wire shape it defends.
func sealedCalendarFeedRevealCookieForTest(t *testing.T, userID uint, feedURL string, expiresAt time.Time) string {
	t.Helper()
	return sealCalendarFeedRevealFieldsForTest(t, map[string]any{
		"uid":        userID,
		"feed_url":   feedURL,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	})
}

// sealedUnboundedCalendarFeedRevealCookieForTest seals the pre-expiry payload
// shape: well-formed, correctly attributed, and carrying no bound at all.
func sealedUnboundedCalendarFeedRevealCookieForTest(t *testing.T, userID uint, feedURL string) string {
	t.Helper()
	return sealCalendarFeedRevealFieldsForTest(t, map[string]any{
		"uid":      userID,
		"feed_url": feedURL,
	})
}

func sealCalendarFeedRevealFieldsForTest(t *testing.T, fields map[string]any) string {
	t.Helper()
	serialized, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal reveal payload: %v", err)
	}
	return sealCookieForTestApp(t, calendarFeedRevealCookieName, serialized)
}
