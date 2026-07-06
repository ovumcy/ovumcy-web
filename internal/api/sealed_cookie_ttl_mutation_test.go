package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Mutation-hardening for the sealed-cookie transport/codec cluster
// (continuing PR #177). Every assertion here is written to FAIL under a
// specific surviving/not-covered gremlins mutant so the mutant is killed:
//
//   - The per-purpose TTL constants (oidcStateCookieTTL, oidcStepupCookieTTL,
//     oidcLinkPendingCookieTTL, resetPasswordCookieTTL, recoveryCodeCookieTTL,
//     totpPendingCookieTTL) were ARITHMETIC_BASE "NOT COVERED": `N * time.Minute`
//     mutates to `N / time.Minute` == 0s (integer division; N < 60e9 ns), so a
//     freshly issued cookie would expire immediately. Each test pins the expiry
//     distance against a *literal* duration — never the production constant,
//     which the mutation would move in lockstep with the assertion.
//   - The clear-cookie backdating in clearSealedCookie (`-1 * time.Hour`) was a
//     LIVED ARITHMETIC_BASE: `-1 / time.Hour` == 0s leaves the clearing cookie
//     un-backdated. The existing `.Before(time.Now())` assertions were too loose
//     to catch it; here we require the clear to land comfortably in the past.
//   - oidcLogoutBridgeCookiePayload.validAt's expiry comparison (`<=`) was a
//     LIVED CONDITIONALS_BOUNDARY at the exact now==ExpiresAtUnix second.
//   - readRecoveryCodeDisplayState's empty-ContinueTarget branch (`== ""`) was a
//     LIVED CONDITIONALS_NEGATION.

// ttlSlack bounds the drift between the test's clock reads and the internal
// time.Now() calls in the production write paths. Large enough to never flake,
// far smaller than any real TTL (min real TTL is 5 minutes), so a mutated
// TTL of 0s can never masquerade as the real value inside the window.
const ttlSlack = 30 * time.Second

func ttlMutationTestHandler() *Handler {
	return &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
}

// assertDurationApprox fails unless got is within ttlSlack of want. Used to pin
// cookie-expiry distances against literal TTL durations so an ARITHMETIC_BASE
// mutation of the TTL constant (to 0s, or a `+`/`-` variant) is caught.
func assertDurationApprox(t *testing.T, label string, got, want time.Duration) {
	t.Helper()
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	if delta > ttlSlack {
		t.Fatalf("%s: expected ~%s, got %s (delta %s exceeds slack %s)", label, want, got, delta, ttlSlack)
	}
}

// --- oidcStateCookieTTL: 10 * time.Minute (payload ExpiresAt) ---------------

func TestNewOIDCAuthStateExpiryHonorsTenMinuteTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	state, err := newOIDCAuthState(now)
	if err != nil {
		t.Fatalf("newOIDCAuthState: %v", err)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, state.ExpiresAt)
	if err != nil {
		t.Fatalf("parse ExpiresAt %q: %v", state.ExpiresAt, err)
	}
	// Literal 10m, NOT oidcStateCookieTTL: the mutation edits the constant.
	assertDurationApprox(t, "oidc state cookie TTL", expiresAt.UTC().Sub(now.UTC()), 10*time.Minute)

	// A 0s TTL would make the fresh state fail its own validity check.
	if !state.validAt(now) {
		t.Fatal("freshly issued OIDC state must be valid at issuance time")
	}
}

// --- oidcStepupCookieTTL: 10 * time.Minute (payload ExpiresAt) --------------

func TestNewOIDCStepupStateExpiryHonorsTenMinuteTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	state, err := newOIDCStepupState(now, oidcStepupPurposeLocalPasswordSetup, 7, "hashed-pw")
	if err != nil {
		t.Fatalf("newOIDCStepupState: %v", err)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, state.ExpiresAt)
	if err != nil {
		t.Fatalf("parse ExpiresAt %q: %v", state.ExpiresAt, err)
	}
	assertDurationApprox(t, "oidc stepup cookie TTL", expiresAt.UTC().Sub(now.UTC()), 10*time.Minute)

	if !state.validAt(now) {
		t.Fatal("freshly issued OIDC stepup state must be valid at issuance time")
	}
}

// --- oidcLinkPendingCookieTTL: 5 * time.Minute (payload ExpiresAt) ----------

