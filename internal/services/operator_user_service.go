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
	ErrOperatorUserEmailRequired  = errors.New("operator user email is required")
	ErrOperatorUserEmailInvalid   = errors.New("operator user email is invalid")
	ErrOperatorUserNotFound       = errors.New("operator user not found")
	ErrOperatorUserListFailed     = errors.New("operator user list failed")
	ErrOperatorUserLookupFailed   = errors.New("operator user lookup failed")
	ErrOperatorUserDeleteFailed   = errors.New("operator user delete failed")
	ErrOperatorUserPasswordWeak   = errors.New("operator user password too weak")
	ErrOperatorUserCreateFailed   = errors.New("operator user create failed")
	ErrOperatorUserEmailExists    = errors.New("operator user email already exists")
	ErrOperatorUserIDRequired     = errors.New("operator user id is required")
	ErrOperatorUserSetEmailFailed = errors.New("operator user set-email failed")
	// ErrOperatorUserChangedUnderRepair is the compare-and-set refusal: the row
	// no longer carries the address the operator was shown, so the repair would
	// be overwriting a change it never saw.
	ErrOperatorUserChangedUnderRepair = errors.New("operator user changed under repair")
)

type OperatorUserRepository interface {
	ListOperatorUserSummaries(ctx context.Context) ([]models.OperatorUserSummary, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
	FindByIDOptional(ctx context.Context, userID uint) (models.User, bool, error)
	ExistsByNormalizedEmailExcludingUser(ctx context.Context, email string, excludeUserID uint) (bool, error)
	SetUserEmailByIDAndRevokeSessions(ctx context.Context, userID uint, fromEmail string, toEmail string) (bool, error)
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

// GetUserByID addresses an account by the id printed by `users list`, which is
// the only handle that reaches EVERY row. An address does not: a row the strict
// NormalizeAuthEmail rule refuses — the decorated legacy forms the boot repair
// leaves standing (AuthEmailRenormalizer) — cannot be passed to any
// email-taking command at all, and its bare address resolves a DIFFERENT
// account, the one that won the repair.
func (service *OperatorUserService) GetUserByID(ctx context.Context, userID uint) (models.OperatorUserSummary, error) {
	if userID == 0 {
		return models.OperatorUserSummary{}, ErrOperatorUserIDRequired
	}

	user, found, err := service.users.FindByIDOptional(ctx, userID)
	if err != nil {
		return models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserLookupFailed, err)
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

	return service.deleteUser(ctx, userSummary)
}

func (service *OperatorUserService) DeleteUserByID(ctx context.Context, userID uint) (models.OperatorUserSummary, error) {
	userSummary, err := service.GetUserByID(ctx, userID)
	if err != nil {
		return models.OperatorUserSummary{}, err
	}

	return service.deleteUser(ctx, userSummary)
}

func (service *OperatorUserService) deleteUser(ctx context.Context, userSummary models.OperatorUserSummary) (models.OperatorUserSummary, error) {
	if deleteErr := service.users.DeleteAccountAndRelatedData(ctx, userSummary.ID); deleteErr != nil {
		return models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserDeleteFailed, deleteErr)
	}

	return userSummary, nil
}

// SetEmailByID re-homes one account to a new address. It is the repair path for
// an account the boot-time email renormalizer had to leave locked out — a
// duplicate mailbox or a value it could not reduce to an addr-spec — and it
// exists because deleting such a row is not a repair: the account's whole
// health history goes with it.
//
// The new address is validated by the SAME strict rule a later login input is
// normalized under, so the repair cannot store an identity no sign-in can
// reproduce. Uniqueness is checked here for a legible refusal and decided by
// the unique index underneath, which is what makes a concurrent create safe.
// The write bumps auth_session_version: the address is the login identity.
//
// Both summaries are returned — the row as it stood and as it now stands — so
// the caller can show the operator exactly which account moved and from what.
func (service *OperatorUserService) SetEmailByID(ctx context.Context, userID uint, email string) (models.OperatorUserSummary, models.OperatorUserSummary, error) {
	before, err := service.GetUserByID(ctx, userID)
	if err != nil {
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, err
	}

	normalizedEmail, err := normalizeOperatorUserEmail(email)
	if err != nil {
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, err
	}

	// A re-run with the address the row already carries is a no-op, not a
	// repair, and it must not reach the write: that write bumps
	// auth_session_version, so a scripted retry would sign the owner out of the
	// session they had just signed into. Byte equality, deliberately: a stored
	// value that only differs in case IS a repair and must go through.
	if normalizedEmail == before.Email {
		return before, before, nil
	}

	taken, err := service.users.ExistsByNormalizedEmailExcludingUser(ctx, normalizedEmail, userID)
	if err != nil {
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserLookupFailed, err)
	}
	if taken {
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, ErrOperatorUserEmailExists
	}

	changed, err := service.users.SetUserEmailByIDAndRevokeSessions(ctx, userID, before.Email, normalizedEmail)
	if err != nil {
		if isRegistrationUniqueConstraintError(err) {
			return models.OperatorUserSummary{}, models.OperatorUserSummary{}, ErrOperatorUserEmailExists
		}
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, fmt.Errorf("%w: %v", ErrOperatorUserSetEmailFailed, err)
	}
	if !changed {
		return models.OperatorUserSummary{}, models.OperatorUserSummary{}, ErrOperatorUserChangedUnderRepair
	}

	after := before
	after.Email = normalizedEmail
	return before, after, nil
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
