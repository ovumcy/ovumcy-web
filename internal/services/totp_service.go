package services

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	totpStepSeconds                  = 30
)

var (
	ErrTOTPInvalidCode        = errors.New("totp invalid code")
	ErrTOTPRateLimited        = errors.New("totp rate limited")
	ErrTOTPDisableRateLimited = errors.New("totp disable rate limited")
	ErrTOTPSecretEncrypt      = errors.New("totp secret encrypt failed")
	ErrTOTPSecretDecrypt      = errors.New("totp secret decrypt failed")
	ErrTOTPUpdateFailed       = errors.New("totp update failed")
	ErrTOTPReplayed           = errors.New("totp code already used")
)

// TOTPUserRepository is the minimal repository interface required by TOTPService.
type TOTPUserRepository interface {
	UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error
	// ClaimTOTPStep atomically advances totp_last_used_step to step iff it is
	// strictly greater than the persisted value. Returns true when the row was
	// updated (the step is now consumed by this caller) and false when the step
	// was already at or beyond `step` (replay or concurrent loser).
	ClaimTOTPStep(userID uint, step int64) (bool, error)
}

// TOTPService handles TOTP secret generation, enrollment, validation, and removal.
type TOTPService struct {
	users                TOTPUserRepository
	secretKey            []byte
	attemptPolicy        *AuthAttemptPolicy
	disableAttemptPolicy *AuthAttemptPolicy
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

// ValidateCode decrypts the stored TOTP secret, finds which RFC 6238 step the
// code belongs to (allowing ±1 step of clock skew), and atomically claims that
// step in the database. A replayed or concurrently-consumed step returns
// ErrTOTPReplayed so the caller can surface it separately in security logs.
// Used during the 2FA login challenge.
func (service *TOTPService) ValidateCode(userID uint, encryptedSecret, code string) (bool, error) {
	rawSecret, err := security.DecryptField(encryptedSecret, service.secretKey)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPSecretDecrypt, err)
	}
	step, found := findValidatedTOTPStep(rawSecret, code, time.Now())
	if !found {
		return false, nil
	}
	claimed, err := service.users.ClaimTOTPStep(userID, step)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	if !claimed {
		return false, ErrTOTPReplayed
	}
	return true, nil
}

// findValidatedTOTPStep returns the RFC 6238 time step whose generated code
// matches the supplied code (within ±1 step of skew), and a boolean indicating
// whether a match was found. Comparison is constant-time to avoid leaking which
// step matched through timing.
func findValidatedTOTPStep(rawSecret, code string, now time.Time) (int64, bool) {
	trimmed := strings.TrimSpace(code)
	if len(trimmed) == 0 {
		return 0, false
	}
	currentStep := now.Unix() / totpStepSeconds
	for _, delta := range []int64{0, -1, +1} {
		step := currentStep + delta
		candidate, err := totp.GenerateCode(rawSecret, time.Unix(step*totpStepSeconds, 0))
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(trimmed)) == 1 {
			return step, true
		}
	}
	return 0, false
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

