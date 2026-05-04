package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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
	app.Get("/seal-pending", func(c *fiber.Ctx) error {
		userID := uint(c.QueryInt("user_id", 0))
		remember := c.QueryBool("remember_me", false)
		if err := handler.setTOTPPendingCookie(c, userID, remember); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-pending", func(c *fiber.Ctx) error {
		uid, remember, err := handler.parseTOTPPendingCookie(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"user_id": uid, "remember_me": remember})
	})
	app.Get("/seal-setup", func(c *fiber.Ctx) error {
		raw := c.Query("raw_secret", "")
		if err := handler.setTOTPSetupCookie(c, raw); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-setup", func(c *fiber.Ctx) error {
		raw, err := handler.parseTOTPSetupCookie(c)
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
	sealResp, err := app.Test(sealReq, -1)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer sealResp.Body.Close()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, -1)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer parseResp.Body.Close()
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
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer resp.Body.Close()
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
	sealResp, err := sealApp.Test(sealReq, -1)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer sealResp.Body.Close()
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, -1)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer parseResp.Body.Close()
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

	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?raw_secret="+rawSecret, nil)
	sealResp, err := app.Test(sealReq, -1)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer sealResp.Body.Close()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, -1)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer parseResp.Body.Close()
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

	sealed := sealExpiredPayload(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
		RawSecret: "JBSWY3DPEHPK3PXP",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/parse-setup", nil)
	req.Header.Set("Cookie", totpSetupCookieName+"="+sealed)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expired") {
		t.Errorf("error %q, want to contain %q", string(body), "expired")
	}
}

func TestTOTPSetupCookie_WrongSigningKey_ParseError(t *testing.T) {
	sealedSecret := []byte("seal-key-original")
	openSecret := []byte("open-key-different")

	sealApp, _ := newTOTPCookieTestApp(t, sealedSecret)
	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?raw_secret=JBSWY3DPEHPK3PXP", nil)
	sealResp, err := sealApp.Test(sealReq, -1)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer sealResp.Body.Close()
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, -1)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer parseResp.Body.Close()
	if parseResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", parseResp.StatusCode)
	}
	body, _ := io.ReadAll(parseResp.Body)
	if !strings.Contains(string(body), "invalid") {
		t.Errorf("error %q, want to contain %q", string(body), "invalid")
	}
}
