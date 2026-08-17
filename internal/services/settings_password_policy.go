package services

import (
	"context"
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
	ErrSettingsPasswordTooLong            = errors.New("settings password too long")
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
	if err := settingsPasswordPolicyError(ValidatePasswordStrength(newPassword)); err != nil {
		return err
	}
	return nil
}

// settingsPasswordPolicyError is the settings-layer twin of
// authPasswordPolicyError: the same split, carried through this package's own
// sentinels so both the change-password form and the local-password setup form
// can name the length refusal on its own.
func settingsPasswordPolicyError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPasswordTooLong):
		return ErrSettingsPasswordTooLong
	default:
		return ErrSettingsWeakPassword
	}
}

func (service *SettingsService) ChangePassword(ctx context.Context, attempt ReauthAttempt, user *models.User, currentPassword string, newPassword string, confirmPassword string) error {
	if user == nil {
		return ErrSettingsPasswordChangeInvalidInput
	}
	// The current-password check inside ValidatePasswordChange is a re-auth
	// factor, so it draws on the same budget as the erasure flows. Without this
	// the change-password form would be a faster password oracle than the login
	// form it protects.
	now := attempt.at()
	identity := attempt.identity()
	if service.reauthPolicy.TooManyRecent(service.reauthSecretKey, attempt.clientBucket(), identity, now) {
		return ErrSettingsReauthRateLimited
	}
	if err := service.ValidatePasswordChange(user.PasswordHash, currentPassword, newPassword, confirmPassword); err != nil {
		if errors.Is(err, ErrSettingsInvalidCurrentPassword) {
			service.reauthPolicy.AddFailure(service.reauthSecretKey, attempt.clientBucket(), identity, now)
		}
		return err
	}
	service.reauthPolicy.Reset(service.reauthSecretKey, attempt.clientBucket(), identity)

	newPassword = strings.TrimSpace(newPassword)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), passwordHashCost)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsPasswordHashFailed, err)
	}

	if err := service.users.UpdatePasswordAndRevokeSessions(ctx, user.ID, string(hashedPassword), false); err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsPasswordUpdateFailed, err)
	}
	user.PasswordHash = string(hashedPassword)
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
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
	if err := settingsPasswordPolicyError(ValidatePasswordStrength(newPassword)); err != nil {
		return "", err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), passwordHashCost)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSettingsPasswordHashFailed, err)
	}
	return string(hashedPassword), nil
}

// FinalizeLocalPasswordSetup commits a previously prepared local password
// hash, mints a fresh recovery code, and flips LocalAuthEnabled. Called only
// after a successful step-up OIDC re-auth that has been bound to user.ID.
func (service *SettingsService) FinalizeLocalPasswordSetup(ctx context.Context, user *models.User, preparedPasswordHash string) (string, error) {
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
	if err := service.users.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, user.ID, preparedPasswordHash, recoveryHash, false); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSettingsPasswordUpdateFailed, err)
	}

	user.PasswordHash = preparedPasswordHash
	user.RecoveryCodeHash = recoveryHash
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.MustChangePassword = false
	return recoveryCode, nil
}
