package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrOperatorUserEmailRequired = errors.New("operator user email is required")
	ErrOperatorUserEmailInvalid  = errors.New("operator user email is invalid")
	ErrOperatorUserNotFound      = errors.New("operator user not found")
	ErrOperatorUserListFailed    = errors.New("operator user list failed")
	ErrOperatorUserLookupFailed  = errors.New("operator user lookup failed")
	ErrOperatorUserDeleteFailed  = errors.New("operator user delete failed")
	ErrOperatorUserPasswordWeak  = errors.New("operator user password too weak")
	ErrOperatorUserCreateFailed  = errors.New("operator user create failed")
	ErrOperatorUserEmailExists   = errors.New("operator user email already exists")
)

type OperatorUserRepository interface {
	ListOperatorUserSummaries(ctx context.Context) ([]models.OperatorUserSummary, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
	DeleteAccountAndRelatedData(ctx context.Context, userID uint) error
	CreateUserWithSymptoms(ctx context.Context, user *models.User, symptoms []models.SymptomType) error
}

// OperatorOwnerBuilder builds the owner account record (password hash, recovery
// code, role, session version, cycle defaults) shared with web registration, so
// a CLI-provisioned owner has the exact same shape as a browser-registered one.
type OperatorOwnerBuilder interface {
	BuildOwnerUserWithRecovery(email string, rawPassword string, createdAt time.Time) (models.User, string, error)
}

type OperatorUserService struct {
	users   OperatorUserRepository
	builder OperatorOwnerBuilder
}

func NewOperatorUserService(users OperatorUserRepository, builder OperatorOwnerBuilder) *OperatorUserService {
	return &OperatorUserService{users: users, builder: builder}
}

func (service *OperatorUserService) ListUsers(ctx context.Context) ([]models.OperatorUserSummary, error) {
	users, err := service.users.ListOperatorUserSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOperatorUserListFailed, err)
	}
	return users, nil
}

func (service *OperatorUserService) GetUserByEmail(ctx context.Context, email string) (models.OperatorUserSummary, error) {
	normalizedEmail, err := normalizeOperatorUserEmail(email)
	if err != nil {
		return models.OperatorUserSummary{}, err
	}

	user, found, lookupErr := service.users.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if lookupErr != nil {
		return models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserLookupFailed, lookupErr)
	}
	if !found {
		return models.OperatorUserSummary{}, ErrOperatorUserNotFound
	}

	return operatorUserSummaryFromUser(user), nil
}

func (service *OperatorUserService) DeleteUserByEmail(ctx context.Context, email string) (models.OperatorUserSummary, error) {
	userSummary, err := service.GetUserByEmail(ctx, email)
	if err != nil {
		return models.OperatorUserSummary{}, err
	}

	if deleteErr := service.users.DeleteAccountAndRelatedData(ctx, userSummary.ID); deleteErr != nil {
		return models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserDeleteFailed, deleteErr)
	}

	return userSummary, nil
}

// CreateOwner provisions an owner account from the operator CLI. It mirrors the
// web registration shape (bcrypt password hash, recovery code, role,
// AuthSessionVersion, cycle defaults, built-in symptoms) but bypasses the
// registration-mode gate because it is a local operator action, not a public
// surface. Multiple independent owners may coexist on one instance (household
// self-hosting); the only uniqueness constraint is the email address, and each
// owner's data stays isolated by user_id. The onboarding baseline (last period
// start, cycle defaults) is intentionally left for the owner to complete on
// first sign-in — health data must not flow through provisioning. The recovery
// code is returned for the caller to surface; the CLI prints it only on explicit
// opt-in so it cannot leak into install logs.
func (service *OperatorUserService) CreateOwner(ctx context.Context, email string, rawPassword string, now time.Time) (models.OperatorUserSummary, string, error) {
	normalizedEmail, err := normalizeOperatorUserEmail(email)
	if err != nil {
		return models.OperatorUserSummary{}, "", err
	}
	if err := ValidatePasswordStrength(rawPassword); err != nil {
		return models.OperatorUserSummary{}, "", ErrOperatorUserPasswordWeak
	}

	user, recoveryCode, err := service.builder.BuildOwnerUserWithRecovery(normalizedEmail, rawPassword, now)
	if err != nil {
		return models.OperatorUserSummary{}, "", fmt.Errorf("%w: %v", ErrOperatorUserCreateFailed, err)
	}

	// The unique email index is the authoritative guard: a duplicate address —
	// from a retry or a concurrent create — is rejected here. Account count is
	// intentionally not gated, so a household can hold several owners.
	if err := service.users.CreateUserWithSymptoms(ctx, &user, BuiltinSymptomRecordsForUser(0)); err != nil {
		if isRegistrationUniqueConstraintError(err) {
			return models.OperatorUserSummary{}, "", ErrOperatorUserEmailExists
		}
		return models.OperatorUserSummary{}, "", fmt.Errorf("%w: %v", ErrOperatorUserCreateFailed, err)
	}

	return operatorUserSummaryFromUser(user), recoveryCode, nil
}

func normalizeOperatorUserEmail(email string) (string, error) {
	trimmedRaw := strings.TrimSpace(email)
	if trimmedRaw == "" {
		return "", ErrOperatorUserEmailRequired
	}

	normalized := NormalizeAuthEmail(trimmedRaw)
	if normalized == "" {
		return "", ErrOperatorUserEmailInvalid
	}
	return normalized, nil
}

func operatorUserSummaryFromUser(user models.User) models.OperatorUserSummary {
	return models.OperatorUserSummary{
		ID:                  user.ID,
		DisplayName:         user.DisplayName,
		Email:               user.Email,
		Role:                user.Role,
		OnboardingCompleted: user.OnboardingCompleted,
		CreatedAt:           user.CreatedAt,
	}
}
