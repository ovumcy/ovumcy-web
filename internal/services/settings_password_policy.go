package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSettingsPasswordChangeInvalidInput = errors.New("settings password change invalid input")
	ErrSettingsPasswordMismatch           = errors.New("settings password mismatch")
	ErrSettingsInvalidCurrentPassword     = errors.New("settings invalid current password")
	ErrSettingsNewPasswordMustDiffer      = errors.New("settings new password must differ")
	ErrSettingsWeakPassword               = errors.New("settings weak password")
	ErrSettingsPasswordHashFailed         = errors.New("settings password hash failed")
	ErrSettingsPasswordUpdateFailed       = errors.New("settings password update failed")
)

func (service *SettingsService) ValidatePasswordChange(passwordHash string, currentPassword string, newPassword string, confirmPassword string) error {
	currentPassword = strings.TrimSpace(currentPassword)
	newPassword = strings.TrimSpace(newPassword)
	confirmPassword = strings.TrimSpace(confirmPassword)

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		return ErrSettingsPasswordChangeInvalidInput
	}
	if newPassword != confirmPassword {
		return ErrSettingsPasswordMismatch
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(currentPassword)) != nil {
		return ErrSettingsInvalidCurrentPassword
	}
	if currentPassword == newPassword {
		return ErrSettingsNewPasswordMustDiffer
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return ErrSettingsWeakPassword
	}
	return nil
}

func (service *SettingsService) ChangePassword(user *models.User, currentPassword string, newPassword string, confirmPassword string) error {
	if user == nil {
		return ErrSettingsPasswordChangeInvalidInput
	}
	if err := service.ValidatePasswordChange(user.PasswordHash, currentPassword, newPassword, confirmPassword); err != nil {
		return err
	}

	newPassword = strings.TrimSpace(newPassword)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsPasswordHashFailed, err)
	}

	if err := service.users.UpdatePasswordAndRevokeSessions(user.ID, string(hashedPassword), false); err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsPasswordUpdateFailed, err)
	}
	user.PasswordHash = string(hashedPassword)
	user.AuthSessionVersion = normalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	return nil
}
