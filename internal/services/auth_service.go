package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrRecoveryCodeNotFound      = errors.New("recovery code not found")
	ErrInvalidResetToken         = errors.New("invalid reset token")
	ErrResetTokenAlreadyConsumed = errors.New("reset token already consumed")
	ErrAuthUserRequired          = errors.New("auth user is required")
	ErrAuthUserNotFound          = errors.New("auth user not found")
	ErrAuthUserLookupFailed      = errors.New("auth user lookup failed")
	ErrRecoveryCodeGenerate      = errors.New("recovery code generation failed")
	ErrRecoveryCodeUpdate        = errors.New("recovery code update failed")
	ErrAuthRegisterInvalid       = errors.New("auth register invalid input")
	ErrAuthEmailExists           = errors.New("auth email already exists")
	ErrAuthRegisterFailed        = errors.New("auth register failed")
	ErrAuthPasswordMismatch      = errors.New("auth register password mismatch")
	ErrAuthWeakPassword          = errors.New("auth register weak password")
	ErrAuthInvalidCreds          = errors.New("auth invalid credentials")
	ErrAuthResetInvalid          = errors.New("auth reset invalid input")
	ErrAuthPasswordHash          = errors.New("auth password hash failed")
	ErrAuthPasswordUpdate        = errors.New("auth password update failed")
)

// recoveryCodeTimingEqualizationHash and credentialsTimingEqualizationHash are
// fixed bcrypt-cost-10 placeholder hashes used by the equalize* helpers below
// to spend bcrypt compute time on the early-return paths in recovery and
// login. They are never compared against a real credential — the result of
// bcrypt.CompareHashAndPassword is discarded — and never authenticate anyone.
const recoveryCodeTimingEqualizationHash = "$2a$10$ReZgUuXu2GXtC.RZ/q2QyesBFX182a3ycbr78sbtgURmuOyc3ygtG" // #nosec G101 -- fixed placeholder bcrypt hash, see comment above; never authenticates a real user
const credentialsTimingEqualizationHash = "$2a$10$h7pMPVpw/fZjbsXnbtpfD.UzmSCNk0FmbmMkP7wKDlO7IqhsBVX1m"  // #nosec G101 -- fixed placeholder bcrypt hash, see comment on recoveryCodeTimingEqualizationHash

type AuthUserRepository interface {
	ExistsByNormalizedEmail(ctx context.Context, email string) (bool, error)
	FindByNormalizedEmail(ctx context.Context, email string) (models.User, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
	FindByID(ctx context.Context, userID uint) (models.User, error)
	Create(ctx context.Context, user *models.User) error
	Save(ctx context.Context, user *models.User) error
	UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, recoveryHash string) error
	UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string, recoveryHash string) error
	BumpAuthSessionVersion(ctx context.Context, userID uint) error
}

const (
	DefaultLogoutAttemptsLimit  = 20
	DefaultLogoutAttemptsWindow = 15 * time.Minute
)

type AuthService struct {
	users               AuthUserRepository
	logoutAttemptPolicy *AuthAttemptPolicy
}

func NewAuthService(users AuthUserRepository) *AuthService {
	return &AuthService{
		users:               users,
		logoutAttemptPolicy: NewAuthAttemptPolicy("logout", nil, DefaultLogoutAttemptsLimit, DefaultLogoutAttemptsWindow),
	}
}

func (service *AuthService) ConfigureLogoutAttemptLimits(attempts int, window time.Duration) {
	service.logoutAttemptPolicy.Configure(attempts, window)
}

// CheckAndRecordLogoutAttempt returns true if the per-account logout rate limit is
// exceeded for this (clientKey, identity) pair. If not exceeded, it also records
// the attempt so subsequent calls count it toward the window.
func (service *AuthService) CheckAndRecordLogoutAttempt(secretKey []byte, clientKey string, identity string, now time.Time) bool {
	if service.logoutAttemptPolicy.TooManyRecent(secretKey, clientKey, identity, now) {
		return true
	}
	service.logoutAttemptPolicy.AddFailure(secretKey, clientKey, identity, now)
	return false
}

