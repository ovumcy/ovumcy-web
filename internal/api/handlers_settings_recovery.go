package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) RegenerateRecoveryCode(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	recoveryCode, err := handler.authService.RegenerateRecoveryCode(user.ID)
	if err != nil {
		return handler.respondMappedError(c, mapRecoveryCodeRegenerationError(err))
	}

	return handler.renderRecoveryCodeResponseWithContinuePath(c, user, recoveryCode, fiber.StatusOK, "/settings")
}
