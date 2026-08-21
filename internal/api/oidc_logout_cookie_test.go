package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// oidcLogoutBridgeTestOwnerID is the account every sealed bridge cookie in
// this file is minted for. The payload names an owner as well as a session,
// and the bridge resolves the stored end-session material by the pair.
const oidcLogoutBridgeTestOwnerID uint = 77

func TestOIDCLogoutBridgeCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-abc", oidcLogoutBridgeTestOwnerID, now); err != nil {
			t.Fatalf("set oidc logout bridge cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		payload := handler.readOIDCLogoutBridgeCookie(c, now)
		if payload.SessionID != "session-id-abc" {
			t.Fatalf("expected session id to round-trip, got %q", payload.SessionID)
		}
		if payload.UserID != oidcLogoutBridgeTestOwnerID {
			t.Fatalf("expected owner id %d to round-trip, got %d", oidcLogoutBridgeTestOwnerID, payload.UserID)
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
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-tamper", oidcLogoutBridgeTestOwnerID, now); err != nil {
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
		if err := sealingHandler.setOIDCLogoutBridgeCookie(c, "session-id-foreign", oidcLogoutBridgeTestOwnerID, now); err != nil {
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

// TestOIDCLogoutBridgeCookieRefusesToMintWithoutAnOwner pins the seal-time
// half of the bridge payload's owner rule. The bridge redirect runs with no
// session, so this payload is the only thing naming the account the provider
// hop acts for; a cookie minted without one would be refused on read anyway,
// which only moves the failure to a later request in a different place.
//
// The refusal is negative — no cookie value — so the same test carries its
// positive anchor: the identical call with an owner does mint one, otherwise a
// writer that sealed nothing at all would pass.
func TestOIDCLogoutBridgeCookieRefusesToMintWithoutAnOwner(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	app := fiber.New()
	app.Get("/seal-unattributed", func(c fiber.Ctx) error {
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-no-owner", 0, now); err == nil {
			t.Fatal("expected sealing a bridge cookie for no owner to be refused")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/seal-attributed", func(c fiber.Ctx) error {
		if err := handler.setOIDCLogoutBridgeCookie(c, "session-id-no-owner", oidcLogoutBridgeTestOwnerID, now); err != nil {
			t.Fatalf("seal for a named owner: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	refused, err := app.Test(httptest.NewRequest("GET", "/seal-unattributed", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unattributed seal request: %v", err)
	}
	defer func() { _ = refused.Body.Close() }()
	if value := responseCookieValue(refused.Cookies(), oidcLogoutBridgeCookieName); value != "" {
		t.Fatalf("a refused mint must leave no usable bridge cookie behind, got %q", value)
	}

	minted, err := app.Test(httptest.NewRequest("GET", "/seal-attributed", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("attributed seal request: %v", err)
	}
	defer func() { _ = minted.Body.Close() }()
	if responseCookieValue(minted.Cookies(), oidcLogoutBridgeCookieName) == "" {
		t.Fatal("the same call naming an owner must mint a bridge cookie; without this anchor the refusal above proves nothing")
	}
}

// TestOIDCLogoutBridgeCookieRefusesAnUnattributedPayloadAndRetractsIt is the
// read-time half, and it is what a bridge cookie minted by a build that
// predates the owner field decodes to: a well-sealed payload naming a session
// and no account. A zero owner is invalid input, never "whichever owner that
// session id belongs to", so the payload is refused and the cookie retracted
// in the same response rather than left to be presented again.
func TestOIDCLogoutBridgeCookieRefusesAnUnattributedPayloadAndRetractsIt(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	// Marshalled from a struct without the field, which is byte-for-byte what
	// the previous version sealed — not a zero value written by this one.
	legacy, err := json.Marshal(struct {
		SessionID     string `json:"session_id"`
		ExpiresAtUnix int64  `json:"expires_at_unix"`
	}{SessionID: "session-id-legacy", ExpiresAtUnix: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("marshal legacy bridge payload: %v", err)
	}
	sealed, err := codec.seal(oidcLogoutBridgeCookieName, legacy)
	if err != nil {
		t.Fatalf("seal legacy bridge payload: %v", err)
	}

	app := fiber.New()
	app.Get("/open", func(c fiber.Ctx) error {
		payload := handler.readOIDCLogoutBridgeCookie(c, now)
		if payload != (oidcLogoutBridgeCookiePayload{}) {
			t.Fatalf("expected a payload naming no owner to be refused, got %+v", payload)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", "/open", nil)
	request.Header.Set("Cookie", oidcLogoutBridgeCookieName+"="+sealed)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	cleared := responseCookie(response.Cookies(), oidcLogoutBridgeCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatalf("a refused bridge cookie must be retracted in the same response, got %#v", cleared)
	}
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
