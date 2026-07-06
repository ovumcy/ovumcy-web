package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
	defer func() { _ = gen.Body.Close() }()
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

	// A HASHED token is persisted (selector + bcrypt verifier hash), and the
	// stored columns are not the plaintext token.
	stored := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if stored.CalendarFeedSelector == "" || stored.CalendarFeedVerifierHash == "" {
		t.Fatal("expected a feed token persisted after generate")
	}
	if !strings.HasPrefix(stored.CalendarFeedVerifierHash, "$2") {
		t.Fatalf("expected a bcrypt verifier hash at rest, got %q", stored.CalendarFeedVerifierHash)
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
	// Extract the token path and prove it actually serves the feed.
	token := extractFeedTokenFromURL(t, revealedURL)
	feedResp := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	defer func() { _ = feedResp.Body.Close() }()
	if feedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the revealed URL to serve the feed (200), got %d", feedResp.StatusCode)
	}

	// The reveal is ONE-TIME: the reveal cookie was cleared, so a second visit
	// carrying the (now-expired) cookie redirects to /settings with no URL.
	clearedCookie := responseCookie(revealResp.Cookies(), calendarFeedRevealCookieName)
	if clearedCookie == nil || strings.TrimSpace(clearedCookie.Value) != "" {
		t.Fatal("expected the reveal page to clear the one-time cookie")
	}
	secondResp, secondBody := followCalendarFeedReveal(t, ctx, clearedCookie)
	_ = secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a second reveal visit to redirect, got %d", secondResp.StatusCode)
	}
	if strings.Contains(secondBody, token) {
		t.Fatal("a second reveal visit must not show the token again")
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
	if htmlElementByAttr(settingsDoc, "data-calendar-feed-status", "configured") == nil {
		t.Fatal("expected the settings feed status to report 'configured'")
	}
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
			defer func() { _ = resp.Body.Close() }()
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
	pre := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(oldToken), nil))
	_ = pre.Body.Close()
	if pre.StatusCode != http.StatusOK {
		t.Fatalf("precondition: old token should serve the feed, got %d", pre.StatusCode)
	}

	rot := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	defer func() { _ = rot.Body.Close() }()
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
	defer func() { _ = post.Body.Close() }()
	if post.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the old token to 404 after rotate, got %d", post.StatusCode)
	}
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
	defer func() { _ = revoke.Body.Close() }()
	assertStatusCode(t, revoke, http.StatusOK)

	got := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" {
		t.Fatalf("expected feed columns cleared after revoke, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	feedResp := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(token), nil))
	defer func() { _ = feedResp.Body.Close() }()
	if feedResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected the revoked feed URL to 404, got %d", feedResp.StatusCode)
	}
}

// TestCalendarFeedRevokeBrowserRedirectsWithFlash covers the non-JSON revoke
// path: a browser form DELETE (no Accept: application/json) redirects to
// /settings and sets a flash success cookie.
func TestCalendarFeedRevokeBrowserRedirectsWithFlash(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-revoke-redirect@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
	_ = gen.Body.Close()
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
	feedA := mustAppResponse(t, ctx.app, httptest.NewRequest(http.MethodGet, calendarFeedURL(tokenA), nil))
	defer func() { _ = feedA.Body.Close() }()
	if feedA.StatusCode != http.StatusOK {
		t.Fatalf("expected owner A's feed to keep serving after owner B generate, got %d", feedA.StatusCode)
	}
	// Owner B got its own (distinct) selector.
	ownerBRow := reloadUserForCalendarFeedAPI(t, ctx, ownerB.ID)
	if ownerBRow.CalendarFeedSelector == "" || ownerBRow.CalendarFeedSelector == ownerABefore.CalendarFeedSelector {
		t.Fatalf("owner B should hold its own distinct selector, got %q", ownerBRow.CalendarFeedSelector)
	}
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
func (failingCalendarFeedRepo) FindByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

// newFailingCalendarFeedHandlerApp builds a minimal app with an injected owner
// and a feed settings service whose repository always fails, plus a real sealed-
// cookie secret so the reveal-cookie writer works. It registers the three feed
// endpoints WITHOUT middleware to isolate the handler failure tails.
func newFailingCalendarFeedHandlerApp(t *testing.T) *fiber.App {
	t.Helper()
	handler := &Handler{
		secretKey:            []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure:         false,
		calendarFeedSettings: services.NewCalendarFeedSettingsService(failingCalendarFeedRepo{}),
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
func (savingCalendarFeedRepo) FindByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

// TestCalendarFeedGenerateRevealCookieSealFailureMapsTo500 covers the tail where
// the token mints successfully but sealing the one-time reveal cookie fails
// (here forced by an empty sealed-cookie secret key): the handler returns a 500
// and no URL leaks.
func TestCalendarFeedGenerateRevealCookieSealFailureMapsTo500(t *testing.T) {
	handler := &Handler{
		secretKey:            []byte(""), // empty key → cookie codec unavailable → seal fails
		cookieSecure:         false,
		calendarFeedSettings: services.NewCalendarFeedSettingsService(savingCalendarFeedRepo{}),
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
