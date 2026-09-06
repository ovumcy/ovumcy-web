package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Calendar (.ics) feed settings lifecycle — API regressions (slice 4). These
// pin the security-critical transport contract: the subscribe URL (a bearer
// secret) is revealed exactly once via a sealed one-time cookie, never
// re-rendered on a later settings load; rotate kills the old URL; revoke clears
// the columns; and every mutation is owner-scoped.

func reloadUserForCalendarFeedAPI(t *testing.T, ctx settingsSecurityTestContext, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// followCalendarFeedReveal performs the one-time reveal GET carrying the sealed
// reveal cookie set by a generate/rotate response, and returns the reveal page
// body plus the response (so a caller can re-check that a second visit is empty).
func followCalendarFeedReveal(t *testing.T, ctx settingsSecurityTestContext, revealCookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	if revealCookie == nil {
		t.Fatal("expected a sealed reveal cookie on the generate/rotate response")
	}
	request := httptest.NewRequest(http.MethodGet, "/settings/calendar-feed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(revealCookie)))
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar feed reveal request failed: %v", err)
	}
	return response, mustReadBodyString(t, response.Body)
}

// TestCalendarFeedGenerateRevealsURLOnceAndNeverAgain is the core secret
// contract: generate persists a HASHED token (never plaintext), reveals the full
// subscribe URL exactly once on the reveal page, and neither a second reveal
// visit nor a fresh settings render ever shows the token again.
func TestCalendarFeedGenerateRevealsURLOnceAndNeverAgain(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-generate-once@example.com")

	gen := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	if gen.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on feed generate, got %d", gen.StatusCode)
	}
	if loc := gen.Header.Get("Location"); loc != "/settings/calendar-feed" {
		t.Fatalf("expected redirect to the reveal page, got %q", loc)
	}
	// The response body must NOT contain the token/URL — the secret only ever
	// leaves via the sealed reveal cookie.
	genBody := mustReadBodyString(t, gen.Body)
	if strings.Contains(genBody, "/calendar/feed/") {
		t.Fatal("generate response body must not contain the subscribe URL")
	}

	// A HASHED token is persisted (selector + keyed verifier MAC + bcrypt verifier
	// hash), and the stored columns are not the plaintext token.
	stored := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if stored.CalendarFeedSelector == "" || stored.CalendarFeedVerifierHash == "" {
		t.Fatal("expected a feed token persisted after generate")
	}
	if !strings.HasPrefix(stored.CalendarFeedVerifierHash, "$2") {
		t.Fatalf("expected a bcrypt verifier hash at rest, got %q", stored.CalendarFeedVerifierHash)
	}
	// The keyed MAC is what the feed endpoint compares; without it a freshly minted
	// subscription would be pinned to the ~265 ms bcrypt path forever.
	if stored.CalendarFeedVerifierMAC == "" {
		t.Fatal("expected a keyed verifier MAC at rest after generate")
	}

	// Reveal exactly once: the reveal page shows the full subscribe URL and the
	// URL resolves the owner's feed (proving it is the real token).
	revealCookie := responseCookie(gen.Cookies(), calendarFeedRevealCookieName)
	revealResp, revealBody := followCalendarFeedReveal(t, ctx, revealCookie)
	_ = revealResp.Body.Close()
	if revealResp.StatusCode != http.StatusOK {
		t.Fatalf("expected reveal page 200, got %d", revealResp.StatusCode)
	}
	document := mustParseHTMLDocument(t, revealBody)
	urlNode := htmlElementByID(document, "calendar-feed-url")
	if urlNode == nil {
		t.Fatal("expected the reveal page to carry the subscribe URL element")
	}
	revealedURL := strings.TrimSpace(htmlNodeText(urlNode))
	if !strings.Contains(revealedURL, "/calendar/feed/") || !strings.HasSuffix(revealedURL, ".ics") {
		t.Fatalf("expected a /calendar/feed/<token>.ics URL revealed, got %q", revealedURL)
	}
	// Extract the token path and prove it actually serves the feed. ctx.app
	// mounts testCSRFMiddlewareConfig, which carries the same calendar-feed
	// exemption production does, so this GET is held to the full armed-feed
	// contract like every other mustServeCalendarFeed caller.
	token := extractFeedTokenFromURL(t, revealedURL)
	mustServeCalendarFeed(t, ctx.app, token, "for the just-revealed URL")

	// The reveal page retracts the cookie...
	clearedCookie := responseCookie(revealResp.Cookies(), calendarFeedRevealCookieName)
	if clearedCookie == nil || strings.TrimSpace(clearedCookie.Value) != "" {
		t.Fatal("expected the reveal page to clear the one-time cookie")
	}
	// ...and the reveal is ONE-TIME independently of whether the client obeyed
	// that retraction. The second visit therefore presents the ORIGINAL sealed
	// value, which is what a client that kept it holds — replaying the cleared
	// (empty) value would only prove that an empty cookie redirects, a property
	// this test asserted for a year while the replay it is named for worked.
	secondResp, secondBody := followCalendarFeedReveal(t, ctx, revealCookie)
	_ = secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a replay of the original sealed reveal cookie to redirect, got %d", secondResp.StatusCode)
	}
	if strings.Contains(secondBody, token) {
		t.Fatal("a replayed reveal cookie must not show the token again")
	}

	// A fresh settings render must never contain the plaintext token: it shows
	// only configured status.
	settingsReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	settingsReq.Header.Set("Accept-Language", "en")
	settingsReq.Header.Set("Cookie", ctx.authCookie)
	settingsResp, err := ctx.app.Test(settingsReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings render failed: %v", err)
	}
	defer func() { _ = settingsResp.Body.Close() }()
	settingsBody := mustReadBodyString(t, settingsResp.Body)
	if strings.Contains(settingsBody, token) {
		t.Fatal("settings page must never re-render the feed token")
	}
	settingsDoc := mustParseHTMLDocument(t, settingsBody)
	// A freshly minted link is stamped with the epoch this instance derives from
	// its own key, which is the only state in which the ledger says the link can
	// still be cut off here. It deliberately does not say the link is "active":
	// the verifier is not stored and its MAC cannot be recomputed, so that is not
	// a claim this code could ever be shown wrong about.
	if htmlElementByAttr(settingsDoc, "data-egress-feed-state", "issued_current_key") == nil {
		t.Fatal("expected the settings feed state to report 'issued_current_key'")
	}
}

