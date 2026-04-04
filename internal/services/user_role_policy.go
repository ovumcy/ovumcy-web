package services

import (
	"errors"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var ErrAuthUnsupportedRole = errors.New("auth unsupported role")

func NormalizeUserRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func IsOwnerUser(user *models.User) bool {
	return user != nil && NormalizeUserRole(user.Role) == models.RoleOwner
}

func ValidateSupportedWebUser(user *models.User) error {
	if IsOwnerUser(user) {
		return nil
	}
	return ErrAuthUnsupportedRole
}
