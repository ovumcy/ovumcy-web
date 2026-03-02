package services

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var ErrRegistrationSeedSymptoms = errors.New("registration seed symptoms failed")

type RegistrationAuthService interface {
	RegisterOwner(email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error)
}

type RegistrationSymptomSeeder interface {
	SeedBuiltinSymptoms(userID uint) error
}

type RegistrationService struct {
	auth     RegistrationAuthService
	symptoms RegistrationSymptomSeeder
}

func NewRegistrationService(auth RegistrationAuthService, symptoms RegistrationSymptomSeeder) *RegistrationService {
	return &RegistrationService{
		auth:     auth,
		symptoms: symptoms,
	}
}

func (service *RegistrationService) RegisterOwnerAccount(email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error) {
	user, recoveryCode, err := service.auth.RegisterOwner(email, rawPassword, confirmPassword, createdAt)
	if err != nil {
		return models.User{}, "", err
	}
	if err := service.symptoms.SeedBuiltinSymptoms(user.ID); err != nil {
		return models.User{}, "", ErrRegistrationSeedSymptoms
	}
	return user, recoveryCode, nil
}
