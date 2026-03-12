package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) RegenerateRecoveryCode(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}
	recoveryCode, err := handler.authService.RegenerateRecoveryCode(user.ID)
	if err != nil {
		spec := mapRecoveryCodeRegenerationError(err)
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.recovery_code_regenerate", "success")
	return handler.renderRecoveryCodeResponseWithContinuePath(c, user, recoveryCode, fiber.StatusOK, "/settings")
}
