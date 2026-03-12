package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) OwnerOnly(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "access.owner_only", spec)
		return respondGlobalMappedError(c, spec)
	}
	if !services.IsOwnerUser(user) {
		spec := ownerAccessRequiredErrorSpec()
		handler.logSecurityError(c, "access.owner_only", spec)
		return respondGlobalMappedError(c, spec)
	}
	return c.Next()
}