// TestCalendarFeedRevealRefusesAReplayedCookieAndRearmsOnRotate is the
// consumption-mark contract for the subscribe URL, in the shape a client that
// kept the sealed value can actually produce.
//
// Retracting the reveal cookie is a request to a browser, not a record: nothing
// stopped that client from handing the same sealed value back on its own
// session, and this cookie carries no payload expiry, so the window closed only
// when the token was rotated, the feed revoked, or SECRET_KEY changed. What
// closes it is users.calendar_feed_revealed_at, claimed by the reveal page with
// a compare-and-set.
//
// Three legs, because refusing forever would satisfy the first two on its own:
//   - the first reveal still shows the URL (positive anchor, without which the
//     refusals below are green against a page that shows nobody anything)
//   - the ORIGINAL sealed value, replayed on the same session, is refused, lands
//     where an absent cookie lands, and carries no URL in the response
//   - a rotate mints a new token and re-arms the mark in the same write, so the
//     new reveal works — the mark tracks the outstanding reveal, it does not
//     retire the surface
func TestCalendarFeedRevealRefusesAReplayedCookieAndRearmsOnRotate(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-reveal-replay@example.com")

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	assertStatusCode(t, generated, http.StatusSeeOther)
	sealedReveal := responseCookie(generated.Cookies(), calendarFeedRevealCookieName)

	firstToken := assertCalendarFeedRevealShowsURL(t, ctx, sealedReveal)
	assertCalendarFeedRevealRefused(t, ctx, sealedReveal, firstToken)

	rotated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	assertStatusCode(t, rotated, http.StatusSeeOther)
	rotatedReveal := responseCookie(rotated.Cookies(), calendarFeedRevealCookieName)

	rotatedToken := assertCalendarFeedRevealShowsURL(t, ctx, rotatedReveal)
	if rotatedToken == firstToken {
		t.Fatal("expected the rotate to mint a different token than the generate")
	}
	// And the re-armed mark belongs to the new reveal only: the rotated cookie is
	// spent too, and the ORIGINAL cookie stays refused rather than becoming
	// presentable again on the back of someone else's re-arm.
	assertCalendarFeedRevealRefused(t, ctx, rotatedReveal, rotatedToken)
	assertCalendarFeedRevealRefused(t, ctx, sealedReveal, firstToken)
}