func (service *AuthService) RegistrationEmailExists(ctx context.Context, email string) (bool, error) {
	return service.users.ExistsByNormalizedEmail(ctx, email)
}

func (service *AuthService) CreateUser(ctx context.Context, user *models.User) error {
	return service.users.Create(ctx, user)
}

func (service *AuthService) FindByNormalizedEmail(ctx context.Context, email string) (models.User, error) {
	return service.users.FindByNormalizedEmail(ctx, email)
}

func (service *AuthService) FindByID(ctx context.Context, userID uint) (models.User, error) {
	return service.users.FindByID(ctx, userID)
}

func (service *AuthService) ValidateRegistrationCredentials(password string, confirmPassword string) error {
	password = strings.TrimSpace(password)
	confirmPassword = strings.TrimSpace(confirmPassword)

	if password == "" || confirmPassword == "" {
		return ErrAuthRegisterInvalid
	}
	if password != confirmPassword {
		return ErrAuthPasswordMismatch
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return ErrAuthWeakPassword
	}
	return nil
}

func (service *AuthService) RegisterOwner(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error) {
	if err := service.ValidateRegistrationCredentials(rawPassword, confirmPassword); err != nil {
		return models.User{}, "", err
	}

	exists, err := service.RegistrationEmailExists(ctx, email)
	if err != nil {
		return models.User{}, "", ErrAuthRegisterFailed
	}
	if exists {
		// Spend the same bcrypt time as the new-account branch so a duplicate-email probe
		// cannot be distinguished from a fresh email by POST /api/v1/users response
		// latency. BuildOwnerUserWithRecovery runs two bcrypt operations (password hash +
		// recovery code hash), so equalize against both placeholders.
		equalizeRegistrationTiming(rawPassword)
		return models.User{}, "", ErrAuthEmailExists
	}

	user, recoveryCode, err := service.BuildOwnerUserWithRecovery(email, rawPassword, createdAt)
	if err != nil {
		return models.User{}, "", ErrAuthRegisterFailed
	}

	return user, recoveryCode, nil
}

func (service *AuthService) ValidateResetPasswordInput(password string, confirmPassword string) error {
	password = strings.TrimSpace(password)
	confirmPassword = strings.TrimSpace(confirmPassword)

	if password == "" || confirmPassword == "" {
		return ErrAuthResetInvalid
	}
	if password != confirmPassword {
		return ErrAuthPasswordMismatch
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return ErrAuthWeakPassword
	}
	return nil
}

func (service *AuthService) ForceResetPasswordByEmail(ctx context.Context, email string, newPassword string) error {
	normalizedEmail := NormalizeAuthEmail(email)
	newPassword = strings.TrimSpace(newPassword)

	if normalizedEmail == "" || newPassword == "" {
		return ErrAuthResetInvalid
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return ErrAuthWeakPassword
	}

	exists, err := service.users.ExistsByNormalizedEmail(ctx, normalizedEmail)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUserLookupFailed, err)
	}
	if !exists {
		return ErrAuthUserNotFound
	}

	user, err := service.users.FindByNormalizedEmail(ctx, normalizedEmail)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUserLookupFailed, err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthPasswordHash, err)
	}

	if err := service.users.UpdatePasswordAndRevokeSessions(ctx, user.ID, string(passwordHash), true); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthPasswordUpdate, err)
	}

	return nil
}

func (service *AuthService) BuildOwnerUserWithRecovery(email string, rawPassword string, createdAt time.Time) (models.User, string, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(rawPassword), bcrypt.DefaultCost)
	if err != nil {
		return models.User{}, "", err
	}
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return models.User{}, "", err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	user := models.User{
		Email:              email,
		PasswordHash:       string(passwordHash),
		RecoveryCodeHash:   recoveryHash,
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          createdAt,
	}
	return user, recoveryCode, nil
}

