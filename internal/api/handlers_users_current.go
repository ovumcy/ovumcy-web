package api

import "github.com/gofiber/fiber/v3"

// GetCurrentUser returns a small, stable representation of the session
// subject. The shape is the minimum needed for an external wrapper to learn
// who it is talking to: the account identity (id, email, display name), the
// authorization tier (role), and the lifecycle flags it must respect before
// issuing mutating calls (onboarding_completed, local_auth_enabled,
// must_change_password). Sensitive fields (password/recovery hashes, TOTP
// secret) are never included.
func (handler *Handler) GetCurrentUser(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	return c.JSON(fiber.Map{
		"id":                   user.ID,
		"email":                user.Email,
		"display_name":         user.DisplayName,
		"role":                 user.Role,
		"onboarding_completed": user.OnboardingCompleted,
		"local_auth_enabled":   user.LocalAuthEnabled,
		"must_change_password": user.MustChangePassword,
	})
}