// assertCalendarFeedRevealShowsURL drives one reveal that must succeed and
// returns the token it displayed, so a caller can assert against the value that
// actually reached the page rather than one it re-typed.
func assertCalendarFeedRevealShowsURL(t *testing.T, ctx settingsSecurityTestContext, sealed *http.Cookie) string {
	t.Helper()

	response, body := followCalendarFeedReveal(t, ctx, sealed)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected the armed reveal to render, got %d", response.StatusCode)
	}
	urlNode := htmlElementByID(mustParseHTMLDocument(t, body), "calendar-feed-url")
	if urlNode == nil {
		t.Fatal("expected the reveal page to carry the subscribe URL element")
	}
	return extractFeedTokenFromURL(t, strings.TrimSpace(htmlNodeText(urlNode)))
}

// assertCalendarFeedRevealRefused states both halves of a refusal: the owner
// lands on /settings — where an absent cookie lands — and the response carries
// no subscribe URL.
func assertCalendarFeedRevealRefused(t *testing.T, ctx settingsSecurityTestContext, sealed *http.Cookie, refusedToken string) {
	t.Helper()

	response, body := followCalendarFeedReveal(t, ctx, sealed)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a spent reveal to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected the refusal to land on /settings, got %q", location)
	}
	if strings.Contains(body, refusedToken) || strings.Contains(body, "/calendar/feed/") {
		t.Fatal("a refused reveal must not carry the subscribe URL")
	}
	assertRevealCookieCleared(t, response, calendarFeedRevealCookieName)
}

// TestCalendarFeedGenerateJSONReturnsRevealPathNotURL proves the JSON branch of
// generate/rotate never returns the subscribe URL: a JSON client gets only the
// next-path to the one-time reveal and a status, so the secret still leaves
// exclusively via the sealed reveal cookie. Covers both generate and rotate.
func TestCalendarFeedGenerateJSONReturnsRevealPathNotURL(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-json-branch@example.com")

	for _, tc := range []struct {
		name       string
		path       string
		wantStatus string
	}{
		{"generate", "/api/v1/users/current/calendar-feed", "calendar_feed_generated"},
		{"rotate", "/api/v1/users/current/calendar-feed/rotate", "calendar_feed_rotated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, tc.path, url.Values{}, map[string]string{
				"Accept": "application/json",
			})
			assertStatusCode(t, resp, http.StatusOK)

			body := mustReadBodyString(t, resp.Body)
			if strings.Contains(body, "/calendar/feed/") {
				t.Fatalf("%s JSON response must not contain the subscribe URL, got %q", tc.name, body)
			}
			if !strings.Contains(body, "\"next_path\":\"/settings/calendar-feed\"") {
				t.Fatalf("%s JSON response should carry the reveal next_path, got %q", tc.name, body)
			}
			if !strings.Contains(body, tc.wantStatus) {
				t.Fatalf("%s JSON response should carry status %q, got %q", tc.name, tc.wantStatus, body)
			}
			// The token was still persisted (the reveal cookie is set for the
			// follow-up GET), proving the JSON branch is not a no-op.
			stored := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
			if stored.CalendarFeedSelector == "" {
				t.Fatalf("%s should still persist a feed token on the JSON path", tc.name)
			}
			if rc := responseCookie(resp.Cookies(), calendarFeedRevealCookieName); rc == nil || strings.TrimSpace(rc.Value) == "" {
				t.Fatalf("%s JSON path must still seal the one-time reveal cookie", tc.name)
			}
		})
	}
}

