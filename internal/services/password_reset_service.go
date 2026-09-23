package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	DefaultRecoveryAttemptsLimit  = 8
	DefaultRecoveryAttemptsWindow = 15 * time.Minute
)

var (
	ErrPasswordRecoveryRateLimited  = errors.New("password recovery rate limited")
	ErrPasswordRecoveryInputInvalid = errors.New("password recovery input invalid")
	ErrPasswordRecoveryCodeInvalid  = errors.New("password recovery code invalid")
)

type PasswordResetService struct {
	auth           *AuthService
	recoveryPolicy *AuthAttemptPolicy
}

func NewPasswordResetService(auth *AuthService, limiter *AttemptLimiter) *PasswordResetService {
	if limiter == nil {
		limiter = NewAttemptLimiter()
	}
	return &PasswordResetService{
		auth:           auth,
		recoveryPolicy: NewAuthAttemptPolicy("recovery", limiter, DefaultRecoveryAttemptsLimit, DefaultRecoveryAttemptsWindow),
	}
}

func (service *PasswordResetService) ConfigureRecoveryAttemptLimits(attempts int, window time.Duration) {
	service.recoveryPolicy.Configure(attempts, window)
}

// IssueResetTokenForUser mints a token with the given purpose. Every caller
// mints one of the two FORCED purposes — the plain login route and OIDC
// link-confirm's password challenge share this via LoginService, which always
// passes PasswordResetTokenPurposeForcedLocal; the OIDC callback's own
// must-change-password branch passes PasswordResetTokenPurposeForcedOIDC
// directly. StartRecovery below is the only recovery-purpose minter and does
// not go through this method.
func (service *PasswordResetService) IssueResetTokenForUser(secretKey []byte, user *models.User, purpose string, ttl time.Duration, now time.Time) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired
	}
	return service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, user.AuthSessionVersion, purpose, ttl, now)
}

// StartRecovery mints a password-reset token for an owner who proves TWO
// factors: the account's current password AND its recovery code. The recovery
// code stands in for the second factor (TOTP), never for the password — an
// owner whose TOTP secret stopped decrypting after a SECRET_KEY rotation still
// knows the password, while an attacker who photographed the recovery code once
// no longer holds a standing takeover credential.
//
// An address that does not normalize to a usable email is malformed input,
// visible to the caller without any lookup, and answers
// ErrPasswordRecoveryInputInvalid. Every failure past that point — unknown
// address, local auth disabled, wrong recovery code, wrong or absent password,
// unsupported role — returns the SAME ErrPasswordRecoveryCodeInvalid, so the
// route reveals neither account existence nor which operand was wrong. Every
// failure, the malformed one included, books its attempt under the same keys
// the budget check above it reads.
func (service *PasswordResetService) StartRecovery(ctx context.Context, secretKey []byte, limiterKey string, email string, rawRecoveryCode string, password string, now time.Time, tokenTTL time.Duration) (string, error) {
	if service.auth == nil {
		return "", errors.New("auth service is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	normalizedEmail := NormalizeAuthEmail(email)
	if service.recoveryPolicy.TooManyRecent(secretKey, limiterKey, normalizedEmail, now) {
		return "", ErrPasswordRecoveryRateLimited
	}
	if normalizedEmail == "" {
		service.recoveryPolicy.AddFailure(secretKey, limiterKey, normalizedEmail, now)
		return "", ErrPasswordRecoveryInputInvalid
	}

	code := NormalizeRecoveryCode(rawRecoveryCode)
	if err := ValidateRecoveryCodeFormat(code); err != nil {
		service.recoveryPolicy.AddFailure(secretKey, limiterKey, normalizedEmail, now)
		return "", ErrPasswordRecoveryCodeInvalid
	}

	user, err := service.auth.FindUserByEmailRecoveryCodeAndPassword(ctx, normalizedEmail, code, password)
	if err != nil {
		if errors.Is(err, ErrRecoveryCodeNotFound) {
			service.recoveryPolicy.AddFailure(secretKey, limiterKey, normalizedEmail, now)
			return "", ErrPasswordRecoveryCodeInvalid
		}
		return "", err
	}

	token, err := service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, user.AuthSessionVersion, PasswordResetTokenPurposeRecovery, tokenTTL, now)
	if err != nil {
		return "", err
	}

	// Unauthenticated flow: forgive this client only (see ResetClient).
	service.recoveryPolicy.ResetClient(limiterKey)
	return token, nil
}

// CompleteReset redeems a reset token: it rewrites the password and rotates
// the recovery code in one compare-and-set write. deliver seals the new
// session and the code's reveal before that write commits (see
// RecoveryCodeDelivery); if it fails, the reset never happened.
func (service *PasswordResetService) CompleteReset(ctx context.Context, secretKey []byte, rawToken string, password string, confirmPassword string, now time.Time, deliver RecoveryCodeDelivery) (*models.User, string, error) {
	if service.auth == nil {
		return nil, "", errors.New("auth service is required")
	}
	if err := service.auth.ValidateResetPasswordInput(password, confirmPassword); err != nil {
		return nil, "", err
	}

	user, err := service.auth.ResolveUserByResetToken(ctx, secretKey, rawToken, now)
	if err != nil {
		return nil, "", ErrInvalidResetToken
	}

	// Capture the password hash that the token was validated against before
	// the write. ResetPasswordAndRotateRecoveryCodeCAS passes it as a CAS
	// predicate so only one of any concurrent redeems of the same token can
	// win; the loser receives ErrResetTokenAlreadyConsumed.
	oldPasswordHash := user.PasswordHash

	recoveryCode, err := service.auth.ResetPasswordAndRotateRecoveryCodeCAS(ctx, user, oldPasswordHash, password, deliver)
	if err != nil {
		return nil, "", err
	}

	return user, recoveryCode, nil
}
