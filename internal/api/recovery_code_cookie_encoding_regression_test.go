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

func TestRecoveryCodeCookieIsNotPlaintextJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	_, recoveryCookie := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-encoding@example.com",
		"StrongPass1",
	)
	if recoveryCookie == "" {
		t.Fatal("expected recovery cookie in register response")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(recoveryCookie)
	if err == nil {
		payload := recoveryCodePagePayload{}
		if json.Unmarshal(decoded, &payload) == nil {
			t.Fatalf("expected recovery cookie to be sealed; got plaintext payload: %#v", payload)
		}
	}

	if strings.Contains(recoveryCookie, "OVUM-") {
		t.Fatalf("expected recovery cookie value not to expose plaintext recovery code")
	}
}

func TestRecoveryCodeCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-TESTCODE-9999", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal recovery cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "OVUM-TESTCODE-9999" {
			t.Fatalf("expected recovery code to round-trip, got %q", state.RecoveryCode)
		}
		if state.ContinuePath != "/settings" {
			t.Fatalf("expected continue path /settings, got %q", state.ContinuePath)
		}
		if state.ContinueTarget != recoveryCodeContinueTargetSettings {
			t.Fatalf("expected settings continue target, got %q", state.ContinueTarget)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestRecoveryCodeCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-TAMPER-CODE0", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("expected tampered recovery cookie to yield empty code, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

// TestRecoveryCodeCookieRefusesUnattributedOwner is the recovery-code arm of the
// same owner-scoping contract the calendar-feed reveal carries
// (TestCalendarFeedRevealCookieRefusesUnattributedOwner): a payload whose `uid`
// is zero names no account, so it must be refused rather than let through for
// want of an owner to compare against. Pinned at both boundaries — the writer
// will not seal a zero owner id, and the reader rejects one (and rejects any
// payload for a session with no id), clearing the cookie on the way out.
//
// The positive anchor in the same test proves the reveal still reaches the
// account it was minted for.
func TestRecoveryCodeCookieRefusesUnattributedOwner(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	const ownerA = uint(99)
	const ownedCode = "OVUM-OWNED-CODE00"
	const unattributedCode = "OVUM-NOOWNER-CODE"

	app := fiber.New()
	// Seal-time guard: an owner id is mandatory, and the refusal writes no cookie.
	app.Get("/seal-unattributed", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 0, unattributedCode, "/settings", recoveryCodeSurfaceDedicated); err == nil {
			t.Fatal("expected setRecoveryCodeIssuanceCookie to refuse a zero owner id")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// Positive anchor: a payload sealed for owner A reveals to owner A.
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, ownerA, ownedCode, "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal recovery cookie for owner A: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, ownerA, "/dashboard")
		if state.RecoveryCode != ownedCode {
			t.Fatalf("owner A must still see the code sealed for owner A, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open-sessionless", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 0, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("a session with no owner id must reveal no code, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	// Read-time guard, driven from a hand-sealed payload the writer would now
	// refuse to mint.
	app.Get("/seal-unattributed-payload", func(c fiber.Ctx) error {
		serialized := []byte(`{"uid":0,"recovery_code":"` + unattributedCode + `","continue_path":"/settings","continue_target":"settings","surface":"dedicated"}`)
		if err := handler.writeSealedCookie(c, recoveryCodeCookieSpec, serialized, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("write sealed unattributed payload: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open-unattributed", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, ownerA, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("an unattributed payload must reveal no code, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	// The writer refuses a zero owner id and leaves no cookie behind.
	unattributedSeal := mustReachNoContent(t, app, "/seal-unattributed", "", "the in-handler zero-owner-id rejection")
	defer func() { _ = unattributedSeal.Body.Close() }()
	if written := responseCookieValue(unattributedSeal.Cookies(), recoveryCodeCookieName); written != "" {
		t.Fatalf("a refused seal must not leave a usable recovery cookie, got %q", written)
	}

	// Positive anchor: owner A's own reveal still works.
	ownedSeal := mustReachNoContent(t, app, "/seal", "", "the owner-A seal")
	defer func() { _ = ownedSeal.Body.Close() }()
	ownedCookie := responseCookieValue(ownedSeal.Cookies(), recoveryCodeCookieName)
	if ownedCookie == "" {
		t.Fatal("expected a sealed recovery cookie for owner A")
	}
	ownedResponse := mustReachNoContent(t, app, "/open", recoveryCodeCookieName+"="+ownedCookie, "the in-handler owner-A reveal assertion")
	defer func() { _ = ownedResponse.Body.Close() }()

	// The same well-formed payload reveals nothing to a session with no id.
	sessionlessResponse := mustReachNoContent(t, app, "/open-sessionless", recoveryCodeCookieName+"="+ownedCookie, "the in-handler sessionless assertion")
	defer func() { _ = sessionlessResponse.Body.Close() }()

	// An unattributed payload reveals nothing and is cleared on the way out.
	craftedSeal := mustReachNoContent(t, app, "/seal-unattributed-payload", "", "the unattributed-payload seal")
	defer func() { _ = craftedSeal.Body.Close() }()
	craftedCookie := responseCookieValue(craftedSeal.Cookies(), recoveryCodeCookieName)
	if craftedCookie == "" {
		t.Fatal("expected a sealed unattributed payload to drive the read path")
	}
	craftedResponse := mustReachNoContent(t, app, "/open-unattributed", recoveryCodeCookieName+"="+craftedCookie, "the in-handler unattributed-payload assertion")
	defer func() { _ = craftedResponse.Body.Close() }()
	assertRevealCookieCleared(t, craftedResponse, recoveryCodeCookieName)
}

func TestRecoveryCodeCookieRejectsForeignKey(t *testing.T) {
	t.Parallel()

	sealingHandler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	openingHandler := &Handler{
		secretKey:    []byte("ffffffffffffffffffffffffffffffff"),
		cookieSecure: true,
	}

	sealingApp := fiber.New()
	sealingApp.Get("/seal", func(c fiber.Ctx) error {
		if err := sealingHandler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-FOREIGN-CODE", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get("/open", func(c fiber.Ctx) error {
		state := openingHandler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("expected rotated-key handler to reject sealed recovery cookie, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}
