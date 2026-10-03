package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// ShowTOTPChallengePage renders the 2FA code entry page after a successful
// password login when the user has TOTP enabled.
func (handler *Handler) ShowTOTPChallengePage(c fiber.Ctx) error {
	_, err := handler.parseTOTPPendingCookie(c)
	if err != nil {
		// No valid pending cookie — send back to login.
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title": localizedPageTitle(messages, "auth.2fa.title", "Ovumcy | Two-Factor Authentication"),
		// The flash carries the error SPEC key ("totp invalid code"); the template
		// translates whatever ErrorKey holds, so it has to be resolved to a locale
		// key here — exactly as the other auth pages do. Passing the spec key
		// straight through rendered it verbatim in every language.
		"ErrorKey": services.AuthErrorTranslationKey(services.ResolveAuthErrorSource(flash.AuthError)),
	}
	return handler.render(c, "auth_2fa", data)
}

// parseTOTPChallengeCode reads the submitted code from the request body, over
// either transport the endpoint serves: the JSON body `docs/openapi.yaml`
// publishes for API clients, and the urlencoded form the challenge page posts.
// A body that cannot be decoded yields "", answered as `totp invalid code`
// exactly like a wrong code. The query string is never consulted, so a code
// carried in a link is not a submission.
func parseTOTPChallengeCode(c fiber.Ctx) string {
	input := totpChallengeInput{}
	if err := bindRequestBody(c, &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.Code)
}

// wellFormedTOTPLoginCode reports whether a submitted sign-in code has the one
// shape an authenticator produces: exactly six ASCII digits. Anything else can
// never match, so it is refused without a compare and without the attempt budget.
func wellFormedTOTPLoginCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for index := range len(code) {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return true
}

// VerifyTOTPLogin validates the 6-digit TOTP code submitted on the challenge page.
// On success it issues the auth session cookie and redirects to the dashboard.
func (handler *Handler) VerifyTOTPLogin(c fiber.Ctx) error {
	grant, err := handler.parseTOTPPendingCookie(c)
	userID, rememberMe, oidcLogoutStateID := grant.UserID, grant.RememberMe, grant.OIDCLogoutStateID
	if err != nil {
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	// A code that cannot be a TOTP code is refused before the budget is
	// consulted: no compare happens for it, so it must neither draw an attempt
	// nor hold a slot (even for the instant before a refund) that the owner's
	// own submission could then find taken, nor clear the pending cookie when
	// the budget is spent.
	code := parseTOTPChallengeCode(c)
	if !wellFormedTOTPLoginCode(code) {
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	// The attempt is reserved here, before any code is compared, and stays
	// booked only when a compared code is wrong (or replayed). Every other way
	// out of this handler past this point — a refused grant, an internal
	// error, a correct code — gives the slot back, so only a failed compare
	// draws the budget while a burst of concurrent submissions cannot all pass
	// the same count.
	reservation, err := handler.totpService.ReserveAttempt(handler.secretKey, c.IP(), userID, time.Now())
	if err != nil {
		// Invalidate the pending session so an exhausted (or stolen) cookie
		// cannot be reused; the user must re-authenticate with their password
		// to obtain a fresh challenge.
		handler.clearTOTPPendingCookie(c)
		spec := totpRateLimitedErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	user, err := handler.authService.FindByID(c.Context(), userID)
	if err != nil || !handler.totpService.Verifiable(user) {
		// Verifiable, not the raw TOTPEnabled column: a pending-TOTP cookie
		// naming an account whose secret has since become unverifiable
		// (SECRET_KEY rotation, or 2FA was disabled after the cookie was
		// minted) can never be resolved by any code — treat it the same as
		// an expired/invalid pending session rather than falling through to
		// ValidateCode, which would fail every submission with an opaque
		// internal error instead of sending the owner toward the
		// operator-reset escape hatch the next login attempt raises.
		reservation.Refund()
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}
	// The grant dies with the credential that earned it: a password change, a
	// session revocation or an operator's forced reset since the first factor
	// passed refuses it here — before ValidateCode, so a stale grant cannot
	// even spend the owner's current TOTP step. The owner signs in again.
	if !services.SecondFactorGrantCurrent(grant.SessionVersion, &user) {
		reservation.Refund()
		handler.clearTOTPPendingCookie(c)
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	valid, err := handler.totpService.ValidateCode(c.Context(), userID, user.TOTPSecret, code)
	if errors.Is(err, services.ErrTOTPReplayed) {
		// Same response shape as a plain invalid code so an attacker cannot
		// distinguish replay from a wrong guess. We log replay separately for
		// security observability (potential captured-code attempt). The
		// reservation stays booked: a replay is a failure.
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityEvent(c, "auth.2fa", "replay_rejected")
		return handler.respondMappedError(c, spec)
	}
	if err != nil {
		reservation.Refund()
		spec := totpInternalErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}
	if !valid {
		// The reservation stays booked: a wrong code is a failure.
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}
	// A correct code was never a failure. The count goes back to what it was;
	// the client's counter is only forgiven further down, once the session is
	// minted.
	reservation.Refund()

	handler.clearTOTPPendingCookie(c)

	sessionID, err := handler.setAuthCookie(c, &user, rememberMe)
	if err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	// A challenge raised by CompleteOIDCLogin (the account had TOTP enabled,
	// so the OIDC callback deferred the session it would otherwise have
	// minted) staged any provider-logout material under an opaque id carried
	// in the pending cookie above, because no session id existed yet at that
	// point to key it by. Relocate it onto the session id this request just
	// minted, mirroring CompleteOIDCLogin's own Save/Delete shape so a
	// TOTP-gated OIDC sign-in ends up with exactly the same provider-logout
	// integration a non-gated one gets. A challenge with no such id — local
	// login, or an OIDC login whose account carried no logout state — takes
	// the Delete arm, which is a no-op against the session id this request
	// just minted.
	if oidcLogoutStateID != "" {
		if err := handler.moveOIDCLogoutState(c.Context(), oidcLogoutStateID, sessionID, userID, time.Now()); err != nil {
			spec := authSessionCreateErrorSpec()
			handler.logSecurityError(c, "auth.2fa", spec)
			handler.clearSessionEndCookies(c)
			return handler.respondMappedError(c, spec)
		}
	} else {
		_ = handler.oidcLogoutStateSvc.Delete(c.Context(), sessionID, userID)
	}
	handler.clearOIDCLogoutBridgeCookie(c)

	// The client's failure count is forgiven only now that the session is
	// minted and its provider-logout state has followed it: a correct code
	// whose session could not be issued proved nothing lasting, so a refusal
	// above keeps the count it found.
	handler.totpService.ResetAttempts(handler.secretKey, c.IP(), userID)
	handler.logSecurityEvent(c, "auth.2fa", "success")
	return redirectOrJSON(c, "/")
}
