package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSettingsPasswordChangeInvalidInput = errors.New("settings password change invalid input")
	ErrSettingsPasswordMismatch           = errors.New("settings password mismatch")
	ErrSettingsInvalidCurrentPassword     = errors.New("settings invalid current password")
	ErrSettingsNewPasswordMustDiffer      = errors.New("settings new password must differ")
	ErrSettingsWeakPassword               = errors.New("settings weak password")
	ErrSettingsPasswordHashFailed         = errors.New("settings password hash failed")
	ErrSettingsRecoveryCodeGenerateFailed = errors.New("settings recovery code generate failed")
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
	if strings.TrimSpace(passwordHash) == "" {
		return ErrSettingsLocalPasswordNotSet
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
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = normalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	return nil
}

// PrepareLocalPasswordHash validates a candidate password pair for enabling
// local auth on an OIDC-only account and returns the resulting bcrypt hash
// WITHOUT touching the database. The hash is meant to be carried through a
// step-up OIDC re-auth flow inside a sealed transport cookie; the matching
// FinalizeLocalPasswordSetup call commits the change once re-auth succeeds.
//
// Splitting prepare/finalize this way means a failed or abandoned re-auth
// leaves no half-completed state in the DB, and the plaintext password never
// has to survive the redirect through the identity provider.
func (service *SettingsService) PrepareLocalPasswordHash(user *models.User, newPassword string, confirmPassword string) (string, error) {
	if user == nil || user.LocalAuthEnabled {
		return "", ErrSettingsPasswordChangeInvalidInput
	}

	newPassword = strings.TrimSpace(newPassword)
	confirmPassword = strings.TrimSpace(confirmPassword)
	if newPassword == "" || confirmPassword == "" {
		return "", ErrSettingsPasswordChangeInvalidInput
	}
	if newPassword != confirmPassword {
		return "", ErrSettingsPasswordMismatch
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return "", ErrSettingsWeakPassword
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSettingsPasswordHashFailed, err)
	}
	return string(hashedPassword), nil
}

// FinalizeLocalPasswordSetup commits a previously prepared local password
// hash, mints a fresh recovery code, and flips LocalAuthEnabled. Called only
// after a successful step-up OIDC re-auth that has been bound to user.ID.
func (service *SettingsService) FinalizeLocalPasswordSetup(user *models.User, preparedPasswordHash string) (string, error) {
	if user == nil || user.LocalAuthEnabled {
		return "", ErrSettingsPasswordChangeInvalidInput
	}
	if strings.TrimSpace(preparedPasswordHash) == "" {
		return "", ErrSettingsPasswordChangeInvalidInput
	}

	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSettingsRecoveryCodeGenerateFailed, err)
	}
	if err := service.users.UpdatePasswordRecoveryCodeAndRevokeSessions(user.ID, preparedPasswordHash, recoveryHash, false); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSettingsPasswordUpdateFailed, err)
	}

	user.PasswordHash = preparedPasswordHash
	user.RecoveryCodeHash = recoveryHash
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = normalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.MustChangePassword = false
	return recoveryCode, nil
}