// TestCalendarFeedRotateInvalidatesOldToken proves rotation kills the previous
// subscribe URL: the OLD token no longer serves the feed after a rotate, while a
// fresh token is persisted.
func TestCalendarFeedRotateInvalidatesOldToken(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-rotate-invalidate@example.com")

	// Arm a feed directly and capture the OLD token, then confirm it serves.
	oldToken := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	oldSelector := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedSelector
	mustServeCalendarFeed(t, ctx.app, oldToken, "before the rotate")

	rot := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	if rot.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on rotate, got %d", rot.StatusCode)
	}

	// The selector changed (fresh token persisted)...
	after := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if after.CalendarFeedSelector == oldSelector || after.CalendarFeedSelector == "" {
		t.Fatalf("expected a fresh selector after rotate, old=%q new=%q", oldSelector, after.CalendarFeedSelector)
	}
	// ...and the OLD token now 404s (its selector no longer resolves).
	post := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(oldToken), nil))
	if post.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the old token to 404 after rotate, got %d", post.StatusCode)
	}
	assertNoSetCookie(t, post, "the rotated-out token's 404 must not set a cookie")
	// The old selector must not resolve any owner anymore.
	if _, ok, err := db.NewRepositories(ctx.database).Users.FindByCalendarFeedSelector(t.Context(), oldSelector); err != nil || ok {
		t.Fatalf("expected old selector to be unresolvable after rotate (ok=%v err=%v)", ok, err)
	}
}

// TestCalendarFeedRevokeClearsColumns proves revoke NULLs both columns so the
// feed URL 404s and the settings status flips to not-configured.
func TestCalendarFeedRevokeClearsColumns(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-revoke-clears@example.com")

	token := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	revoke := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, revoke, http.StatusOK)

	got := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" || got.CalendarFeedVerifierMAC != "" {
		t.Fatalf("expected feed columns cleared after revoke, got selector=%q hash=%q mac=%q",
			got.CalendarFeedSelector, got.CalendarFeedVerifierHash, got.CalendarFeedVerifierMAC)
	}
	feedResp := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	if feedResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the revoked feed URL to 404, got %d", feedResp.StatusCode)
	}
	assertNoSetCookie(t, feedResp, "the revoked feed's 404 must not set a cookie")
}

// TestCalendarFeedRevokeBrowserRedirectsWithFlash covers the non-JSON revoke
// path: a browser form DELETE (no Accept: application/json) redirects to
// /settings and sets a flash success cookie.
func TestCalendarFeedRevokeBrowserRedirectsWithFlash(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-revoke-redirect@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on browser revoke, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", loc)
	}
	if flash := responseCookie(resp.Cookies(), flashCookieName); flash == nil || strings.TrimSpace(flash.Value) == "" {
		t.Fatal("expected a flash success cookie on browser revoke")
	}
	got := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if got.CalendarFeedSelector != "" {
		t.Fatal("expected feed cleared after browser revoke")
	}
}

// TestCalendarFeedRevokeHTMXReturnsSuccessMarkup covers the HTMX revoke branch:
// an HX-Request DELETE returns 200 with success status markup, not a redirect.
func TestCalendarFeedRevokeHTMXReturnsSuccessMarkup(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-revoke-htmx@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, map[string]string{
		"HX-Request": "true",
	})
	assertStatusCode(t, resp, http.StatusOK)
	body := mustReadBodyString(t, resp.Body)
	if strings.TrimSpace(body) == "" {
		t.Fatal("expected HTMX revoke to return success markup")
	}
}

// TestCalendarFeedRevealCrossOwnerCookieIgnored proves the reveal is user-scoped:
// owner A's sealed reveal cookie presented on owner B's session does NOT reveal
// A's URL — B is redirected to /settings with no URL. This is the reveal-side
// arm of the privacy boundary.
func TestCalendarFeedRevealCrossOwnerCookieIgnored(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-reveal-owner-a@example.com")

	// Owner A generates and captures A's sealed reveal cookie.
	gen := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	revealCookieA := responseCookie(gen.Cookies(), calendarFeedRevealCookieName)
	if revealCookieA == nil {
		t.Fatal("expected owner A reveal cookie")
	}

	// Owner B presents A's cookie on B's own session.
	ownerB := createOnboardingTestUser(t, ctx.database, "feed-reveal-owner-b@example.com", "StrongPass1", true)
	authB := loginAndExtractAuthCookieWithCSRF(t, ctx.app, ownerB.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/settings/calendar-feed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authB, cookiePair(revealCookieA)))
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("owner B reveal request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected owner B to be redirected (no reveal), got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if strings.Contains(body, "/calendar/feed/") {
		t.Fatal("owner B must not see owner A's subscribe URL")
	}
}

