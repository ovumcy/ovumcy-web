package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// The OIDC link-pending cookie hands a user from a partial OIDC callback to
// the password-confirmation page, carrying enough state to finish the link
// without re-running the OIDC exchange. testing.md AEAD-sealed cookie rules
// require every sealed-cookie purpose to lock the four AAD-binding invariants:
// round-trip, cross-purpose AAD, byte tamper, key rotation. Expiry and
// minimum-payload checks are added because validAt() also gates handler
// behavior.

func newOIDCLinkPendingTestPayload(t *testing.T, now time.Time) oidcLinkPendingPayload {
	t.Helper()
	payload, err := newOIDCLinkPendingPayload(now, 7, "https://idp.example", "subject-123", "owner@example.com")
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	return payload
}

func TestOIDCLinkPendingCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	payload := newOIDCLinkPendingTestPayload(t, time.Now().UTC())

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCLinkPendingCookie(c, payload); err != nil {
			t.Fatalf("set oidc link pending cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get(oidcLinkConfirmPath, func(c fiber.Ctx) error {
		recovered, ok := handler.readOIDCLinkPendingCookie(c)
		if !ok {
			t.Fatal("expected sealed link-pending cookie to round-trip, got !ok")
		}
		if recovered.TargetUserID != payload.TargetUserID {
			t.Fatalf("expected target_user_id %d, got %d", payload.TargetUserID, recovered.TargetUserID)
		}
		if recovered.Issuer != payload.Issuer || recovered.Subject != payload.Subject || recovered.Email != payload.Email {
			t.Fatalf("expected (issuer,subject,email) to round-trip, got %+v", recovered)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLinkPendingCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed link-pending cookie in response")
	}

	openRequest := httptest.NewRequest("GET", oidcLinkConfirmPath, nil)
	openRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer openResponse.Body.Close()
}

func TestOIDCLinkPendingCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	payload := newOIDCLinkPendingTestPayload(t, time.Now().UTC())

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCLinkPendingCookie(c, payload); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get(oidcLinkConfirmPath, func(c fiber.Ctx) error {
		if _, ok := handler.readOIDCLinkPendingCookie(c); ok {
			t.Fatal("expected tampered link-pending cookie to be rejected")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLinkPendingCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed link-pending cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", oidcLinkConfirmPath, nil)
	openRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer openResponse.Body.Close()
}

func TestOIDCLinkPendingCookieRejectsForeignKey(t *testing.T) {
	t.Parallel()

	sealingHandler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	openingHandler := &Handler{
		secretKey:    []byte("ffffffffffffffffffffffffffffffff"),
		cookieSecure: true,
	}
	payload := newOIDCLinkPendingTestPayload(t, time.Now().UTC())

	sealingApp := fiber.New()
	sealingApp.Get("/seal", func(c fiber.Ctx) error {
		if err := sealingHandler.setOIDCLinkPendingCookie(c, payload); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get(oidcLinkConfirmPath, func(c fiber.Ctx) error {
		if _, ok := openingHandler.readOIDCLinkPendingCookie(c); ok {
			t.Fatal("expected rotated-key handler to reject sealed link-pending cookie")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcLinkPendingCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed link-pending cookie in response")
	}

	openRequest := httptest.NewRequest("GET", oidcLinkConfirmPath, nil)
	openRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer openResponse.Body.Close()
}

// TestOIDCLinkPendingCookieRejectsCrossPurposeAAD seals the link-pending
// payload under the codec's AAD purpose for one cookie name and tries to open
// it under the link-pending purpose. The shared-key codec must refuse,
// otherwise an attacker who captured an unrelated sealed cookie (oidc-state,
// oidc-logout-bridge, recovery, etc.) could replay it into the link-confirm
// reader and bypass the password challenge with stolen claims.
func TestOIDCLinkPendingCookieRejectsCrossPurposeAAD(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	payload := newOIDCLinkPendingTestPayload(t, time.Now().UTC())
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	// Seal under a *different* cookie name (foreign purpose), inject the value
	// as if it were our link-pending cookie.
	foreignSealed, err := codec.seal(oidcStateCookieName, serialized)
	if err != nil {
		t.Fatalf("seal under foreign purpose: %v", err)
	}

	app := fiber.New()
	app.Get(oidcLinkConfirmPath, func(c fiber.Ctx) error {
		if _, ok := handler.readOIDCLinkPendingCookie(c); ok {
			t.Fatal("expected cross-purpose sealed cookie to be rejected by AAD binding")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	openRequest := httptest.NewRequest("GET", oidcLinkConfirmPath, nil)
	openRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+foreignSealed)
	response, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open cross-purpose request: %v", err)
	}
	defer response.Body.Close()
}

// TestOIDCLinkPendingCookieRejectsExpiredPayload locks the TTL gate. The
// codec round-trip would succeed for an expired payload, so validAt() in
// readOIDCLinkPendingCookie is what bounds the replay window if the cookie
// leaks. If this regression breaks, an attacker holding a 1-day-old leaked
// cookie could resume the password challenge for the linked account.
func TestOIDCLinkPendingCookieRejectsExpiredPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	expiredPayload := oidcLinkPendingPayload{
		TargetUserID: 11,
		Issuer:       "https://idp.example",
		Subject:      "subject-expired",
		Email:        "owner@example.com",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}
	serialized, err := json.Marshal(expiredPayload)
	if err != nil {
		t.Fatalf("marshal expired payload: %v", err)
	}
	sealed, err := codec.seal(oidcLinkPendingCookieName, serialized)
	if err != nil {
		t.Fatalf("seal expired payload: %v", err)
	}

	app := fiber.New()
	app.Get(oidcLinkConfirmPath, func(c fiber.Ctx) error {
		if _, ok := handler.readOIDCLinkPendingCookie(c); ok {
			t.Fatal("expected expired link-pending payload to be rejected by validAt() TTL gate")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	openRequest := httptest.NewRequest("GET", oidcLinkConfirmPath, nil)
	openRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+sealed)
	response, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open expired request: %v", err)
	}
	defer response.Body.Close()
}

// TestNewOIDCLinkPendingPayloadRequiresIdentityFields locks the builder
// invariant: the cookie must never seal a payload that cannot prove which
// user it is for, since the confirmation handler trusts target_user_id to
// look up the account whose password will gate the link.
func TestNewOIDCLinkPendingPayloadRequiresIdentityFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		targetUserID uint
		issuer       string
		subject      string
	}{
		{name: "missing target user id", targetUserID: 0, issuer: "https://idp.example", subject: "subject-1"},
		{name: "missing issuer", targetUserID: 7, issuer: "", subject: "subject-1"},
		{name: "blank issuer", targetUserID: 7, issuer: "   ", subject: "subject-1"},
		{name: "missing subject", targetUserID: 7, issuer: "https://idp.example", subject: ""},
		{name: "blank subject", targetUserID: 7, issuer: "https://idp.example", subject: "\t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newOIDCLinkPendingPayload(time.Now().UTC(), tt.targetUserID, tt.issuer, tt.subject, "owner@example.com"); err == nil {
				t.Fatalf("expected newOIDCLinkPendingPayload to reject %s, got nil error", tt.name)
			}
		})
	}
}
