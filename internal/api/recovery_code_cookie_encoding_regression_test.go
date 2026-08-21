package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
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

	assertSealedCookieEnvelope(t, recoveryCookie, &recoveryCodePagePayload{})

	if strings.Contains(recoveryCookie, "OVUM-") {
		t.Fatalf("expected recovery cookie value not to expose plaintext recovery code")
	}
}

// TestSealedEnvelopeAroundPlaintextRecoveryPayloadIsRefused is the recovery-code
// arm of TestSealedEnvelopeAroundPlaintextFlashPayloadIsRefused: the v2 envelope
// is framing, not a seal, so base64url(plaintext JSON) behind it must reveal no
// code. Both requests carry the same payload bytes, attributed to the same
// account, and differ only in whether the codec sealed them — the sealed one is
// the positive anchor proving the page still reveals what it should.
func TestSealedEnvelopeAroundPlaintextRecoveryPayloadIsRefused(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	owner := createOnboardingTestUser(t, database, "recovery-cookie-forged-envelope@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")

	const revealedCode = "OVUM-ENVELOPE-COD"
	serialized, err := json.Marshal(recoveryCodePagePayload{
		UserID:         owner.ID,
		RecoveryCode:   revealedCode,
		ContinuePath:   "/dashboard",
		ContinueTarget: recoveryCodeContinueTargetDashboard,
		Surface:        recoveryCodeSurfaceDedicated,
		ExpiresAt:      time.Now().Add(recoveryCodeCookieTTL),
	})
	if err != nil {
		t.Fatalf("marshal recovery payload: %v", err)
	}

	// Positive anchor: sealed, this payload reveals its code to its owner.
	sealedResponse := recoveryCodePageWithCookie(t, app, authCookie, sealCookieForTestApp(t, recoveryCodeCookieName, serialized))
	if sealedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the owner's sealed recovery payload to render, got %d", sealedResponse.StatusCode)
	}
	if !strings.Contains(mustReadBodyString(t, sealedResponse.Body), revealedCode) {
		t.Fatal("expected the sealed recovery payload to reveal its code")
	}

	// The forgery: same bytes, same envelope, no seal.
	forged := secureCookieVersion + "." + base64.RawURLEncoding.EncodeToString(serialized)
	forgedResponse := recoveryCodePageWithCookie(t, app, authCookie, forged)
	if forgedResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a plaintext recovery payload behind the version envelope to be refused with a redirect, got %d", forgedResponse.StatusCode)
	}
	if strings.Contains(mustReadBodyString(t, forgedResponse.Body), revealedCode) {
		t.Fatal("a plaintext recovery payload must not surface its code")
	}

	cleared := responseCookie(forgedResponse.Cookies(), recoveryCodeCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatalf("expected the refused recovery cookie to be cleared, got %#v", cleared)
	}
}

func recoveryCodePageWithCookie(t *testing.T, app *fiber.App, authCookie string, recoveryCookie string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)
	return mustAppResponse(t, app, request)
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
	// The payload carries a live expiry, so the owner id is the only guard that
	// can refuse it — the lifetime bound cannot stand in for the scoping one.
	app.Get("/seal-unattributed-payload", func(c fiber.Ctx) error {
		expiresAt := time.Now().Add(recoveryCodeCookieTTL).UTC().Format(time.RFC3339Nano)
		serialized := []byte(`{"uid":0,"recovery_code":"` + unattributedCode + `","continue_path":"/settings","continue_target":"settings","surface":"dedicated","expires_at":"` + expiresAt + `"}`)
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