// TestCalendarFeedRevealRefusesUnattributedCookie is the reveal page's answer to
// a sealed payload that names NO owner. The scoping comparison has two operands;
// a payload carrying `uid` 0 supplies neither an owner to match nor a reason to
// skip the match. Refusing it is what stops a well-sealed but unattributed
// payload from handing one owner's subscribe URL — a bearer capability token —
// to a different signed-in owner.
//
// The payload here is sealed under the app's own secret, so it opens cleanly:
// only the owner-scoping guard stands between it and the reveal. The positive
// anchor at the top proves the page still reveals to the owner it was minted
// for, so the refusal below cannot be satisfied by a page that reveals nothing.
func TestCalendarFeedRevealRefusesUnattributedCookie(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-reveal-unattributed-a@example.com")

	// Positive anchor: owner A generates and sees the URL on the reveal page.
	gen := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	revealResponse, revealBody := followCalendarFeedReveal(t, ctx, responseCookie(gen.Cookies(), calendarFeedRevealCookieName))
	defer func() { _ = revealResponse.Body.Close() }()
	if !strings.Contains(revealBody, "/calendar/feed/") {
		t.Fatal("owner A must see the subscribe URL on the reveal page it was minted for")
	}

	// A second independent owner arms a feed of their own; its token is the
	// secret an unattributed payload would carry across the owner boundary.
	ownerB := createOnboardingTestUser(t, ctx.database, "feed-reveal-unattributed-b@example.com", "StrongPass1", true)
	tokenB := armCalendarFeedForUser(t, ctx.database, ownerB.ID)

	unattributed := sealCookieForTestApp(t, calendarFeedRevealCookieName,
		[]byte(`{"uid":0,"feed_url":"https://ovumcy.example`+calendarFeedURL(tokenB)+`"}`))

	request := httptest.NewRequest(http.MethodGet, "/settings/calendar-feed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, calendarFeedRevealCookieName+"="+unattributed))
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unattributed reveal request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected an unattributed reveal payload to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
	body := mustReadBodyString(t, response.Body)
	if strings.Contains(body, tokenB) {
		t.Fatal("an unattributed reveal payload must not surface another owner's feed token")
	}
	cleared := responseCookie(response.Cookies(), calendarFeedRevealCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatal("expected the refused reveal cookie to be cleared, not left presentable on a retry")
	}
}

// TestCalendarFeedRevealWithoutCookieRedirects proves a direct visit to the
// reveal page with no sealed cookie redirects to /settings and shows nothing.
func TestCalendarFeedRevealWithoutCookieRedirects(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-reveal-nocookie@example.com")

	request := httptest.NewRequest(http.MethodGet, "/settings/calendar-feed", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reveal request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect with no reveal cookie, got %d", response.StatusCode)
	}
	if loc := response.Header.Get("Location"); loc != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", loc)
	}
}

// TestCalendarFeedGenerateScopedToOwner is the cross-owner IDOR guard: owner B's
// generate only ever writes B's own row; owner A's armed feed is untouched.
func TestCalendarFeedGenerateScopedToOwner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-owner-a@example.com")

	// Owner A arms a feed and keeps its token working.
	tokenA := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	ownerABefore := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)

	// A second independent owner B generates a feed on the same instance.
	ownerB := createOnboardingTestUser(t, ctx.database, "feed-owner-b@example.com", "StrongPass1", true)
	authB := loginAndExtractAuthCookieWithCSRF(t, ctx.app, ownerB.Email, "StrongPass1")
	csrfCookieB, csrfTokenB := loadSettingsCSRFContext(t, ctx.app, authB)

	formB := url.Values{"csrf_token": {csrfTokenB}}
	requestB := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/calendar-feed", strings.NewReader(formB.Encode()))
	requestB.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestB.Header.Set("Cookie", settingsCookieHeader(authB, csrfCookieB))
	responseB, err := ctx.app.Test(requestB, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("owner B feed generate failed: %v", err)
	}
	_ = responseB.Body.Close()
	if responseB.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 for owner B generate, got %d", responseB.StatusCode)
	}

	// Owner A's row is unchanged and A's token still serves.
	ownerAAfter := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if ownerAAfter.CalendarFeedSelector != ownerABefore.CalendarFeedSelector {
		t.Fatal("owner A's feed selector must not change when owner B generates")
	}
	mustServeCalendarFeed(t, ctx.app, tokenA, "after owner B generate")
	// Owner B got its own (distinct) selector.
	ownerBRow := reloadUserForCalendarFeedAPI(t, ctx, ownerB.ID)
	if ownerBRow.CalendarFeedSelector == "" || ownerBRow.CalendarFeedSelector == ownerABefore.CalendarFeedSelector {
		t.Fatalf("owner B should hold its own distinct selector, got %q", ownerBRow.CalendarFeedSelector)
	}
}

