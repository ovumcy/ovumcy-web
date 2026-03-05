package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrRecoveryCodeNotFound = errors.New("recovery code not found")
	ErrInvalidResetToken    = errors.New("invalid reset token")
	ErrAuthUserRequired     = errors.New("auth user is required")
	ErrAuthUserNotFound     = errors.New("auth user not found")
	ErrAuthUserLookupFailed = errors.New("auth user lookup failed")
	ErrRecoveryCodeGenerate = errors.New("recovery code generation failed")
	ErrRecoveryCodeUpdate   = errors.New("recovery code update failed")
	ErrAuthRegisterInvalid  = errors.New("auth register invalid input")
	ErrAuthEmailExists      = errors.New("auth email already exists")
	ErrAuthRegisterFailed   = errors.New("auth register failed")
	ErrAuthPasswordMismatch = errors.New("auth register password mismatch")
	ErrAuthWeakPassword     = errors.New("auth register weak password")
	ErrAuthInvalidCreds     = errors.New("auth invalid credentials")
	ErrAuthResetInvalid     = errors.New("auth reset invalid input")
	ErrAuthPasswordHash     = errors.New("auth password hash failed")
	ErrAuthPasswordUpdate   = errors.New("auth password update failed")
)

type AuthUserRepository interface {
	ExistsByNormalizedEmail(email string) (bool, error)
	FindByNormalizedEmail(email string) (models.User, error)
	FindByID(userID uint) (models.User, error)
	Create(user *models.User) error
	Save(user *models.User) error
	UpdateRecoveryCodeHash(userID uint, recoveryHash string) error
	ListWithRecoveryCodeHash() ([]models.User, error)
}

type AuthService struct {
	users AuthUserRepository
}

func NewAuthService(users AuthUserRepository) *AuthService {
	return &AuthService{users: users}
}

func (service *AuthService) RegistrationEmailExists(email string) (bool, error) {
	return service.users.ExistsByNormalizedEmail(email)
}

func (service *AuthService) CreateUser(user *models.User) error {
	return service.users.Create(user)
}

func (service *AuthService) FindByNormalizedEmail(email string) (models.User, error) {
	return service.users.FindByNormalizedEmail(email)
}

func (service *AuthService) FindByID(userID uint) (models.User, error) {
	return service.users.FindByID(userID)
}

func (service *AuthService) SaveUser(user *models.User) error {
	return service.users.Save(user)
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

func (service *AuthService) RegisterOwner(email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error) {
	if err := service.ValidateRegistrationCredentials(rawPassword, confirmPassword); err != nil {
		return models.User{}, "", err
	}

	exists, err := service.RegistrationEmailExists(email)
	if err != nil {
		return models.User{}, "", ErrAuthRegisterFailed
	}
	if exists {
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

func (service *AuthService) ForceResetPasswordByEmail(email string, newPassword string) error {
	normalizedEmail := NormalizeAuthEmail(email)
	newPassword = strings.TrimSpace(newPassword)

	if normalizedEmail == "" || newPassword == "" {
		return ErrAuthResetInvalid
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return ErrAuthWeakPassword
	}

	exists, err := service.users.ExistsByNormalizedEmail(normalizedEmail)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUserLookupFailed, err)
	}
	if !exists {
		return ErrAuthUserNotFound
	}

	user, err := service.users.FindByNormalizedEmail(normalizedEmail)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUserLookupFailed, err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthPasswordHash, err)
	}

	user.PasswordHash = string(passwordHash)
	user.MustChangePassword = true
	if err := service.users.Save(&user); err != nil {
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
		Email:            email,
		PasswordHash:     string(passwordHash),
		RecoveryCodeHash: recoveryHash,
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        createdAt,
	}
	return user, recoveryCode, nil
}

func (service *AuthService) AuthenticateCredentials(email string, password string) (models.User, error) {
	user, err := service.users.FindByNormalizedEmail(email)
	if err != nil {
		return models.User{}, ErrAuthInvalidCreds
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return models.User{}, ErrAuthInvalidCreds
	}
	return user, nil
}

func (service *AuthService) FindUserByRecoveryCode(code string) (*models.User, error) {
	return service.findUserByRecoveryCode(code, "")
}

func (service *AuthService) FindUserByEmailAndRecoveryCode(email string, code string) (*models.User, error) {
	normalizedEmail := NormalizeAuthEmail(email)
	if normalizedEmail == "" {
		return nil, ErrRecoveryCodeNotFound
	}
	return service.findUserByRecoveryCode(code, normalizedEmail)
}

func (service *AuthService) findUserByRecoveryCode(code string, normalizedEmail string) (*models.User, error) {
	users, err := service.users.ListWithRecoveryCodeHash()
	if err != nil {
		return nil, err
	}

	normalizedCode := NormalizeRecoveryCode(code)
	for index := range users {
		if normalizedEmail != "" && NormalizeAuthEmail(users[index].Email) != normalizedEmail {
			continue
		}
		hash := strings.TrimSpace(users[index].RecoveryCodeHash)
		if hash == "" {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(normalizedCode)) == nil {
			return &users[index], nil
		}
	}
	return nil, ErrRecoveryCodeNotFound
}

func (service *AuthService) BuildPasswordResetToken(secretKey []byte, userID uint, passwordHash string, ttl time.Duration, now time.Time) (string, error) {
	return BuildPasswordResetToken(secretKey, userID, passwordHash, ttl, now)
}

func (service *AuthService) BuildAuthSessionToken(secretKey []byte, userID uint, role string, ttl time.Duration, now time.Time) (string, error) {
	return BuildAuthSessionToken(secretKey, userID, role, ttl, now)
}

func (service *AuthService) ResolveUserByAuthSessionToken(secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	claims, err := ParseAuthSessionToken(secretKey, rawToken, now)
	if err != nil {
		return nil, err
	}

	user, err := service.users.FindByID(claims.UserID)
	if err != nil {
		return nil, ErrAuthInvalidCreds
	}
	if user.MustChangePassword {
		return nil, ErrAuthSessionTokenRevoked
	}
	return &user, nil
}

func (service *AuthService) ResolveUserByResetToken(secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	claims, err := ParsePasswordResetToken(secretKey, rawToken, now)
	if err != nil {
		return nil, ErrInvalidResetToken
	}

	user, err := service.users.FindByID(claims.UserID)
	if err != nil {
		return nil, ErrInvalidResetToken
	}
	if !IsPasswordStateFingerprintMatch(claims.PasswordState, user.PasswordHash) {
		return nil, ErrInvalidResetToken
	}
	return &user, nil
}

func (service *AuthService) GenerateRecoveryCodeHash() (string, string, error) {
	return GenerateRecoveryCodeHash()
}

func (service *AuthService) RegenerateRecoveryCode(userID uint) (string, error) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeGenerate, err)
	}
	if err := service.users.UpdateRecoveryCodeHash(userID, recoveryHash); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeUpdate, err)
	}
	return recoveryCode, nil
}

func (service *AuthService) ResetPasswordAndRotateRecoveryCode(user *models.User, newPassword string) (string, error) {
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

	user.PasswordHash = string(passwordHash)
	user.RecoveryCodeHash = recoveryHash
	user.MustChangePassword = false
	if err := service.users.Save(user); err != nil {
		return "", err
	}

	return recoveryCode, nil
}