func TestNewOIDCLinkPendingPayloadExpiryHonorsFiveMinuteTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	payload, err := newOIDCLinkPendingPayload(now, 11, "https://idp.example", "subject-123", "user@example.test")
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
	if err != nil {
		t.Fatalf("parse ExpiresAt %q: %v", payload.ExpiresAt, err)
	}
	assertDurationApprox(t, "oidc link-pending cookie TTL", expiresAt.UTC().Sub(now.UTC()), 5*time.Minute)

	if !payload.validAt(now) {
		t.Fatal("freshly issued OIDC link-pending payload must be valid at issuance time")
	}
}

// --- totpPendingCookieTTL: 5 * time.Minute (payload ExpiresAt) --------------
//
// Round-trips through the real seal/write path, then opens the produced sealed
// cookie to read the encoded ExpiresAt back out — the parse helpers do not
// surface it, so we decode the payload directly.

func TestTOTPPendingCookieExpiryHonorsFiveMinuteTTL(t *testing.T) {
	t.Parallel()

	handler := ttlMutationTestHandler()
	app := fiber.New()
	app.Get("/set", func(c fiber.Ctx) error {
		if err := handler.setTOTPPendingCookie(c, 42, true); err != nil {
			t.Fatalf("setTOTPPendingCookie: %v", err)
		}
		return c.SendStatus(http.StatusNoContent)
	})

	before := time.Now()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	after := time.Now()

	sealed := responseCookieValue(response.Cookies(), totpPendingCookieName)
	if sealed == "" {
		t.Fatal("expected a sealed TOTP pending cookie")
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	decoded, err := codec.open(totpPendingCookieName, sealed)
	if err != nil {
		t.Fatalf("open sealed TOTP pending cookie: %v", err)
	}
	var payload totpPendingCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal TOTP pending payload: %v", err)
	}

	// ExpiresAt was set from an internal time.Now(); it must land ~5m ahead of
	// the window we bracketed the write with. A 0s TTL would place it inside
	// [before, after], far below the 5m literal.
	minExpiry := before.Add(5*time.Minute - ttlSlack)
	maxExpiry := after.Add(5*time.Minute + ttlSlack)
	if payload.ExpiresAt.Before(minExpiry) || payload.ExpiresAt.After(maxExpiry) {
		t.Fatalf("expected TOTP pending ExpiresAt in [%s, %s], got %s", minExpiry, maxExpiry, payload.ExpiresAt)
	}
}

// --- resetPasswordCookieTTL: 30 * time.Minute (cookie Expires attribute) ----

func TestResetPasswordCookieExpiresHonorsThirtyMinuteTTL(t *testing.T) {
	t.Parallel()

	handler := ttlMutationTestHandler()
	app := fiber.New()
	app.Get("/set", func(c fiber.Ctx) error {
		if err := handler.setResetPasswordCookie(c, "reset-token-fixture", false); err != nil {
			t.Fatalf("setResetPasswordCookie: %v", err)
		}
		return c.SendStatus(http.StatusNoContent)
	})

	before := time.Now()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	after := time.Now()

	cookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if cookie == nil {
		t.Fatal("expected a reset-password Set-Cookie")
	}
	if cookie.Expires.IsZero() {
		t.Fatal("reset-password cookie must carry an explicit expiry, not be session-scoped")
	}

	minExpiry := before.Add(30*time.Minute - ttlSlack)
	maxExpiry := after.Add(30*time.Minute + ttlSlack)
	if cookie.Expires.Before(minExpiry) || cookie.Expires.After(maxExpiry) {
		t.Fatalf("expected reset-password cookie expiry in [%s, %s], got %s", minExpiry, maxExpiry, cookie.Expires)
	}
}

// --- recoveryCodeCookieTTL: 20 * time.Minute (cookie Expires attribute) -----

func TestRecoveryCodeCookieExpiresHonorsTwentyMinuteTTL(t *testing.T) {
	t.Parallel()

	handler := ttlMutationTestHandler()
	app := fiber.New()
	app.Get("/set", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 9, "recovery-code-fixture", "/dashboard", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("setRecoveryCodeIssuanceCookie: %v", err)
		}
		return c.SendStatus(http.StatusNoContent)
	})

	before := time.Now()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/set", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	after := time.Now()

	cookie := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cookie == nil {
		t.Fatal("expected a recovery-code Set-Cookie")
	}
	if cookie.Expires.IsZero() {
		t.Fatal("recovery-code cookie must carry an explicit expiry, not be session-scoped")
	}

	minExpiry := before.Add(20*time.Minute - ttlSlack)
	maxExpiry := after.Add(20*time.Minute + ttlSlack)
	if cookie.Expires.Before(minExpiry) || cookie.Expires.After(maxExpiry) {
		t.Fatalf("expected recovery-code cookie expiry in [%s, %s], got %s", minExpiry, maxExpiry, cookie.Expires)
	}
}

