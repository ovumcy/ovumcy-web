package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OwnerOnly(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return respondGlobalMappedError(c, unauthorizedErrorSpec())
	}
	if !services.IsOwnerUser(user) {
		return respondGlobalMappedError(c, ownerAccessRequiredErrorSpec())
	}
	return c.Next()
}
