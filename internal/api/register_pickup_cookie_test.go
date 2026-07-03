package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func newPickupTestHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: false,
	}
}

func TestNewRegisterPickupPayloadShape(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	payload, err := newRegisterPickupPayload(now, "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("newRegisterPickupPayload: %v", err)
	}
	if !validPickupNonceShape(payload.Nonce) {
		t.Fatalf("expected valid pickup nonce, got %q", payload.Nonce)
	}
	if payload.RC != "OVUM-AAAA-BBBB-CCCC" {
		t.Fatalf("expected RC preserved, got %q", payload.RC)
	}
	if len(payload.EXP) != 16 {
		t.Fatalf("expected EXP length 16, got %d (%q)", len(payload.EXP), payload.EXP)
	}
	if !payload.validAt(now) {
		t.Fatal("expected fresh payload to be valid")
	}
	if payload.validAt(now.Add(registerPickupCookieTTL + time.Second)) {
		t.Fatal("expected payload past TTL to be invalid")
	}
}

func TestNewRegisterPickupPayloadDrawsFreshNonce(t *testing.T) {
	t.Parallel()

	first, err := newRegisterPickupPayload(time.Now(), "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("first payload: %v", err)
	}
	second, err := newRegisterPickupPayload(time.Now(), "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("second payload: %v", err)
	}
	if first.Nonce == second.Nonce {
		t.Fatalf("expected unique nonces per pickup, got the same %q twice", first.Nonce)
	}
}

func TestNewRegisterPickupPayloadRejectsBadRecoveryCode(t *testing.T) {
	t.Parallel()

	for _, badCode := range []string{
		"",
		"too-short",
		"NOT-AAAA-BBBB-CCCC",
		"OVUM-XXXXX-BBBB-CCCC",
	} {
		if _, err := newRegisterPickupPayload(time.Now(), badCode); err == nil {
			t.Fatalf("expected error for recovery code %q", badCode)
		}
	}
}

// TestRegisterPickupRealAndDecoyMatchInLength is the load-bearing check for
// the per-request enumeration oracle closure: real and decoy ciphertexts
// must have identical lengths so an attacker watching Set-Cookie size cannot
// distinguish branches. Built on plaintext serialization shape; if anyone
// touches the payload struct without preserving fixed-width fields, this
// test will catch it.
func TestRegisterPickupRealAndDecoyMatchInLength(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)

	realA, err := newRegisterPickupPayload(now, "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("real payload A: %v", err)
	}
	realB, err := newRegisterPickupPayload(now, "OVUM-1234-5678-9ABC")
	if err != nil {
		t.Fatalf("real payload B: %v", err)
	}
	decoy, err := newRegisterPickupDecoyPayload(now)
	if err != nil {
		t.Fatalf("decoy payload: %v", err)
	}

	realABytes, err := json.Marshal(realA)
	if err != nil {
		t.Fatalf("marshal realA: %v", err)
	}
	realBBytes, err := json.Marshal(realB)
	if err != nil {
		t.Fatalf("marshal realB: %v", err)
	}
	decoyBytes, err := json.Marshal(decoy)
	if err != nil {
		t.Fatalf("marshal decoy: %v", err)
	}

	if len(realABytes) != len(decoyBytes) || len(realABytes) != len(realBBytes) {
		t.Fatalf(
			"serialized length mismatch: realA=%d realB=%d decoy=%d",
			len(realABytes), len(realBBytes), len(decoyBytes),
		)
	}
}

func TestSetAndPopRegisterPickupCookieRoundTrip(t *testing.T) {
	t.Parallel()

	handler := newPickupTestHandler(t)
	now := time.Now().UTC()
	original, err := newRegisterPickupPayload(now, "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	var cookieValue string
	setApp := fiber.New()
	setApp.Get("/set", func(c fiber.Ctx) error {
		if err := handler.setRegisterPickupCookie(c, original); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	setResp, err := setApp.Test(httptest.NewRequest("GET", "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("set request: %v", err)
	}
	defer setResp.Body.Close()
	for _, c := range setResp.Cookies() {
		if c.Name == registerPickupCookieName {
			cookieValue = c.Value
		}
	}
	if cookieValue == "" {
		t.Fatal("expected pickup cookie to be set")
	}

	popApp := fiber.New()
	var popped registerPickupPayload
	var poppedOK bool
	popApp.Get("/welcome", func(c fiber.Ctx) error {
		popped, poppedOK = handler.popRegisterPickupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	popReq := httptest.NewRequest("GET", "/welcome", nil)
	popReq.Header.Set("Cookie", registerPickupCookieName+"="+cookieValue)
	popResp, err := popApp.Test(popReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pop request: %v", err)
	}
	defer popResp.Body.Close()

	if !poppedOK {
		t.Fatal("expected popRegisterPickupCookie to succeed")
	}
	if popped.Nonce != original.Nonce || popped.RC != original.RC || popped.EXP != original.EXP {
		t.Fatalf("payload not preserved across round-trip: got %+v want %+v", popped, original)
	}
}

func TestPopRegisterPickupCookieWrongKeyReturnsEmpty(t *testing.T) {
	t.Parallel()

	signer := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: false,
	}
	verifier := &Handler{
		secretKey:    []byte("fedcba9876543210fedcba9876543210"),
		cookieSecure: false,
	}

	now := time.Now().UTC()
	payload, err := newRegisterPickupPayload(now, "OVUM-AAAA-BBBB-CCCC")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	var cookieValue string
	setApp := fiber.New()
	setApp.Get("/set", func(c fiber.Ctx) error {
		if err := signer.setRegisterPickupCookie(c, payload); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	setResp, err := setApp.Test(httptest.NewRequest("GET", "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("set request: %v", err)
	}
	defer setResp.Body.Close()
	for _, c := range setResp.Cookies() {
		if c.Name == registerPickupCookieName {
			cookieValue = c.Value
		}
	}

	popApp := fiber.New()
	var popped registerPickupPayload
	var poppedOK bool
	popApp.Get("/welcome", func(c fiber.Ctx) error {
		popped, poppedOK = verifier.popRegisterPickupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})
	popReq := httptest.NewRequest("GET", "/welcome", nil)
	popReq.Header.Set("Cookie", registerPickupCookieName+"="+cookieValue)
	popResp, err := popApp.Test(popReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pop request: %v", err)
	}
	defer popResp.Body.Close()

	if poppedOK {
		t.Fatalf("expected wrong-key pop to fail, got %+v", popped)
	}
}

func TestPopRegisterPickupCookieTamperedValueReturnsEmpty(t *testing.T) {
	t.Parallel()

	handler := newPickupTestHandler(t)
	popApp := fiber.New()
	var poppedOK bool
	popApp.Get("/welcome", func(c fiber.Ctx) error {
		_, poppedOK = handler.popRegisterPickupCookie(c)
		return c.SendStatus(fiber.StatusNoContent)
	})

	popReq := httptest.NewRequest("GET", "/welcome", nil)
	popReq.Header.Set("Cookie", registerPickupCookieName+"=v2.garbage-payload")
	popResp, err := popApp.Test(popReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pop request: %v", err)
	}
	defer popResp.Body.Close()

	if poppedOK {
		t.Fatal("expected tampered pickup cookie to be rejected")
	}
}
