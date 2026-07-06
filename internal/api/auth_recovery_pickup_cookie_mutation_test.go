package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// This file hardens the register-pickup and recovery-code page cookie helpers
// (register_pickup_cookie.go, recovery_code_page_cookie.go) and the recovery-code
// render helpers (handlers_auth_session_helpers.go) against surviving mutants
// from gremlins baseline run 28758365493.

const recoveryCookieMutationSecret = "0123456789abcdef0123456789abcdef"

// TestEncodePickupExpiryHexAcceptsUnixEpoch pins the lower boundary of
// encodePickupExpiryHex's `if nanos < 0` guard (register_pickup_cookie.go L116).
// The CONDITIONALS_BOUNDARY mutant widens it to `<= 0`, which would reject an
// expiry that lands exactly on the unix epoch (nanos == 0) even though 0 is a
// valid, representable timestamp. The epoch is the only value that separates the
// two predicates.
func TestEncodePickupExpiryHexAcceptsUnixEpoch(t *testing.T) {
	t.Parallel()

	encoded, err := encodePickupExpiryHex(time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("encodePickupExpiryHex(epoch) must succeed for nanos == 0, got error: %v", err)
	}
	if encoded != "0000000000000000" {
		t.Fatalf("encodePickupExpiryHex(epoch) = %q, want the zero-padded hex of 0", encoded)
	}
}

// TestDecodePickupExpiryAcceptsUnixEpoch pins the matching boundary on the
// decode side (register_pickup_cookie.go L134). `if nanos < 0` widened to
// `<= 0` would reject the encoded epoch ("0000000000000000") that
// encodePickupExpiryHex legitimately produces, breaking the encode/decode
// symmetry at the boundary value.
func TestDecodePickupExpiryAcceptsUnixEpoch(t *testing.T) {
	t.Parallel()

	decoded, err := decodePickupExpiry("0000000000000000")
	if err != nil {
		t.Fatalf("decodePickupExpiry(epoch hex) must succeed for nanos == 0, got error: %v", err)
	}
	if !decoded.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("decodePickupExpiry(epoch hex) = %s, want the unix epoch", decoded)
	}
}

// TestRecoveryCodeIssuanceCookieCarriesFutureExpiry pins the recovery-code
// cookie lifetime (recovery_code_page_cookie.go L13, `20 * time.Minute`). The
// ARITHMETIC_BASE mutant rewrites `*` to `/`, collapsing the const to 0, so the
// cookie would be written with an Expires at (or before) now and the browser
// would drop it immediately — the user could never read their recovery code.
// Asserting the Set-Cookie expiry is comfortably in the future fails under the
// collapsed TTL.
func TestRecoveryCodeIssuanceCookieCarriesFutureExpiry(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte(recoveryCookieMutationSecret),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 42, "OVUM-EXPIRY-CODE0", "/dashboard", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal recovery cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	before := time.Now()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	cookie := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cookie == nil {
		t.Fatal("expected a recovery-code Set-Cookie in the response")
	}
	if cookie.Expires.IsZero() {
		t.Fatal("recovery-code cookie must carry an explicit expiry, not a session cookie")
	}
	// A live 20-minute TTL lands well beyond a couple of minutes from now; a
	// collapsed (zero) TTL lands at ~before, i.e. already in the past by the
	// time the browser sees it.
	if !cookie.Expires.After(before.Add(2 * time.Minute)) {
		t.Fatalf("recovery-code cookie expiry %s is not meaningfully in the future (issued around %s); TTL collapsed", cookie.Expires, before)
	}
}