func (service *AuthService) BuildOIDCOwnerUser(email string, createdAt time.Time) (models.User, error) {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return models.User{
		Email:              email,
		PasswordHash:       "",
		RecoveryCodeHash:   "",
		LocalAuthEnabled:   false,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          createdAt,
	}, nil
}

func (service *AuthService) AuthenticateCredentials(ctx context.Context, email string, password string) (models.User, error) {
	user, err := service.users.FindByNormalizedEmail(ctx, email)
	if err != nil {
		equalizeAuthCredentialsTiming(password)
		return models.User{}, ErrAuthInvalidCreds
	}
	if !user.LocalAuthEnabled || strings.TrimSpace(user.PasswordHash) == "" {
		equalizeAuthCredentialsTiming(password)
		return models.User{}, ErrAuthInvalidCreds
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return models.User{}, ErrAuthInvalidCreds
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (service *AuthService) FindUserByEmailAndRecoveryCode(ctx context.Context, email string, code string) (*models.User, error) {
	normalizedEmail := NormalizeAuthEmail(email)
	if normalizedEmail == "" {
		return nil, ErrRecoveryCodeNotFound
	}
	user, found, err := service.users.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if err != nil {
		return nil, err
	}
	if !found {
		equalizeRecoveryCodeLookupTiming(code)
		return nil, ErrRecoveryCodeNotFound
	}

	hash := strings.TrimSpace(user.RecoveryCodeHash)
	if !user.LocalAuthEnabled || hash == "" {
		equalizeRecoveryCodeLookupTiming(code)
		return nil, ErrRecoveryCodeNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(NormalizeRecoveryCode(code))) != nil {
		return nil, ErrRecoveryCodeNotFound
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, ErrRecoveryCodeNotFound
	}
	return &user, nil
}

func (service *AuthService) BuildPasswordResetToken(secretKey []byte, userID uint, passwordHash string, ttl time.Duration, now time.Time) (string, error) {
	return BuildPasswordResetToken(secretKey, userID, passwordHash, ttl, now)
}

func (service *AuthService) BuildAuthSessionToken(secretKey []byte, userID uint, role string, sessionVersion int, ttl time.Duration, now time.Time) (string, error) {
	return BuildAuthSessionTokenWithVersion(secretKey, userID, role, sessionVersion, ttl, now)
}

func (service *AuthService) BuildAuthSessionTokenWithSessionID(secretKey []byte, userID uint, role string, sessionVersion int, ttl time.Duration, now time.Time) (string, string, error) {
	return BuildAuthSessionTokenWithVersionAndSessionID(secretKey, userID, role, sessionVersion, ttl, now)
}

func (service *AuthService) ResolveUserByAuthSessionToken(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	user, _, err := service.ResolveAuthSession(ctx, secretKey, rawToken, now)
	return user, err
}

func (service *AuthService) ResolveAuthSession(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, *AuthSessionClaims, error) {
	claims, err := ParseAuthSessionToken(secretKey, rawToken, now)
	if err != nil {
		return nil, nil, err
	}

	user, err := service.users.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, nil, ErrAuthInvalidCreds
	}
	if user.MustChangePassword {
		return nil, nil, ErrAuthSessionTokenRevoked
	}
	if NormalizeAuthSessionVersion(claims.SessionVersion) != NormalizeAuthSessionVersion(user.AuthSessionVersion) {
		return nil, nil, ErrAuthSessionTokenRevoked
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, nil, err
	}
	return &user, claims, nil
}

func (service *AuthService) ResolveUserByResetToken(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	claims, err := ParsePasswordResetToken(secretKey, rawToken, now)
	if err != nil {
		return nil, ErrInvalidResetToken
	}

	user, err := service.users.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, ErrInvalidResetToken
	}
	if !user.LocalAuthEnabled || strings.TrimSpace(user.PasswordHash) == "" {
		return nil, ErrInvalidResetToken
	}
	if !IsPasswordStateFingerprintMatch(claims.PasswordState, user.PasswordHash) {
		return nil, ErrInvalidResetToken
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, ErrInvalidResetToken
	}
	return &user, nil
}

