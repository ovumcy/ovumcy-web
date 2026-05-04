package services

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	DefaultTOTPAttemptsLimit         = 5
	DefaultTOTPAttemptsWindow        = 15 * time.Minute
	DefaultTOTPDisableAttemptsLimit  = 5
	DefaultTOTPDisableAttemptsWindow = 15 * time.Minute
	totpReplayTTL                    = 90 * time.Second
)

var (
	ErrTOTPInvalidCode          = errors.New("totp invalid code")
	ErrTOTPRateLimited          = errors.New("totp rate limited")
	ErrTOTPDisableRateLimited   = errors.New("totp disable rate limited")
	ErrTOTPSecretEncrypt        = errors.New("totp secret encrypt failed")
	ErrTOTPSecretDecrypt        = errors.New("totp secret decrypt failed")
	ErrTOTPUpdateFailed         = errors.New("totp update failed")
)

type recentCode struct {
	code      string
	expiresAt time.Time
}

// TOTPUserRepository is the minimal repository interface required by TOTPService.
type TOTPUserRepository interface {
	UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error
}

// TOTPService handles TOTP secret generation, enrollment, validation, and removal.
type TOTPService struct {
	users                TOTPUserRepository
	secretKey            []byte
	attemptPolicy        *AuthAttemptPolicy
	disableAttemptPolicy *AuthAttemptPolicy
	usedCodes            sync.Map
}

// NewTOTPService creates a TOTPService. secretKey is used to encrypt TOTP secrets
// before they are written to the database. limiter is the shared AttemptLimiter;
// pass nil to use a dedicated one.
func NewTOTPService(users TOTPUserRepository, secretKey []byte, limiter *AttemptLimiter) *TOTPService {
	return &TOTPService{
		users:                users,
		secretKey:            secretKey,
		attemptPolicy:        NewAuthAttemptPolicy("totp", limiter, DefaultTOTPAttemptsLimit, DefaultTOTPAttemptsWindow),
		disableAttemptPolicy: NewAuthAttemptPolicy("totp.disable", limiter, DefaultTOTPDisableAttemptsLimit, DefaultTOTPDisableAttemptsWindow),
	}
}

// CheckRateLimit returns ErrTOTPRateLimited when the client or user has exceeded
// the allowed number of verification attempts within the configured window.
func (service *TOTPService) CheckRateLimit(secretKey []byte, clientKey string, userID uint, now time.Time) error {
	if service.attemptPolicy.TooManyRecent(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now) {
		return ErrTOTPRateLimited
	}
	return nil
}

// RecordFailure records a failed verification attempt for rate-limit tracking.
func (service *TOTPService) RecordFailure(secretKey []byte, clientKey string, userID uint, now time.Time) {
	service.attemptPolicy.AddFailure(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now)
}

// ResetAttempts clears the failure counter after a successful verification.
func (service *TOTPService) ResetAttempts(secretKey []byte, clientKey string, userID uint) {
	service.attemptPolicy.Reset(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10))
}

// CheckDisableRateLimit returns ErrTOTPDisableRateLimited when the client or user
// has exceeded the allowed number of disable-confirmation password attempts.
func (service *TOTPService) CheckDisableRateLimit(secretKey []byte, clientKey string, userID uint, now time.Time) error {
	if service.disableAttemptPolicy.TooManyRecent(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now) {
		return ErrTOTPDisableRateLimited
	}
	return nil
}

// RecordDisableFailure records a failed disable-confirmation password attempt.
func (service *TOTPService) RecordDisableFailure(secretKey []byte, clientKey string, userID uint, now time.Time) {
	service.disableAttemptPolicy.AddFailure(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now)
}

// ResetDisableAttempts clears the disable-confirmation failure counter after success.
func (service *TOTPService) ResetDisableAttempts(secretKey []byte, clientKey string, userID uint) {
	service.disableAttemptPolicy.Reset(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10))
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

// ValidateCode decrypts the stored TOTP secret and validates the 6-digit code.
// Returns false without an error if the code was already used within totpReplayTTL
// (replay protection). Used during the 2FA login challenge.
func (service *TOTPService) ValidateCode(userID uint, encryptedSecret, code string) (bool, error) {
	rawSecret, err := security.DecryptField(encryptedSecret, service.secretKey)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPSecretDecrypt, err)
	}
	now := time.Now()
	if service.isRecentlyUsed(userID, code, now) {
		return false, nil
	}
	valid := totp.Validate(code, rawSecret)
	if valid {
		service.markUsed(userID, code, now)
	}
	return valid, nil
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

func (service *TOTPService) isRecentlyUsed(userID uint, code string, now time.Time) bool {
	v, ok := service.usedCodes.Load(userID)
	if !ok {
		return false
	}
	rc := v.(recentCode)
	return rc.code == code && now.Before(rc.expiresAt)
}

func (service *TOTPService) markUsed(userID uint, code string, now time.Time) {
	service.usedCodes.Store(userID, recentCode{
		code:      code,
		expiresAt: now.Add(totpReplayTTL),
	})
}
