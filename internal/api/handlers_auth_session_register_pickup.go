package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

func (handler *Handler) respondRegisterPickup(c fiber.Ctx, outcome registerPickupOutcome) error {
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
		if err := handler.registerPickupTokens.Issue(c.Context(), outcome.payload.Nonce, outcome.userID, expiresAt); err != nil {
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
func (handler *Handler) PickupRegister(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		handler.clearRegisterPickupCookie(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	payload, ok := handler.popRegisterPickupCookie(c)
	if !ok {
		return handler.redirectToPostRegisterSignin(c, "missing_or_expired")
	}

	// The token stays exactly as redeemable as it is until there is something
	// sealed and ready to hand over: identifying the pending pickup is a
	// read (Peek), never a spend, so a failure resolving the account or
	// sealing either the session or the reveal below reaches no Consume call
	// at all. The single-use grant is spent only once both are sealed
	// (WEB-64, the WEB-58 shape: seal before the spend). The DB-side row
	// staying unconsumed only pays off for a real client if the cookie
	// carrying its nonce+RC also survives the response: the two post-Peek
	// seal failures below (auth_cookie_failed, recovery_cookie_failed) hand
	// the pickup cookie back via redirectToPostRegisterSigninKeepingPickupCookie
	// instead of the plain redirect every other exit uses.
	userID, live, err := handler.registerPickupTokens.Peek(c.Context(), payload.Nonce, time.Now())
	if err != nil {
		return handler.redirectToPostRegisterSignin(c, "consume_failed")
	}
	if !live || userID == 0 {
		return handler.redirectToPostRegisterSignin(c, "decoy_or_replay")
	}

	user, err := handler.authService.FindByID(c.Context(), userID)
	if err != nil {
		return handler.redirectToPostRegisterSignin(c, "user_not_found")
	}

	if strings.TrimSpace(user.RecoveryCodeHash) == "" {
		return handler.redirectToPostRegisterSignin(c, "recovery_hash_missing")
	}

	if !handler.authService.VerifyStoredRecoveryCode(user.RecoveryCodeHash, payload.RC) {
		return handler.redirectToPostRegisterSignin(c, "decoy_or_mismatch")
	}

	// The pickup form carries no remember-me control either, so it takes the same
	// default for the same reason — the N+1 site of the recovery reset above, and
	// the only other place that asked for a remembered device on the owner's
	// behalf. Sealed here, not written: writing waits until the token is
	// actually spent below.
	session, err := handler.prepareAuthCookie(&user, false)
	if err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.register_pickup", spec)
		return handler.redirectToPostRegisterSigninKeepingPickupCookie(c, payload, "auth_cookie_failed")
	}

	continuePath := services.PostLoginRedirectPath(&user)
	reveal, err := handler.sealRecoveryCodeIssuanceCookie(user.ID, payload.RC, continuePath, recoveryCodeSurfaceInlineRegister)
	if err != nil {
		spec := authRecoveryCodePersistErrorSpec()
		handler.logSecurityError(c, "auth.register_pickup", spec)
		return handler.redirectToPostRegisterSigninKeepingPickupCookie(c, payload, "recovery_cookie_failed")
	}

	// Both cookies are sealed and ready to write; only now is the single-use
	// grant spent. A lost race — a concurrent redeem, or the token expiring
	// in the interval above — leaves consumed false: the sealed values above
	// are discarded, nothing is written to the response, and the request
	// falls through to the same neutral redirect a replay always got.
	consumedUserID, consumed, err := handler.registerPickupTokens.Consume(c.Context(), payload.Nonce, time.Now())
	if err != nil {
		return handler.redirectToPostRegisterSignin(c, "consume_failed")
	}
	if !consumed || consumedUserID != userID {
		return handler.redirectToPostRegisterSignin(c, "decoy_or_replay")
	}

	handler.writeAuthCookie(c, &user, session)
	handler.clearOIDCLogoutBridgeCookie(c)
	handler.writeSealed(c, reveal)

	handler.logSecurityEvent(c, "auth.register_pickup", "success")
	return c.Redirect().Status(fiber.StatusSeeOther).To("/register")
}

// refuseRegisterPickupRequest is the first-party guard's exit for
// GET /register/welcome: the endpoint's own neutral refusal, so a forged
// cross-site navigation is answered exactly like a stale, decoy or
// already-consumed pickup, and is audited in the same vocabulary.
//
// It reproduces redirectToPostRegisterSignin rather than calling it because of
// the one line it must NOT run: that helper clears the pickup cookie, which is
// right for every exit it serves — each refuses a cookie that is spent or was
// never real. This one refuses the REQUEST and leaves a good cookie standing.
// Clearing it would hand the forged navigation the outcome the guard exists to
// deny: the owner arriving to find her hand-off gone.
func (handler *Handler) refuseRegisterPickupRequest(c fiber.Ctx, reason string) error {
	// The flash is written only when a hand-off was actually presented. A
	// speculative load, or a stray link on a session holding no pickup, would
	// otherwise leave the owner an error about a registration she never started —
	// a prefetch discards the body but keeps the Set-Cookie. It is the line the
	// calendar-feed refusal already draws for its own audit line.
	if strings.TrimSpace(c.Cookies(registerPickupCookieName)) != "" {
		handler.setFlashCookie(c, FlashPayload{AuthError: "register pickup unavailable"})
	}
	handler.logSecurityEvent(c, "auth.register_pickup", "redirect_signin", securityEventField("reason", reason))
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}

func (handler *Handler) redirectToPostRegisterSignin(c fiber.Ctx, reason string) error {
	handler.clearRegisterPickupCookie(c)
	handler.setFlashCookie(c, FlashPayload{AuthError: "register pickup unavailable"})
	if reason != "" {
		handler.logSecurityEvent(c, "auth.register_pickup", "redirect_signin", securityEventField("reason", reason))
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}

// redirectToPostRegisterSigninKeepingPickupCookie is redirectToPostRegisterSignin's
// twin for the two outcomes reachable only after Peek succeeded: a failure
// sealing the auth cookie or the recovery-code reveal (WEB-64). popRegisterPickupCookie
// above already retracted the pickup cookie as it read it — that read-spends
// contract is unconditional and covers every other exit from this handler —
// so this re-seals the SAME payload back onto the response, undoing that
// retraction for exactly these two branches. The register_pickup_tokens row
// was only Peeked, never Consumed, so the browser leaving this request still
// holding the pickup cookie is what lets the owner retry against the very
// same unconsumed row instead of losing the recovery code the moment sealing
// glitches.
//
// A seal failure here needs a crypto/codec fault (rand.Reader, HKDF, an
// unavailable cookie secret) that no request can provoke, so restoring the
// cookie opens no enumeration oracle: an attacker cannot make this branch
// fire on demand to learn anything a normal missing/tampered/decoy/replay
// response would not already tell them.
func (handler *Handler) redirectToPostRegisterSigninKeepingPickupCookie(c fiber.Ctx, payload registerPickupPayload, reason string) error {
	if err := handler.setRegisterPickupCookie(c, payload); err != nil {
		// codecov:ignore:start -- unreachable in practice within one request:
		// the codec that must fail here already built and opened this very
		// payload moments ago at pop time, and Handler caches it once
		// (cookieCodecOnce), so it cannot start refusing mid-request. The
		// other failure setRegisterPickupCookie can return, payload.validAt
		// going false, would need this single request to outlive the
		// pickup's own TTL. Nothing to keep alive either way, so fall back to
		// the ordinary clearing exit rather than leaving a broken Set-Cookie
		// a retry could never use anyway.
		return handler.redirectToPostRegisterSignin(c, reason)
		// codecov:ignore:end
	}
	handler.setFlashCookie(c, FlashPayload{AuthError: "register pickup unavailable"})
	if reason != "" {
		handler.logSecurityEvent(c, "auth.register_pickup", "redirect_signin", securityEventField("reason", reason))
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}
