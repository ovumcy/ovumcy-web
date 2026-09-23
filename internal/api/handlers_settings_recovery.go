package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func (handler *Handler) RegenerateRecoveryCode(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	if !user.LocalAuthEnabled {
		spec := settingsLocalPasswordRequiredErrorSpec()
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	if _, spec, valid := handler.validateSettingsActionPassword(c); !valid {
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
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
	if err != nil {
		spec := mapRecoveryCodeRegenerationError(err)
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}

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
