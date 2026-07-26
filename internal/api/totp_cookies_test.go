package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// --- helpers for cookie tests ---

// newTOTPCookieTestApp returns a *fiber.App and a *Handler wired to use the
// given secret key. Two routes are registered: /seal-pending and /parse-pending
// (and the same pair for setup) — they thinly wrap the cookie helpers under
// test so the tests can drive the seal/parse flow through real HTTP.
func newTOTPCookieTestApp(t *testing.T, secretKey []byte) (*fiber.App, *Handler) {
	t.Helper()
	handler := &Handler{secretKey: secretKey, cookieSecure: false}
	app := fiber.New()
	app.Get("/seal-pending", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		remember := fiber.Query[bool](c, "remember_me", false)
		if err := handler.setTOTPPendingCookie(c, userID, remember); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-pending", func(c fiber.Ctx) error {
		uid, remember, err := handler.parseTOTPPendingCookie(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"user_id": uid, "remember_me": remember})
	})
	// The setup routes take the acting account as a query parameter so a test
	// can seal for one owner and parse as another, the way two independent
	// owners on one household instance would.
	app.Get("/seal-setup", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		raw := c.Query("raw_secret", "")
		if err := handler.setTOTPSetupCookie(c, userID, raw); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-setup", func(c fiber.Ctx) error {
		sessionUserID := uint(fiber.Query[int](c, "user_id", 0))
		raw, err := handler.parseTOTPSetupCookie(c, sessionUserID)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"raw_secret": raw})
	})
	return app, handler
}

func captureCookieValue(t *testing.T, resp *http.Response, name string) string {
	t.Helper()
	c := responseCookie(resp.Cookies(), name)
	if c == nil || c.Value == "" {
		t.Fatalf("expected Set-Cookie %q with non-empty value", name)
	}
	return c.Value
}

func sealExpiredPayload(t *testing.T, secretKey []byte, purpose string, payload any) string {
	t.Helper()
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(purpose, serialized)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return sealed
}

// --- TOTP pending cookie ---

func TestTOTPPendingCookie_RoundTrip(t *testing.T) {
	app, _ := newTOTPCookieTestApp(t, []byte("test-secret-key"))

	sealReq := httptest.NewRequest(http.MethodGet, "/seal-pending?user_id=42&remember_me=true", nil)
	sealResp, err := app.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(parseResp.Body)
		t.Fatalf("parse status = %d, body = %q", parseResp.StatusCode, body)
	}

	var got struct {
		UserID     uint `json:"user_id"`
		RememberMe bool `json:"remember_me"`
	}
	if err := json.NewDecoder(parseResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	if got.UserID != 42 {
		t.Errorf("user_id = %d, want 42", got.UserID)
	}
	if !got.RememberMe {
		t.Error("remember_me = false, want true")
	}
}

func TestTOTPPendingCookie_ExpiredPayload_ParseError(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	sealed := sealExpiredPayload(t, secretKey, totpPendingCookieName, totpPendingCookiePayload{
		UserID:    1,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	req.Header.Set("Cookie", totpPendingCookieName+"="+sealed)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expired") {
		t.Errorf("error %q, want to contain %q", string(body), "expired")
	}
}

func TestTOTPPendingCookie_WrongSigningKey_ParseError(t *testing.T) {
	sealedSecret := []byte("seal-key-original")
	openSecret := []byte("open-key-different")

	sealApp, _ := newTOTPCookieTestApp(t, sealedSecret)
	sealReq := httptest.NewRequest(http.MethodGet, "/seal-pending?user_id=7", nil)
	sealResp, err := sealApp.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", parseResp.StatusCode)
	}
	body, _ := io.ReadAll(parseResp.Body)
	if !strings.Contains(string(body), "invalid") {
		t.Errorf("error %q, want to contain %q", string(body), "invalid")
	}
}

// --- TOTP setup cookie ---

func TestTOTPSetupCookie_RoundTrip(t *testing.T) {
	app, _ := newTOTPCookieTestApp(t, []byte("test-secret-key"))

	const rawSecret = "JBSWY3DPEHPK3PXP"

	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+rawSecret, nil)
	sealResp, err := app.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(parseResp.Body)
		t.Fatalf("parse status = %d, body = %q", parseResp.StatusCode, body)
	}

	var got struct {
		RawSecret string `json:"raw_secret"`
	}
	if err := json.NewDecoder(parseResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	if got.RawSecret != rawSecret {
		t.Errorf("raw_secret = %q, want %q", got.RawSecret, rawSecret)
	}
}