// TestCalendarFeedRevokeScopedToOwner is the revoke-side arm of the cross-owner
// IDOR guard, and the only place the api→services owner-id threading of revoke is
// observed end to end. Both single-owner revoke regressions above stay green when
// the handler's user.ID is replaced by a constant on the way to the service, and
// so does the repository suite, which is scoped and proven on its own — the
// untested link was the argument in between.
//
// Two owners are armed, owner B revokes, and the verdict is read from both rows
// and both feed URLs: A must keep serving (the containment must not spill) and B
// must 404 (it must actually land). A's still-serving feed is also the positive
// anchor for B's 404 — a revoke that killed every feed on the instance would
// satisfy the 404 alone.
func TestCalendarFeedRevokeScopedToOwner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-revoke-owner-a@example.com")

	// Owner A arms a feed and keeps it.
	tokenA := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	ownerABefore := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)

	// A second independent owner B arms one of their own — the feed the revoke
	// below is actually aimed at.
	ownerB := createOnboardingTestUser(t, ctx.database, "feed-revoke-owner-b@example.com", "StrongPass1", true)
	tokenB := armCalendarFeedForUser(t, ctx.database, ownerB.ID)

	// Both feeds serve before the revoke, so neither verdict below can be
	// satisfied by a URL that never worked. Asserted here and not in a subtest:
	// t.Fatalf inside t.Run ends only the subtest, so a lost precondition would
	// leave the verdicts below to run anyway and report a second, misleading
	// failure beside it.
	for _, owner := range []struct {
		label string
		token string
	}{{label: "A", token: tokenA}, {label: "B", token: tokenB}} {
		mustServeCalendarFeed(t, ctx.app, owner.token, "for owner "+owner.label+" before the revoke")
	}

	// Owner B revokes, on owner B's own session.
	authB := loginAndExtractAuthCookieWithCSRF(t, ctx.app, ownerB.Email, "StrongPass1")
	csrfCookieB, csrfTokenB := loadSettingsCSRFContext(t, ctx.app, authB)

	formB := url.Values{"csrf_token": {csrfTokenB}}
	requestB := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current/calendar-feed", strings.NewReader(formB.Encode()))
	requestB.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestB.Header.Set("Accept", "application/json")
	requestB.Header.Set("Cookie", settingsCookieHeader(authB, csrfCookieB))
	responseB, err := ctx.app.Test(requestB, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("owner B feed revoke failed: %v", err)
	}
	defer func() { _ = responseB.Body.Close() }()
	assertStatusCode(t, responseB, http.StatusOK)

	// Owner B's columns are the ones that were cleared.
	ownerBAfter := reloadUserForCalendarFeedAPI(t, ctx, ownerB.ID)
	if ownerBAfter.CalendarFeedSelector != "" || ownerBAfter.CalendarFeedVerifierHash != "" || ownerBAfter.CalendarFeedVerifierMAC != "" {
		t.Fatalf("expected owner B's feed columns cleared by owner B's revoke, got selector=%q hash=%q mac=%q",
			ownerBAfter.CalendarFeedSelector, ownerBAfter.CalendarFeedVerifierHash, ownerBAfter.CalendarFeedVerifierMAC)
	}
	feedB := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(tokenB), nil))
	if feedB.StatusCode != http.StatusNotFound {
		t.Fatalf("expected owner B's revoked feed URL to 404, got %d", feedB.StatusCode)
	}
	assertNoSetCookie(t, feedB, "owner B's revoked feed 404 must not set a cookie")

	// Owner A's row is untouched and A's URL still serves.
	ownerAAfter := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if ownerAAfter.CalendarFeedSelector != ownerABefore.CalendarFeedSelector ||
		ownerAAfter.CalendarFeedVerifierHash != ownerABefore.CalendarFeedVerifierHash ||
		ownerAAfter.CalendarFeedVerifierMAC != ownerABefore.CalendarFeedVerifierMAC {
		t.Fatal("owner A's feed columns must not change when owner B revokes")
	}
	mustServeCalendarFeed(t, ctx.app, tokenA, "after owner B's revoke")
}

