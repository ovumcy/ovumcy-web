package services

import (
	"errors"
	"fmt"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

var (
	ErrTOTPInvalidCode   = errors.New("totp invalid code")
	ErrTOTPSecretEncrypt = errors.New("totp secret encrypt failed")
	ErrTOTPSecretDecrypt = errors.New("totp secret decrypt failed")
	ErrTOTPUpdateFailed  = errors.New("totp update failed")
)

// TOTPUserRepository is the minimal repository interface required by TOTPService.
type TOTPUserRepository interface {
	UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error
}

// TOTPService handles TOTP secret generation, enrollment, validation, and removal.
type TOTPService struct {
	users     TOTPUserRepository
	secretKey []byte
}

// NewTOTPService creates a TOTPService. secretKey must be the application secret
// key; it is used to encrypt TOTP secrets before they are written to the database.
func NewTOTPService(users TOTPUserRepository, secretKey []byte) *TOTPService {
	return &TOTPService{users: users, secretKey: secretKey}
}

// GenerateSetupKey generates a new TOTP key for the given issuer and account name.
// The raw secret (key.Secret()) should be passed to ValidateCodeRaw during enrollment
// and then to EnableTOTP once the user confirms their code.
func (service *TOTPService) GenerateSetupKey(issuer, accountName string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
	})
}

// ValidateCodeRaw validates a 6-digit code against a raw (unencrypted) TOTP secret.
// Used during enrollment before the secret has been persisted.
func (service *TOTPService) ValidateCodeRaw(rawSecret, code string) bool {
	return totp.Validate(code, rawSecret)
}

// ValidateCode decrypts the stored TOTP secret and validates the 6-digit code against it.
// Used during the 2FA login challenge.
func (service *TOTPService) ValidateCode(encryptedSecret, code string) (bool, error) {
	rawSecret, err := security.DecryptField(encryptedSecret, service.secretKey)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPSecretDecrypt, err)
	}
	return totp.Validate(code, rawSecret), nil
}

// EnableTOTP encrypts rawSecret and stores it alongside totp_enabled=true for the user.
func (service *TOTPService) EnableTOTP(userID uint, rawSecret string) error {
	encrypted, err := security.EncryptField(rawSecret, service.secretKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTOTPSecretEncrypt, err)
	}
	if err := service.users.UpdateTOTPFields(userID, encrypted, true); err != nil {
		return fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	return nil
}

// DisableTOTP clears the TOTP secret and sets totp_enabled=false for the user.
func (service *TOTPService) DisableTOTP(userID uint) error {
	if err := service.users.UpdateTOTPFields(userID, "", false); err != nil {
		return fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	return nil
}