func (service *AuthService) RegenerateRecoveryCode(ctx context.Context, userID uint) (string, error) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeGenerate, err)
	}
	if err := service.users.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, recoveryHash); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeUpdate, err)
	}
	return recoveryCode, nil
}

func (service *AuthService) RevokeAuthSessions(ctx context.Context, userID uint) error {
	if userID == 0 {
		return ErrAuthUserRequired
	}
	return service.users.BumpAuthSessionVersion(ctx, userID)
}

func equalizeRecoveryCodeLookupTiming(code string) {
	_ = bcrypt.CompareHashAndPassword([]byte(recoveryCodeTimingEqualizationHash), []byte(NormalizeRecoveryCode(code)))
}

// equalizeAuthCredentialsTiming runs a bcrypt comparison against a fixed
// placeholder hash so AuthenticateCredentials spends comparable time on every
// path. Without it, the early "user not found" / "local auth disabled" returns
// short-circuit before any bcrypt work and leak account existence through
// response timing (CWE-208 / CWE-204).
//
// Declared as a var so tests can replace it with an invocation counter,
// asserting "bcrypt was called" without measuring wall-clock time (which is
// flake-prone on shared CI runners). Production code never reassigns this.
var equalizeAuthCredentialsTiming = func(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(password))
}

// equalizeRegistrationTiming mirrors the bcrypt work BuildOwnerUserWithRecovery
// performs on a fresh registration (password hash + recovery-code hash) so the
// duplicate-email branch of RegisterOwner spends comparable time and an attacker
// cannot tell a new email from an existing one through POST /api/v1/users
// response latency. Declared as a var for the same test-substitution reason as
// equalizeAuthCredentialsTiming.
var equalizeRegistrationTiming = func(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(password))
	_ = bcrypt.CompareHashAndPassword([]byte(recoveryCodeTimingEqualizationHash), []byte(password))
}

// ResetPasswordAndRotateRecoveryCode applies the new password and a fresh
// recovery code using an unconditional UPDATE. Kept for backward compatibility
// with callers that do not have the old password hash at hand (e.g. the
// FinalizeLocalPasswordSetup path). For the password-reset flow use
// ResetPasswordAndRotateRecoveryCodeCAS which adds a single-use CAS guard.
func (service *AuthService) ResetPasswordAndRotateRecoveryCode(ctx context.Context, user *models.User, newPassword string) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", err
	}

	if err := service.users.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, user.ID, string(passwordHash), recoveryHash, false); err != nil {
		return "", err
	}
	user.PasswordHash = string(passwordHash)
	user.RecoveryCodeHash = recoveryHash
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.MustChangePassword = false

	return recoveryCode, nil
}

// ResetPasswordAndRotateRecoveryCodeCAS is the single-use variant of
// ResetPasswordAndRotateRecoveryCode used by the password-reset flow.
// oldPasswordHash must be the hash that was current when the reset token was
// issued (sourced from the resolved user before any write).
//
// The UPDATE carries the predicate
// `WHERE id = ? AND password_hash = oldPasswordHash`. Concurrent or replayed
// redeems both reach the UPDATE, but only one sees RowsAffected == 1; the
// loser receives ErrResetTokenAlreadyConsumed.
func (service *AuthService) ResetPasswordAndRotateRecoveryCodeCAS(ctx context.Context, user *models.User, oldPasswordHash string, newPassword string) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired // codecov:ignore -- defensive; callers always pass a resolved user
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err // codecov:ignore -- bcrypt only errors on an out-of-range cost
	}
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", err // codecov:ignore -- crypto/rand failure, not reachable in tests
	}

	// CAS predicate prevents concurrent / replayed redeems.
	if err := service.users.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, oldPasswordHash, string(passwordHash), recoveryHash); err != nil {
		return "", err
	}
	user.PasswordHash = string(passwordHash)
	user.RecoveryCodeHash = recoveryHash
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.MustChangePassword = false

	return recoveryCode, nil
}
