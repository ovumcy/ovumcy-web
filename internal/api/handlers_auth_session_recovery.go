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
	// A FORCED token is the opposite case and must survive the gate: the two
	// paths that mint one — CompleteOIDCLogin and CompleteOIDCLinkConfirmation —
	// are exactly the paths still live under oidc_only, and refusing their redeem
	// would strand an owner whose account carries must_change_password with no
	// way to clear it.
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

	if _, err := handler.setAuthCookie(c, user, true); err != nil {
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
