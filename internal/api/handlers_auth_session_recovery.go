package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) ForgotPassword(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		spec := authLocalRecoveryDisabledErrorSpec()
		handler.logSecurityError(c, "auth.recovery_start", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	now := time.Now().In(handler.location)
	input, parseError := parseForgotPasswordInput(c)
	if parseError != "" {
		spec := authValidationErrorSpec(parseError)
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}

	if strings.TrimSpace(input.RecoveryCode) == "" {
		if acceptsJSON(c) {
			return c.JSON(fiber.Map{
				"ok":        true,
				"next_step": "recovery_code",
			})
		}
		handler.setFlashCookie(c, FlashPayload{
			ForgotEmail: input.Email,
		})
		return redirectToPath(c, "/forgot-password")
	}

	token, err := handler.passwordResetSvc.StartRecovery(
		c.Context(),
		handler.secretKey,
		c.IP(),
		input.Email,
		input.RecoveryCode,
		input.Password,
		now,
		30*time.Minute,
	)
	if err != nil {
		spec := mapPasswordRecoveryStartError(err)
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.setResetPasswordCookie(c, token); err != nil {
		spec := authResetTokenCreateErrorSpec()
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.logSecurityEvent(c, "auth.recovery_start", "success")

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok": true,
		})
	}

	return redirectToPath(c, "/reset-password")
}

func (handler *Handler) ResetPassword(c fiber.Ctx) error {
	input, parseError := parseResetPasswordInput(c)
	if parseError != "" {
		spec := authValidationErrorSpec(parseError)
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}

	token := handler.readResetPasswordCookie(c)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		spec := invalidResetTokenErrorSpec()
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}
	// The redeem half of the local recovery flow has to stop being reachable at
	// the same moment ForgotPassword above starts refusing to start one:
	// otherwise a token minted before local public auth was switched off still
	// rewrites the password, still sets local_auth_enabled back to true
	// (UpdatePasswordRecoveryCodeAndRevokeSessionsCAS), and still mints a session
	// on an instance whose operator turned local sign-in off. The factors are all
	// verified at issuance, so this is the account's POSTURE, not session
	// issuance parity.
	//
	// The decision reads the token's own SIGNED purpose (PRIV-4), never a
	// cookie-carried bool: only a forced-from-OIDC token is exempt, because
	// CompleteOIDCLogin mints one without ever checking a local password and an
	// oidc_only instance must keep redeeming it. A forced-from-LOCAL token — the
	// plain login route, and OIDC link-confirm's own password challenge — is
	// gated exactly like a recovery token: the factor that produced it is
	// exactly what the operator disabled, and refusing its redeem would not
	// strand anyone, since the account still reaches a genuine forced-from-OIDC
	// token through a plain OIDC sign-in. CompleteOIDCLinkConfirmation itself
	// now refuses outright while local sign-in is off (the instance-level gate
	// added there), so its forced-from-LOCAL token can only be minted while the
	// toggle is still on; this arm still has to cover the narrow race where the
	// operator flips the toggle off in the few minutes between that mint and
	// redemption (the token's ≤30-minute TTL). Refusing it there too is the
	// correct answer, not a gap: the token was produced by the local-password
	// factor regardless of when the toggle later moved.
	//
	// A token that fails to parse (expired, malformed, unrecognised purpose)
	// never reaches this refusal: PasswordResetTokenRefusedByLocalAuthGate
	// answers false for it, and CompleteReset below independently re-parses
	// and gives the accurate "invalid reset token" answer. Otherwise a
	// forced-from-OIDC reset that simply outlives its 30-minute TTL — the
	// ordinary case on an oidc_only instance — would be told local recovery is
	// unavailable and would log a local-recovery-disabled event for a routine
	// expiry.
	if !handler.localPublicAuthEnabled() && services.PasswordResetTokenRefusedByLocalAuthGate(handler.secretKey, token, time.Now()) {
		handler.clearResetPasswordCookie(c)
		spec := authLocalRecoveryDisabledErrorSpec()
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}
	user, recoveryCode, err := handler.passwordResetSvc.CompleteReset(
		c.Context(),
		handler.secretKey,
		token,
		input.Password,
		input.ConfirmPassword,
		time.Now(),
	)
	if err != nil {
		spec := mapPasswordResetCompleteError(err)
		if spec.Key == "invalid reset token" {
			handler.clearResetPasswordCookie(c)
		}
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}

	// A recovery reset carries no remember-me control — the flow asks for an
	// email, a recovery code and a password, and never for a device choice — so
	// it takes the same default an unchecked login box takes. Passing true here
	// minted the 30-day remembered cookie on a device nobody said to remember,
	// on the one path whose whole premise is that the owner just lost control of
	// her password.
	if _, err := handler.setAuthCookie(c, user, false); err != nil {
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearOIDCLogoutBridgeCookie(c)
	handler.clearResetPasswordCookie(c)
	handler.logSecurityEvent(c, "auth.reset_password", "success")

	return handler.renderRecoveryCodeResponse(c, user, recoveryCode, fiber.StatusOK)
}