// TestRecoveryCodeDisplayStateRejectsForeignUserID pins the ownership-scoping
// guard in readRecoveryCodeDisplayState (recovery_code_page_cookie.go L145,
// `payload.UserID != 0 && userID != 0 && payload.UserID != userID`). The
// CONDITIONALS_NEGATION mutant on the first operand (`payload.UserID == 0`)
// short-circuits the whole conjunction to false for any real (non-zero) cookie
// owner, so the guard never clears the cookie and one owner's recovery code
// leaks to a different authenticated owner. Sealing for owner A and reading as
// owner B must yield the empty fallback.
//
// This also pins handlers_auth_session_helpers.go L73 (`if user != nil { userID
// = user.ID }`): the render helper stamps the issuing owner's id into the
// cookie via that branch. If it is skipped (userID left 0), the scoping check
// above cannot bind the cookie to an owner, and the same cross-owner read
// succeeds. The render-driven variant below exercises that path end to end.
func TestRecoveryCodeDisplayStateRejectsForeignUserID(t *testing.T) {
	t.Parallel()

	const ownerA = uint(101)
	const ownerB = uint(202)

	handler := &Handler{
		secretKey:    []byte(recoveryCookieMutationSecret),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, ownerA, "OVUM-SCOPE-CODE00", "/dashboard", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal recovery cookie for owner A: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		// Owner B reads the cookie owner A was issued.
		state := handler.readRecoveryCodeDisplayState(c, ownerB, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("owner B must not see owner A's recovery code; got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie for owner A in response")
	}

	openRequest := httptest.NewRequest(http.MethodGet, "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

// TestRenderRecoveryCodeResponseStampsIssuingUser drives renderRecoveryCodeResponse
// through a test route and asserts both render-helper branches in
// handlers_auth_session_helpers.go:
//
//   - L61 `if user != nil { continuePath = services.PostLoginRedirectPath(user) }`:
//     a not-yet-onboarded owner routes the recovery-code continue path to
//     /onboarding rather than the /dashboard default. The CONDITIONALS_NEGATION
//     mutant (`user == nil`) keeps the default, so the persisted ContinueTarget
//     drops from onboarding to dashboard.
//   - L73 `if user != nil { userID = user.ID }`: the issuing owner's id is
//     stamped into the cookie, which the sibling scoping test relies on. Reading
//     the cookie back as that same owner recovers the code; the ContinueTarget
//     assertion below observes L61.
func TestRenderRecoveryCodeResponseStampsIssuingUser(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte(recoveryCookieMutationSecret),
		cookieSecure: true,
		location:     time.UTC,
	}

	const issuingOwner = uint(303)
	const foreignOwner = uint(404)
	// A not-yet-onboarded owner: PostLoginRedirectPath -> /onboarding.
	pendingOwner := &models.User{
		ID:                  issuingOwner,
		Email:               "recovery-render-onboarding@example.com",
		PasswordHash:        "test-hash",
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: false,
		CreatedAt:           time.Now().UTC(),
	}

	app := fiber.New()
	app.Get("/render", func(c fiber.Ctx) error {
		return handler.renderRecoveryCodeResponse(c, pendingOwner, "OVUM-RENDER-CODE0", fiber.StatusOK)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		// Read as the issuing owner: recovers the code, and reflects the
		// onboarding continue target chosen at render time (L61).
		state := handler.readRecoveryCodeDisplayState(c, issuingOwner, "/dashboard")
		if state.RecoveryCode != "OVUM-RENDER-CODE0" {
			t.Fatalf("issuing owner must recover the rendered code, got %q", state.RecoveryCode)
		}
		if state.ContinueTarget != recoveryCodeContinueTargetOnboarding {
			t.Fatalf("expected onboarding continue target from PostLoginRedirectPath(pending owner), got %q", state.ContinueTarget)
		}
		if state.ContinuePath != "/onboarding" {
			t.Fatalf("expected /onboarding continue path, got %q", state.ContinuePath)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open-foreign", func(c fiber.Ctx) error {
		// A different owner must NOT recover the code the render helper stamped
		// with the issuing owner's id (L73 stamps user.ID; L145 enforces it).
		state := handler.readRecoveryCodeDisplayState(c, foreignOwner, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("foreign owner must not recover the issuing owner's rendered code, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	renderResponse, err := app.Test(httptest.NewRequest(http.MethodGet, "/render", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("render request: %v", err)
	}
	defer func() { _ = renderResponse.Body.Close() }()

	cookieValue := responseCookieValue(renderResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected renderRecoveryCodeResponse to set the recovery-code cookie")
	}

	openRequest := httptest.NewRequest(http.MethodGet, "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()

	foreignRequest := httptest.NewRequest(http.MethodGet, "/open-foreign", nil)
	foreignRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	foreignResponse, err := app.Test(foreignRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open-foreign request: %v", err)
	}
	defer func() { _ = foreignResponse.Body.Close() }()
}