func TestTOTPSetupCookie_ExpiredPayload_ParseError(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	// Attributed to the parsing account on purpose: the owner check runs first,
	// so an unattributed payload would be refused before expiry is ever reached
	// and this test would stop covering the TTL.
	sealed := sealExpiredPayload(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
		UserID:    42,
		RawSecret: "JBSWY3DPEHPK3PXP",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	req.Header.Set("Cookie", totpSetupCookieName+"="+sealed)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expired") {
		t.Errorf("error %q, want to contain %q", string(body), "expired")
	}
}

// TestTOTPSetupCookieRefusesUnattributedOwner pins both halves of the owner
// scoping on the enrollment setup cookie, at the seal boundary and at the read
// boundary, so neither depends on the other.
//
// The payload carries the raw TOTP secret. A payload whose `user_id` is zero
// names no account, so there is nothing to scope that secret to; treating it as
// "no owner to compare against, so let it through" would enrol it against
// whichever session presented the cookie. So:
//   - the writer refuses to seal a zero owner id at all, and clears any prior
//     cookie rather than leaving a stale one presentable, and
//   - the reader refuses a zero owner id in the payload, refuses a payload
//     minted for a different account, and refuses any payload when the session
//     itself has no id.
//
// The positive anchor proves the cookie still round-trips for the account it was
// minted for; without it every assertion here would also pass against a reader
// that returns nothing to anybody.
func TestTOTPSetupCookieRefusesUnattributedOwner(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	const ownerA = uint(42)
	const ownerB = uint(43)
	const rawSecret = "JBSWY3DPEHPK3PXP"

	// Seal-time guard: an owner id is mandatory, and the refusal leaves no
	// usable cookie behind.
	refusedSeal, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=0&raw_secret="+rawSecret, nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup with a zero owner id: %v", err)
	}
	defer func() { _ = refusedSeal.Body.Close() }()
	if refusedSeal.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected setTOTPSetupCookie to refuse a zero owner id, got status %d", refusedSeal.StatusCode)
	}
	if written := responseCookieValue(refusedSeal.Cookies(), totpSetupCookieName); written != "" {
		t.Fatalf("a refused seal must not leave a usable setup cookie, got %q", written)
	}

	// Positive anchor: owner A's own payload still parses back for owner A.
	ownedSeal, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+rawSecret, nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup for owner A: %v", err)
	}
	defer func() { _ = ownedSeal.Body.Close() }()
	ownedCookie := totpSetupCookieName + "=" + captureCookieValue(t, ownedSeal, totpSetupCookieName)

	ownedParse := parseTOTPSetupAs(t, app, ownerA, ownedCookie)
	defer func() { _ = ownedParse.Body.Close() }()
	if ownedParse.StatusCode != http.StatusOK {
		t.Fatalf("owner A must still recover the secret sealed for owner A, got status %d (%s)", ownedParse.StatusCode, mustReadBodyString(t, ownedParse.Body))
	}

	// Read-time guards. A hand-sealed payload with no owner is the shape the
	// writer now refuses to mint; a payload minted for owner A is the shape a
	// stale cookie on a household instance would have.
	unattributed := sealTOTPSetupCookieForTest(t, secretKey, 0, rawSecret)
	foreign := sealTOTPSetupCookieForTest(t, secretKey, ownerA, rawSecret)

	rejections := []struct {
		name    string
		session uint
		cookie  string
	}{
		{name: "unattributed_payload", session: ownerA, cookie: unattributed},
		{name: "payload_minted_for_another_owner", session: ownerB, cookie: foreign},
		{name: "session_without_an_owner_id", session: 0, cookie: ownedCookie},
	}
	for _, rejection := range rejections {
		t.Run(rejection.name, func(t *testing.T) {
			response := parseTOTPSetupAs(t, app, rejection.session, rejection.cookie)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected %s to be refused, got status %d", rejection.name, response.StatusCode)
			}
			if body := mustReadBodyString(t, response.Body); strings.Contains(body, rawSecret) {
				t.Fatalf("a refused setup cookie must not surface the pending secret, got %q", body)
			}
		})
	}
}

func parseTOTPSetupAs(t *testing.T, app *fiber.App, sessionUserID uint, cookie string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id="+strconv.FormatUint(uint64(sessionUserID), 10), nil)
	request.Header.Set("Cookie", cookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	return response
}

func TestTOTPSetupCookie_WrongSigningKey_ParseError(t *testing.T) {
	sealedSecret := []byte("seal-key-original")
	openSecret := []byte("open-key-different")

	sealApp, _ := newTOTPCookieTestApp(t, sealedSecret)
	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret=JBSWY3DPEHPK3PXP", nil)
	sealResp, err := sealApp.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", parseResp.StatusCode)
	}
	body, _ := io.ReadAll(parseResp.Body)
	if !strings.Contains(string(body), "invalid") {
		t.Errorf("error %q, want to contain %q", string(body), "invalid")
	}
}
