package api

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestOIDCLogoutBridgeCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-abc", now); err != nil {
			t.Fatalf("set oidc logout bridge cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		payload := handler.readOIDCLogoutBridgeCookie(c, now)
		if payload.SessionID != "session-id-abc" {
			t.Fatalf("expected session id to round-trip, got %q", payload.SessionID)
		}
		expectedExpiry := now.UTC().Add(time.Minute).Unix()
		if payload.ExpiresAtUnix != expectedExpiry {
			t.Fatalf("expected expiry %d, got %d", expectedExpiry, payload.ExpiresAtUnix)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLogoutBridgeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc logout bridge cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", oidcLogoutBridgeCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestOIDCLogoutBridgeCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-tamper", now); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		payload := handler.readOIDCLogoutBridgeCookie(c, now)
		if payload.SessionID != "" || payload.ExpiresAtUnix != 0 {
			t.Fatalf("expected tampered logout bridge cookie to yield empty payload, got %+v", payload)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLogoutBridgeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc logout bridge cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", oidcLogoutBridgeCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestOIDCLogoutBridgeCookieRejectsForeignKey(t *testing.T) {
	t.Parallel()

	sealingHandler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	openingHandler := &Handler{
		secretKey:    []byte("ffffffffffffffffffffffffffffffff"),
		cookieSecure: true,
	}
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	sealingApp := fiber.New()
	sealingApp.Get("/seal", func(c fiber.Ctx) error {
		if err := sealingHandler.setOIDCLogoutBridgeCookie(c, "session-id-foreign", now); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get("/open", func(c fiber.Ctx) error {
		payload := openingHandler.readOIDCLogoutBridgeCookie(c, now)
		if payload.SessionID != "" || payload.ExpiresAtUnix != 0 {
			t.Fatalf("expected rotated-key handler to reject sealed cookie, got %+v", payload)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLogoutBridgeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc logout bridge cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", oidcLogoutBridgeCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

// flipLastBaseEncodedByte XORs the last byte of the base64url-decoded portion
// of a sealed cookie value (which lands in the GCM auth tag) and re-encodes.
// Shared by all per-cookie tamper regressions.
func flipLastBaseEncodedByte(t *testing.T, sealed string) string {
	t.Helper()

	version, encoded, found := strings.Cut(sealed, ".")
	if !found {
		t.Fatalf("expected versioned sealed cookie, got %q", sealed)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode sealed cookie %q: %v", sealed, err)
	}
	if len(decoded) == 0 {
		t.Fatalf("decoded sealed cookie is empty")
	}
	decoded[len(decoded)-1] ^= 0xFF
	return version + "." + base64.RawURLEncoding.EncodeToString(decoded)
}
