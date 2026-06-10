package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) RegenerateRecoveryCode(c *fiber.Ctx) error {
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
	recoveryCode, err := handler.authService.RegenerateRecoveryCode(c.UserContext(), user.ID)
	if err != nil {
		spec := mapRecoveryCodeRegenerationError(err)
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}

	// Session version was bumped atomically with the hash rotation — issue a
	// fresh auth cookie so the current request context remains valid.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	if _, err := handler.setAuthCookie(c, user, false); err != nil {
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.recovery_code_regenerate", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.recovery_code_regenerate", "success")
	return handler.renderRecoveryCodeResponseWithContinuePath(c, user, recoveryCode, fiber.StatusOK, "/settings")
}