// failingCalendarFeedRepo forces the feed settings repository to error on every
// write/clear, so the handler's service-error tails (and the shared 500 error
// spec) can be exercised without tearing down a real database mid-request.
type failingCalendarFeedRepo struct{}

func (failingCalendarFeedRepo) SaveCalendarFeedToken(context.Context, uint, models.CalendarFeedTokenColumns) error {
	return errors.New("save failed")
}
func (failingCalendarFeedRepo) ClearCalendarFeedToken(context.Context, uint) error {
	return errors.New("clear failed")
}
func (failingCalendarFeedRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}
func (failingCalendarFeedRepo) ClaimCalendarFeedReveal(context.Context, uint, time.Time) (bool, error) {
	return false, errors.New("claim failed")
}

// newFailingCalendarFeedHandlerApp builds a minimal app with an injected owner
// and a feed settings service whose repository always fails, plus a real sealed-
// cookie secret so the reveal-cookie writer works. It registers the three feed
// endpoints WITHOUT middleware to isolate the handler failure tails.
func newFailingCalendarFeedHandlerApp(t *testing.T) *fiber.App {
	t.Helper()
	const sealedCookieSecret = "0123456789abcdef0123456789abcdef"
	handler := &Handler{
		secretKey:            []byte(sealedCookieSecret),
		cookieSecure:         false,
		calendarFeedSettings: services.NewCalendarFeedSettingsService(failingCalendarFeedRepo{}, []byte(sealedCookieSecret)),
	}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner})
		return c.Next()
	})
	app.Post("/api/v1/users/current/calendar-feed", handler.GenerateCalendarFeed)
	app.Post("/api/v1/users/current/calendar-feed/rotate", handler.RotateCalendarFeed)
	app.Delete("/api/v1/users/current/calendar-feed", handler.RevokeCalendarFeed)
	return app
}

// savingCalendarFeedRepo persists successfully, so a handler paired with a
// BROKEN sealed-cookie key reaches the reveal-cookie seal step and fails there,
// exercising the post-mint cookie-set error tail.
type savingCalendarFeedRepo struct{}

func (savingCalendarFeedRepo) SaveCalendarFeedToken(context.Context, uint, models.CalendarFeedTokenColumns) error {
	return nil
}
func (savingCalendarFeedRepo) ClearCalendarFeedToken(context.Context, uint) error { return nil }
func (savingCalendarFeedRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}
func (savingCalendarFeedRepo) ClaimCalendarFeedReveal(context.Context, uint, time.Time) (bool, error) {
	return true, nil
}

// TestCalendarFeedGenerateRevealCookieSealFailureMapsTo500 covers the tail where
// the token mints successfully but sealing the one-time reveal cookie fails
// (here forced by an empty sealed-cookie secret key): the handler returns a 500
// and no URL leaks.
func TestCalendarFeedGenerateRevealCookieSealFailureMapsTo500(t *testing.T) {
	handler := &Handler{
		secretKey:    []byte(""), // empty key → cookie codec unavailable → seal fails
		cookieSecure: false,
		// The settings service keeps a WORKING key on purpose: minting must succeed so
		// the request reaches the cookie-seal step this test covers. With an empty key
		// here the mint itself would fail (no verifier MAC), and the 500 would come
		// from the wrong branch.
		calendarFeedSettings: services.NewCalendarFeedSettingsService(savingCalendarFeedRepo{}, []byte("0123456789abcdef0123456789abcdef")),
	}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner})
		return c.Next()
	})
	app.Post("/api/v1/users/current/calendar-feed", handler.GenerateCalendarFeed)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/calendar-feed", strings.NewReader(""))
	request.Header.Set("Accept", "application/json")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when reveal-cookie seal fails, got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if strings.Contains(body, "/calendar/feed/") {
		t.Fatalf("500 body must not leak the subscribe URL, got %q", body)
	}
}

