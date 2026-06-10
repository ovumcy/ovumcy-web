package services

import "context"

type SetupUserRepository interface {
	CountUsers(ctx context.Context) (int64, error)
}

type SetupService struct {
	users SetupUserRepository
}

func NewSetupService(users SetupUserRepository) *SetupService {
	return &SetupService{users: users}
}

func (service *SetupService) RequiresInitialSetup(ctx context.Context) (bool, error) {
	usersCount, err := service.users.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return usersCount == 0, nil
}
