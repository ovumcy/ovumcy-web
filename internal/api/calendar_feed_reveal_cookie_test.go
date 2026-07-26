package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// newCalendarFeedRevealCookieTestHandler builds a bare Handler with a fixed
// secret key, mirroring the recovery-code cookie encoding tests, so the sealed
// one-time reveal cookie's seal/open/validation branches can be driven directly.
func newCalendarFeedRevealCookieTestHandler() *Handler {
	return &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
}

// TestCalendarFeedRevealCookieRoundTripPreservesURL proves a sealed reveal
// cookie round-trips the full subscribe URL and the rotated flag for the owner
// it was scoped to.
func TestCalendarFeedRevealCookieRoundTripPreservesURL(t *testing.T) {
	t.Parallel()
	handler := newCalendarFeedRevealCookieTestHandler()
	const feedURL = "https://ovumcy.example/calendar/feed/ABCDEFGHJKLMNPQR1234567890ABCDEFGH.ics"

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setCalendarFeedRevealCookie(c, 77, feedURL, true); err != nil {
			t.Fatalf("seal reveal cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, 77)
		if state.FeedURL != feedURL {
			t.Fatalf("expected URL to round-trip, got %q", state.FeedURL)
		}
		if !state.Rotated {
			t.Fatal("expected rotated flag to round-trip")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	cookieValue := sealAndExtractCalendarFeedRevealCookie(t, app)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", calendarFeedRevealCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
	// The round-trip assertions live in a t.Fatalf inside /open; assert the
	// handler ran to completion so a future early-return can't pass vacuously.
	if openResponse.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected /open to reach 204, got %d; the in-handler round-trip assertions may have been skipped", openResponse.StatusCode)
	}
}

// TestCalendarFeedRevealCookieRejectsTamperedByte proves a flipped ciphertext
// byte fails to open and yields an empty state (open-failure branch).
func TestCalendarFeedRevealCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()
	handler := newCalendarFeedRevealCookieTestHandler()

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setCalendarFeedRevealCookie(c, 77, "https://ovumcy.example/calendar/feed/TAMPER1234567890ABCDEFGHJKLMNPQR.ics", false); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, 77)
		if state.FeedURL != "" {
			t.Fatalf("expected tampered cookie to yield empty URL, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	cookieValue := sealAndExtractCalendarFeedRevealCookie(t, app)
	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", calendarFeedRevealCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
	// The empty-state assertion lives in a t.Fatalf inside /open; prove it ran.
	if openResponse.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected /open to reach 204, got %d; the in-handler tampered-cookie assertion may have been skipped", openResponse.StatusCode)
	}
}

// TestCalendarFeedRevealCookieEmptyURLGuardsAndRejects covers two defensive
// paths: sealing a blank URL is refused (the caller must pass a real token), and
// a sealed payload that carries an empty URL opens to an empty state.
func TestCalendarFeedRevealCookieEmptyURLGuardsAndRejects(t *testing.T) {
	t.Parallel()
	handler := newCalendarFeedRevealCookieTestHandler()

	app := fiber.New()
	// Refuses to seal a blank URL.
	app.Get("/seal-blank", func(c fiber.Ctx) error {
		if err := handler.setCalendarFeedRevealCookie(c, 77, "   ", false); err == nil {
			t.Fatal("expected setCalendarFeedRevealCookie to reject a blank URL")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// Seals a payload with an EMPTY feed_url via the raw sealed-cookie writer, so
	// the read path exercises the empty-URL-in-payload branch.
	app.Get("/seal-empty-payload", func(c fiber.Ctx) error {
		serialized := []byte(`{"uid":77,"feed_url":""}`)
		if err := handler.writeSealedCookie(c, calendarFeedRevealCookieSpec, serialized, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("write sealed empty-payload cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, 77)
		if state.FeedURL != "" {
			t.Fatalf("expected empty-URL payload to yield empty state, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	// Blank-seal path.
	blankResp, err := app.Test(httptest.NewRequest("GET", "/seal-blank", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal-blank request: %v", err)
	}
	_ = blankResp.Body.Close()
	// The blank-URL rejection lives in a t.Fatal inside /seal-blank; assert the
	// handler ran to completion so the rejection can't be skipped vacuously.
	if blankResp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected /seal-blank to reach 204, got %d; the in-handler blank-URL rejection may have been skipped", blankResp.StatusCode)
	}

	// Empty-payload seal + open.
	sealResp, err := app.Test(httptest.NewRequest("GET", "/seal-empty-payload", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal-empty-payload request: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := responseCookieValue(sealResp.Cookies(), calendarFeedRevealCookieName)
	if cookieValue == "" {
		t.Fatal("expected a sealed empty-payload cookie")
	}
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", calendarFeedRevealCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open empty-payload request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
	// The empty-state assertion lives in a t.Fatalf inside /open; prove it ran.
	if openResponse.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected /open to reach 204, got %d; the in-handler empty-payload assertion may have been skipped", openResponse.StatusCode)
	}
}

// TestCalendarFeedRevealCookieRejectsSealedNonJSON covers the unmarshal-failure
// branch: a validly-sealed but non-JSON payload opens (AEAD passes) yet fails to
// unmarshal, yielding an empty state.
func TestCalendarFeedRevealCookieRejectsSealedNonJSON(t *testing.T) {
	t.Parallel()
	handler := newCalendarFeedRevealCookieTestHandler()

	app := fiber.New()
	app.Get("/seal-nonjson", func(c fiber.Ctx) error {
		if err := handler.writeSealedCookie(c, calendarFeedRevealCookieSpec, []byte("not-json-at-all"), time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("write sealed non-json cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, 77)
		if state.FeedURL != "" {
			t.Fatalf("expected non-JSON payload to yield empty state, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResp, err := app.Test(httptest.NewRequest("GET", "/seal-nonjson", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal-nonjson request: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := responseCookieValue(sealResp.Cookies(), calendarFeedRevealCookieName)
	if cookieValue == "" {
		t.Fatal("expected a sealed non-json cookie")
	}
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", calendarFeedRevealCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open non-json request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
	// The empty-state assertion lives in a t.Fatalf inside /open; prove it ran.
	if openResponse.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected /open to reach 204, got %d; the in-handler non-json assertion may have been skipped", openResponse.StatusCode)
	}
}

// TestCalendarFeedRevealCookieRefusesUnattributedOwner pins both halves of the
// owner scoping on the reveal cookie, at the seal boundary and at the read
// boundary.
//
// A payload whose `uid` is zero names no account. Treating that as "no owner to
// compare against, so let it through" would reveal the subscribe URL — a bearer
// capability token — to whichever session presented the cookie. So:
//   - the writer refuses to seal a zero owner id at all, and
//   - the reader refuses a zero owner id in the payload, and refuses any payload
//     when the session itself has no id, clearing the cookie either way.
//
// The positive anchor in the same test proves the reveal still works for the
// owner it was minted for; without it every assertion here would also pass
// against a reader that returns nothing to anybody.
func TestCalendarFeedRevealCookieRefusesUnattributedOwner(t *testing.T) {
	t.Parallel()
	handler := newCalendarFeedRevealCookieTestHandler()

	const ownerA = uint(77)
	const ownedURL = "https://ovumcy.example/calendar/feed/OWNEDBYAAAAAAAAAAAAAAAAAAAAAAAAA.ics"
	const unattributedURL = "https://ovumcy.example/calendar/feed/UNATTRIBUTED000000000000000000000.ics"

	app := fiber.New()
	// Seal-time guard: an owner id is mandatory, and the refusal writes no cookie.
	app.Get("/seal-unattributed", func(c fiber.Ctx) error {
		if err := handler.setCalendarFeedRevealCookie(c, 0, unattributedURL, false); err == nil {
			t.Fatal("expected setCalendarFeedRevealCookie to refuse a zero owner id")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// Positive anchor: a payload sealed for owner A reveals to owner A.
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setCalendarFeedRevealCookie(c, ownerA, ownedURL, false); err != nil {
			t.Fatalf("seal reveal cookie for owner A: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, ownerA)
		if state.FeedURL != ownedURL {
			t.Fatalf("owner A must still see the URL sealed for owner A, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// A session with no id of its own must never satisfy the comparison, even
	// against a properly attributed payload.
	app.Get("/open-sessionless", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, 0)
		if state.FeedURL != "" {
			t.Fatalf("a session with no owner id must reveal nothing, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// Read-time guard, driven from a hand-sealed payload the writer would now
	// refuse to mint: uid 0 must be rejected, not skipped.
	app.Get("/seal-unattributed-payload", func(c fiber.Ctx) error {
		serialized := []byte(`{"uid":0,"feed_url":"` + unattributedURL + `"}`)
		if err := handler.writeSealedCookie(c, calendarFeedRevealCookieSpec, serialized, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("write sealed unattributed payload: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open-unattributed", func(c fiber.Ctx) error {
		state := handler.readCalendarFeedRevealState(c, ownerA)
		if state.FeedURL != "" {
			t.Fatalf("an unattributed payload must reveal nothing, got %q", state.FeedURL)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	// The writer refuses a zero owner id and leaves no cookie behind.
	unattributedSeal := mustReachNoContent(t, app, "/seal-unattributed", "", "the in-handler zero-owner-id rejection")
	defer func() { _ = unattributedSeal.Body.Close() }()
	if written := responseCookieValue(unattributedSeal.Cookies(), calendarFeedRevealCookieName); written != "" {
		t.Fatalf("a refused seal must not leave a usable reveal cookie, got %q", written)
	}

	// Positive anchor: owner A's own reveal still works.
	ownedCookie := sealAndExtractCalendarFeedRevealCookie(t, app)
	ownedResponse := mustReachNoContent(t, app, "/open", calendarFeedRevealCookieName+"="+ownedCookie, "the in-handler owner-A reveal assertion")
	defer func() { _ = ownedResponse.Body.Close() }()

	// The same well-formed payload reveals nothing to a session with no id.
	sessionlessResponse := mustReachNoContent(t, app, "/open-sessionless", calendarFeedRevealCookieName+"="+ownedCookie, "the in-handler sessionless assertion")
	defer func() { _ = sessionlessResponse.Body.Close() }()

	// An unattributed payload reveals nothing and is cleared on the way out.
	craftedSeal := mustReachNoContent(t, app, "/seal-unattributed-payload", "", "the unattributed-payload seal")
	defer func() { _ = craftedSeal.Body.Close() }()
	craftedCookie := responseCookieValue(craftedSeal.Cookies(), calendarFeedRevealCookieName)
	if craftedCookie == "" {
		t.Fatal("expected a sealed unattributed payload to drive the read path")
	}
	craftedResponse := mustReachNoContent(t, app, "/open-unattributed", calendarFeedRevealCookieName+"="+craftedCookie, "the in-handler unattributed-payload assertion")
	defer func() { _ = craftedResponse.Body.Close() }()
	assertRevealCookieCleared(t, craftedResponse, calendarFeedRevealCookieName)
}

// mustReachNoContent drives one GET (optionally carrying a Cookie header) and
// fails unless it reached 204. The reveal-cookie assertions live in t.Fatalf
// calls inside the routes, so a route that returned early would otherwise let
// the test pass having checked nothing.
func mustReachNoContent(t *testing.T, app *fiber.App, path string, cookieHeader string, what string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s request: %v", path, err)
	}
	if response.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected %s to reach 204, got %d; %s may have been skipped", path, response.StatusCode, what)
	}
	return response
}

// assertRevealCookieCleared pins the reject path's second obligation: a refused
// reveal payload is not merely ignored, it is expired off the client so a
// retry cannot present it again.
func assertRevealCookieCleared(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()
	cleared := responseCookie(response.Cookies(), cookieName)
	if cleared == nil {
		t.Fatalf("expected the refused %s cookie to be cleared", cookieName)
	}
	if cleared.Value != "" {
		t.Fatalf("expected an empty cleared %s cookie value, got %q", cookieName, cleared.Value)
	}
}

func sealAndExtractCalendarFeedRevealCookie(t *testing.T, app *fiber.App) string {
	t.Helper()
	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()
	cookieValue := responseCookieValue(sealResponse.Cookies(), calendarFeedRevealCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed reveal cookie in response")
	}
	return cookieValue
}
