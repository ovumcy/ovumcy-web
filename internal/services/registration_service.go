package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var ErrRegistrationSeedSymptoms = errors.New("registration seed symptoms failed")

type RegistrationAuthService interface {
	RegisterOwner(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error)
	BuildOIDCOwnerUser(email string, createdAt time.Time) (models.User, error)
}

type RegistrationPersistence interface {
	CreateUserWithSymptoms(ctx context.Context, user *models.User, symptoms []models.SymptomType) error
}

type RegistrationService struct {
	auth  RegistrationAuthService
	store RegistrationPersistence
	mode  RegistrationMode
}

func NewRegistrationService(auth RegistrationAuthService, store RegistrationPersistence, mode RegistrationMode) *RegistrationService {
	return &RegistrationService{
		auth:  auth,
		store: store,
		mode:  mode,
	}
}

func (service *RegistrationService) RegistrationOpen() bool {
	return service.mode.IsOpen()
}

func (service *RegistrationService) RegisterOwnerAccount(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error) {
	if !service.RegistrationOpen() {
		return models.User{}, "", ErrAuthRegistrationDisabled
	}

	user, recoveryCode, err := service.auth.RegisterOwner(ctx, email, rawPassword, confirmPassword, createdAt)
	if err != nil {
		return models.User{}, "", err
	}

	if err := service.store.CreateUserWithSymptoms(ctx, &user, BuiltinSymptomRecordsForUser(0)); err != nil {
		if isRegistrationUniqueConstraintError(err) {
			return models.User{}, "", ErrAuthEmailExists
		}
		if isRegistrationSymptomSeedError(err) {
			return models.User{}, "", ErrRegistrationSeedSymptoms
		}
		return models.User{}, "", ErrAuthRegisterFailed
	}

	return user, recoveryCode, nil
}

func (service *RegistrationService) AutoProvisionOwnerAccount(ctx context.Context, email string, createdAt time.Time) (models.User, error) {
	if !service.RegistrationOpen() {
		return models.User{}, ErrAuthRegistrationDisabled
	}

	user, err := service.auth.BuildOIDCOwnerUser(email, createdAt)
	if err != nil {
		return models.User{}, ErrAuthRegisterFailed
	}

	if err := service.store.CreateUserWithSymptoms(ctx, &user, BuiltinSymptomRecordsForUser(0)); err != nil {
		if isRegistrationUniqueConstraintError(err) {
			return models.User{}, ErrAuthEmailExists
		}
		if isRegistrationSymptomSeedError(err) {
			return models.User{}, ErrRegistrationSeedSymptoms
		}
		return models.User{}, ErrAuthRegisterFailed
	}

	return user, nil
}

type registrationUniqueConstraintError interface {
	UniqueConstraint() string
}

func isRegistrationUniqueConstraintError(err error) bool {
	var uniqueErr registrationUniqueConstraintError
	return errors.As(err, &uniqueErr)
}

type registrationSymptomSeedError interface {
	SymptomSeedFailure() bool
}

func isRegistrationSymptomSeedError(err error) bool {
	var seedErr registrationSymptomSeedError
	return errors.As(err, &seedErr)
}
