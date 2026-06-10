package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrOperatorUserEmailRequired = errors.New("operator user email is required")
	ErrOperatorUserEmailInvalid  = errors.New("operator user email is invalid")
	ErrOperatorUserNotFound      = errors.New("operator user not found")
	ErrOperatorUserListFailed    = errors.New("operator user list failed")
	ErrOperatorUserLookupFailed  = errors.New("operator user lookup failed")
	ErrOperatorUserDeleteFailed  = errors.New("operator user delete failed")
)

type OperatorUserRepository interface {
	ListOperatorUserSummaries(ctx context.Context) ([]models.OperatorUserSummary, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
	DeleteAccountAndRelatedData(ctx context.Context, userID uint) error
}

type OperatorUserService struct {
	users OperatorUserRepository
}

func NewOperatorUserService(users OperatorUserRepository) *OperatorUserService {
	return &OperatorUserService{users: users}
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
