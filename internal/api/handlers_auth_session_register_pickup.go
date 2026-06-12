package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

const registerPickupNextPath = "/register/welcome"

// registerPickupOutcome wraps the pickup payload built for the new-user
// branch or the decoy payload built for the duplicate-email branch, so that
// the Register handler can keep both branches structurally identical and
// respondRegisterPickup is the single point where the sealed cookie is
// written. Build errors are extremely rare (rand.Reader failures) but are
// surfaced consistently between branches.
//
// `userID` is set only on the real-pickup branch. respondRegisterPickup uses
// it to persist a server-side single-use row in register_pickup_tokens
// before the sealed cookie is set, so a captured cookie cannot be replayed
// to mint a second auth session inside the 5-minute TTL (Finding #3 fix).
// Decoy pickups deliberately skip the DB row: their nonce never resolves on
// consume, which is observationally identical to a real pickup that has
// already been consumed or expired.
type registerPickupOutcome struct {
	payload registerPickupPayload
	userID  uint
	err     error
}

func registerPickupOutcomeReal(now time.Time, userID uint, recoveryCode string) registerPickupOutcome {
	if userID == 0 {
		return registerPickupOutcome{err: errors.New("pickup outcome requires user id")}
	}
	payload, err := newRegisterPickupPayload(now, recoveryCode)
	return registerPickupOutcome{payload: payload, userID: userID, err: err}
}

func registerPickupOutcomeDecoy(now time.Time) registerPickupOutcome {
	payload, err := newRegisterPickupDecoyPayload(now)
	return registerPickupOutcome{payload: payload, err: err}
}

func (handler *Handler) respondRegisterPickup(c *fiber.Ctx, outcome registerPickupOutcome) error {
	if outcome.err != nil {
		spec := registerPickupCookieErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	// Real pickups get a server-side single-use row so the welcome handler
	// can atomically consume the nonce. Decoy pickups (userID == 0) skip the
	// insert; their nonce never resolves and falls through to the same
	// /login redirect as a stale or already-consumed pickup. This is the
	// server-side guarantee that closes the cookie-replay window.
	if outcome.userID != 0 {
		expiresAt := time.Now().UTC().Add(registerPickupCookieTTL)
		if err := handler.registerPickupTokens.Issue(c.UserContext(), outcome.payload.Nonce, outcome.userID, expiresAt); err != nil {
			spec := registerPickupCookieErrorSpec()
			handler.logSecurityError(c, "auth.register", spec)
			return handler.respondMappedError(c, spec)
		}
	}

	if err := handler.setRegisterPickupCookie(c, outcome.payload); err != nil {
		spec := registerPickupCookieErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"ok":        true,
			"next_step": "register_welcome",
			"next_path": registerPickupNextPath,
		})
	}
	return redirectToPath(c, registerPickupNextPath)
}

// PickupRegister completes a fresh registration by exchanging the sealed
// pickup cookie that POST /api/v1/users handed back for the real auth
// session cookie and the inline recovery-code surface. The same endpoint
// handles three indistinguishable-from-outside outcomes:
//
//   - real pickup: cookie decrypts to a uid that resolves to a user whose
//     RecoveryCodeHash matches the pickup recovery code; we issue auth +
//     recovery cookies and redirect to /register to reveal the code.
//   - decoy pickup (duplicate email branch): cookie decrypts to a random uid
//     whose bcrypt(recovery_code) verification fails; we redirect to /login
//     with a neutral flash.
//   - missing / tampered / expired pickup: same /login redirect with the
//     same flash so an attacker who arrives at /register/welcome by hand
//     cannot tell the failure mode from the response.
//
// See SECURITY.md "Register enumeration" for the residual two-step oracle
// (which redirect target the holder of a pickup cookie observes after their
// own POST /api/v1/users).
func (handler *Handler) PickupRegister(c *fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		handler.clearRegisterPickupCookie(c)
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	payload, ok := handler.popRegisterPickupCookie(c)
	if !ok {
		return handler.redirectToPostRegisterSignin(c, "missing_or_expired")
	}

	// Atomic single-use consume: a captured cookie that has already been
	// exchanged once, or a decoy whose nonce was never persisted, returns
	// consumed == false here and falls through to the same neutral
	// "register pickup unavailable" /login redirect.
	userID, consumed, err := handler.registerPickupTokens.Consume(c.UserContext(), payload.Nonce, time.Now())
	if err != nil {
		return handler.redirectToPostRegisterSignin(c, "consume_failed")
	}
	if !consumed || userID == 0 {
		return handler.redirectToPostRegisterSignin(c, "decoy_or_replay")
	}

	user, err := handler.authService.FindByID(c.UserContext(), userID)
	if err != nil {
		return handler.redirectToPostRegisterSignin(c, "user_not_found")
	}

	if strings.TrimSpace(user.RecoveryCodeHash) == "" {
		return handler.redirectToPostRegisterSignin(c, "recovery_hash_missing")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.RecoveryCodeHash), []byte(payload.RC)); err != nil {
		return handler.redirectToPostRegisterSignin(c, "decoy_or_mismatch")
	}

	if _, err := handler.setAuthCookie(c, &user, true); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.register_pickup", spec)
		return handler.redirectToPostRegisterSignin(c, "auth_cookie_failed")
	}
	handler.clearOIDCLogoutBridgeCookie(c)

	continuePath := services.PostLoginRedirectPath(&user)
	if err := handler.setRecoveryCodeIssuanceCookie(c, user.ID, payload.RC, continuePath, recoveryCodeSurfaceInlineRegister); err != nil {
		handler.clearAuthCookie(c)
		spec := authRecoveryCodePersistErrorSpec()
		handler.logSecurityError(c, "auth.register_pickup", spec)
		return handler.redirectToPostRegisterSignin(c, "recovery_cookie_failed")
	}

	handler.logSecurityEvent(c, "auth.register_pickup", "success")
	return c.Redirect("/register", fiber.StatusSeeOther)
}

func (handler *Handler) redirectToPostRegisterSignin(c *fiber.Ctx, reason string) error {
	handler.clearRegisterPickupCookie(c)
	handler.setFlashCookie(c, FlashPayload{AuthError: "register pickup unavailable"})
	if reason != "" {
		handler.logSecurityEvent(c, "auth.register_pickup", "redirect_signin", securityEventField("reason", reason))
	}
	return c.Redirect("/login", fiber.StatusSeeOther)
}
