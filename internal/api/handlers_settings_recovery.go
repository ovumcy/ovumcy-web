package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) RegenerateRecoveryCode(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	// WEB-54: no early `!user.LocalAuthEnabled` exit here — that used to answer
	// an OIDC-only account with a distinct 403 "local password required"
	// before ever reading the submitted password, which was a faster and
	// differently-shaped oracle than the one closed everywhere else this
	// account state is refused. validateSettingsActionPassword now carries
	// this account exactly like every other password-gated settings action:
	// budgeted, equalized-timing, and answered with the same 401 "invalid
	// password" a wrong password gets. Regression:
	// TestSettingsReauthMergesNoLocalPasswordIntoInvalidPassword.
	reauth, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec, cause)
		return handler.respondMappedError(c, spec)
	}

	// The rotation revokes every session, this one included, so the device is
	// re-issued one at the version the write stored. That session and the
	// code's reveal are sealed before the write commits: if either cannot be,
	// the rotation rolls back and the owner keeps the code she has and the
	// session she is using (WEB-58).
	//
	// The re-issue carries the owner's remember-me choice, like every other
	// posture change. This one mints directly instead of through
	// refreshCurrentSession — it owns its own error scope — so the choice has to
	// be read here too, or this becomes the one screen that quietly un-remembers
	// a device.
	deliver, delivery := handler.newRecoveryCodeDelivery(sessionWasRemembered(c), settingsContinuePath, recoveryCodeSurfaceDedicated)
	_, err := handler.authService.RegenerateRecoveryCode(c.Context(), user, deliver)
	if delivery.failure != nil {
		spec := mapRecoveryCodeDeliveryError(delivery.failure)
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	if errors.Is(err, services.ErrAuthSessionVersionChanged) {
		return handler.respondSignedOutRefusal(c, handler.refuseSessionRevokedDuring(c, "auth.recovery_code_regenerate", "recovery_code_regenerate"))
	}
	if err != nil {
		spec := mapRecoveryCodeRegenerationError(err)
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	// The rotation committed and its delivery was sealed: only now does the
	// correct password clear settings.reauth. A rotation that rolled back, for
	// whatever reason, leaves the count where it was.
	reauth.resetBudget()

	handler.writeAuthCookie(c, user, delivery.session)
	handler.writeSealed(c, delivery.reveal)
	handler.logSecurityEvent(c, "auth.recovery_code_regenerate", "success")
	return respondRecoveryCodeNextStep(c, fiber.StatusOK, delivery.nextPath)
}

// settingsContinuePath is where a reveal minted from the settings page sends
// the owner once she has saved the code.
func settingsContinuePath(*models.User) string {
	return "/settings"
}
