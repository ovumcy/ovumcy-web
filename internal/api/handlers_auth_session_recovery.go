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
	if err := handler.setResetPasswordCookie(c, token, false); err != nil {
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

	token, forced := handler.readResetPasswordCookie(c)
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
	// A FORCED token is the opposite case and must survive the gate.
	// CompleteOIDCLogin mints one unconditionally under oidc_only — it never
	// checks a local password, so there is nothing here for it to leak.
	// CompleteOIDCLinkConfirmation also mints one, but (unlike CompleteOIDCLogin)
	// it now refuses outright while local sign-in is off — the password it
	// checks to authorize the identity link is exactly the credential the
	// operator switched off, and authorizing that link is a bigger leak than
	// the session this gate was written to stop, so gating only the session and
	// leaving the link open is not a fix. A CompleteOIDCLinkConfirmation token
	// can therefore only be minted while local sign-in is still on; this arm
	// only has to cover it redeeming after the operator flips the toggle off in
	// the few minutes between minting and redemption, and still refusing that
	// redeem would strand an owner whose account carries must_change_password
	// with no way to clear it.
	if !forced && !handler.localPublicAuthEnabled() {
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
