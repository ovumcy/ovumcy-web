package api

import (
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