// --- clearSealedCookie backdating: -1 * time.Hour ---------------------------
//
// A `-1 / time.Hour` mutation yields 0s, so the clearing cookie is stamped at
// "now" rather than an hour in the past. Requiring the clear to land well
// before the request even started kills that mutant (and the +/- variants).

func TestClearSealedCookieBackdatesExpiryIntoThePast(t *testing.T) {
	t.Parallel()

	handler := ttlMutationTestHandler()
	app := fiber.New()
	app.Get("/clear", func(c fiber.Ctx) error {
		handler.clearSealedCookie(c, resetPasswordCookieSpec)
		return c.SendStatus(http.StatusNoContent)
	})

	requestStart := time.Now()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/clear", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	cookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if cookie == nil {
		t.Fatal("expected a clearing Set-Cookie")
	}
	if cookie.Value != "" {
		t.Fatalf("expected an empty clearing cookie value, got %q", cookie.Value)
	}
	// -1h backdating must put the expiry at least half an hour before the
	// request began; a 0s (un-backdated) clear would sit at ~requestStart.
	cutoff := requestStart.Add(-30 * time.Minute)
	if !cookie.Expires.Before(cutoff) {
		t.Fatalf("expected clearing cookie expiry before %s (backdated ~1h), got %s", cutoff, cookie.Expires)
	}
}

// --- oidcLogoutBridgeCookiePayload.validAt boundary (<=) ---------------------
//
// At the exact second now.Unix() == ExpiresAtUnix the cookie is still valid
// (`<=`). A CONDITIONALS_BOUNDARY mutation to `<` rejects that boundary second.

func TestOIDCLogoutBridgeValidAtBoundarySecondIsStillValid(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	payload := oidcLogoutBridgeCookiePayload{
		SessionID:     "session-boundary",
		ExpiresAtUnix: now.UTC().Unix(),
	}
	if !payload.validAt(now) {
		t.Fatal("expected payload to be valid at the exact expiry second (now == ExpiresAtUnix)")
	}

	// One second past expiry must be rejected (guards against the boundary
	// moving the other direction, and pins the comparison to the right side).
	if payload.validAt(now.Add(time.Second)) {
		t.Fatal("expected payload one second past expiry to be rejected")
	}
}

// --- readRecoveryCodeDisplayState empty-ContinueTarget branch (== "") --------
//
// When the sealed payload carries no ContinueTarget, the target is derived from
// ContinuePath; when it carries one, that value is sanitized. A
// CONDITIONALS_NEGATION mutation (`== ""` -> `!= ""`) swaps the two branches, so
// the derived-from-path case collapses to the sanitize-default ("dashboard").

func TestReadRecoveryCodeDisplayStateEmptyTargetDerivesFromPath(t *testing.T) {
	t.Parallel()

	handler := ttlMutationTestHandler()
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}

	// Empty ContinueTarget + a /settings ContinuePath: the `== ""` branch must
	// derive "settings" from the path. Under the negation it would fall to the
	// else branch and sanitize "" -> "dashboard".
	payload := recoveryCodePagePayload{
		UserID:       9,
		RecoveryCode: "recovery-code-fixture",
		ContinuePath: "/settings",
		// ContinueTarget intentionally empty.
		Surface: recoveryCodeSurfaceDedicated,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal recovery payload: %v", err)
	}
	sealed, err := codec.seal(recoveryCodeCookieName, serialized)
	if err != nil {
		t.Fatalf("seal recovery payload: %v", err)
	}

	app := fiber.New()
	app.Get("/read", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 9, "/dashboard")
		if state.ContinueTarget != recoveryCodeContinueTargetSettings {
			t.Fatalf("expected ContinueTarget %q derived from path, got %q", recoveryCodeContinueTargetSettings, state.ContinueTarget)
		}
		if state.ContinuePath != "/settings" {
			t.Fatalf("expected ContinuePath %q, got %q", "/settings", state.ContinuePath)
		}
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/read", nil)
	request.Header.Set("Cookie", recoveryCodeCookieName+"="+sealed)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}
}
