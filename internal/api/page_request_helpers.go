package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) redirectAuthenticatedUserIfPresent(c *fiber.Ctx) (bool, error) {
	if user, err := handler.authenticateRequest(c); err == nil {
		if redirectErr := c.Redirect(services.PostLoginRedirectPath(user), fiber.StatusSeeOther); redirectErr != nil {
			return false, redirectErr
		}
		return true, nil
	}
	return false, nil
}

func (handler *Handler) currentUserOrRedirectToLogin(c *fiber.Ctx) (*models.User, bool, error) {
	user, ok := currentUser(c)
	if !ok {
		if redirectErr := c.Redirect("/login", fiber.StatusSeeOther); redirectErr != nil {
			return nil, false, redirectErr
		}
		return nil, true, nil
	}
	return user, false, nil
}

func currentUserOrUnauthorized(c *fiber.Ctx) (*models.User, bool, error) {
	user, ok := currentUser(c)
	if !ok {
		if sendErr := respondGlobalMappedError(c, unauthorizedErrorSpec()); sendErr != nil {
			return nil, false, sendErr
		}
		return nil, true, nil
	}
	return user, false, nil
}

func currentRequestLocation(c *fiber.Ctx) *time.Location {
	location, ok := c.Locals(contextLocationKey).(*time.Location)
	if !ok || location == nil {
		return nil
	}
	return location
}

func (handler *Handler) requestLocation(c *fiber.Ctx) *time.Location {
	location := currentRequestLocation(c)
	if location != nil {
		return location
	}
	if handler.location != nil {
		return handler.location
	}
	return time.UTC
}

func (handler *Handler) currentPageViewContext(c *fiber.Ctx) (string, map[string]string, time.Time) {
	location := handler.requestLocation(c)
	return currentLanguage(c), currentMessages(c), time.Now().In(location)
}

func (handler *Handler) optionalAuthenticatedUser(c *fiber.Ctx) *models.User {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		return nil
	}
	return user
}