// TestCalendarFeedHandlersMapServiceErrorsTo500 covers the generate/rotate/revoke
// failure tails: a repository error surfaces as the generic 500 feed spec, never
// a leak of the token or the underlying error.
func TestCalendarFeedHandlersMapServiceErrorsTo500(t *testing.T) {
	app := newFailingCalendarFeedHandlerApp(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"generate", http.MethodPost, "/api/v1/users/current/calendar-feed"},
		{"rotate", http.MethodPost, "/api/v1/users/current/calendar-feed/rotate"},
		{"revoke", http.MethodDelete, "/api/v1/users/current/calendar-feed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
			request.Header.Set("Accept", "application/json")
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("%s request failed: %v", tc.name, err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusInternalServerError {
				t.Fatalf("%s: expected 500 on service failure, got %d", tc.name, response.StatusCode)
			}
			body := mustReadBodyString(t, response.Body)
			if strings.Contains(body, "/calendar/feed/") || strings.Contains(body, "save failed") || strings.Contains(body, "clear failed") {
				t.Fatalf("%s 500 body must not leak a token or the raw error, got %q", tc.name, body)
			}
		})
	}
}

// extractFeedTokenFromURL pulls the <token> out of a …/calendar/feed/<token>.ics
// URL so a test can drive the feed endpoint with it.
func extractFeedTokenFromURL(t *testing.T, feedURL string) string {
	t.Helper()
	const marker = "/calendar/feed/"
	idx := strings.Index(feedURL, marker)
	if idx < 0 {
		t.Fatalf("URL %q missing feed path marker", feedURL)
	}
	rest := feedURL[idx+len(marker):]
	token := strings.TrimSuffix(rest, ".ics")
	if token == "" || token == rest {
		t.Fatalf("could not extract token from %q", feedURL)
	}
	return token
}

// TestHeadToAShownOnceSurfaceDoesNotSpendTheReveal is the behavioural half of
// the shown-once exception to HEAD parity (routes.go, shownOnceGETRoutes). The
// reveal page is served on HEAD like every other GET route, and its chain
// claims the owner's one-time mark BEFORE it renders — so a twin left to run
// that chain would record the disclosure and hand back a response the protocol
// strips the body from: the single display of a bearer secret spent on a
// request that could not carry it.
//
// The probe sends exactly what the owner's own visit sends — the session and
// the sealed reveal cookie the generate minted — and asserts the three halves
// of "nothing was spent": the refusal, the untouched server-side mark, and the
// owner's later GET still showing the URL.
func TestHeadToAShownOnceSurfaceDoesNotSpendTheReveal(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-head-does-not-spend@example.com")

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	assertStatusCode(t, generated, http.StatusSeeOther)
	sealedReveal := responseCookie(generated.Cookies(), calendarFeedRevealCookieName)
	if sealedReveal == nil {
		t.Fatal("expected a sealed reveal cookie on the generate response")
	}
	if armed := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID); armed.CalendarFeedRevealedAt != nil {
		t.Fatal("expected the generate to arm an unclaimed reveal mark; the assertion below could not tell a spent mark from one that was never armed")
	}

	headRequest := httptest.NewRequest(http.MethodHead, calendarFeedRevealPath, nil)
	headRequest.Header.Set("Accept-Language", "en")
	headRequest.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(sealedReveal)))
	headResponse := mustAppResponse(t, ctx.app, headRequest)
	if headResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("expected HEAD on the reveal page to be refused with the unknown-path 404, got %d", headResponse.StatusCode)
	}

	if spent := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID); spent.CalendarFeedRevealedAt != nil {
		t.Fatalf("HEAD claimed the owner's one-time reveal (calendar_feed_revealed_at = %v), so her own visit can never show the subscribe URL again", spent.CalendarFeedRevealedAt)
	}

	// Positive anchor, on the same app and the same cookie: the reveal the HEAD
	// did not spend is still there for the owner to spend.
	if revealed := assertCalendarFeedRevealShowsURL(t, ctx, sealedReveal); strings.TrimSpace(revealed) == "" {
		t.Fatal("expected the owner's own visit to reveal a subscribe token after the refused HEAD")
	}
}
