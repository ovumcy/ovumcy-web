package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubOperatorUserRepo struct {
	listUsers       []models.OperatorUserSummary
	listErr         error
	user            models.User
	found           bool
	findErr         error
	deleteErr       error
	deletedUserID   uint
	deleteWasCalled bool
}

func (stub *stubOperatorUserRepo) ListOperatorUserSummaries(context.Context) ([]models.OperatorUserSummary, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	return stub.listUsers, nil
}

func (stub *stubOperatorUserRepo) FindByNormalizedEmailOptional(context.Context, string) (models.User, bool, error) {
	if stub.findErr != nil {
		return models.User{}, false, stub.findErr
	}
	return stub.user, stub.found, nil
}

func (stub *stubOperatorUserRepo) DeleteAccountAndRelatedData(ctx context.Context, userID uint) error {
	stub.deleteWasCalled = true
	stub.deletedUserID = userID
	return stub.deleteErr
}

func TestOperatorUserServiceListUsers(t *testing.T) {
	t.Parallel()

	service := NewOperatorUserService(&stubOperatorUserRepo{
		listUsers: []models.OperatorUserSummary{
			{ID: 1, Email: "owner@example.com", Role: models.RoleOwner},
		},
	})

	users, err := service.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() unexpected error: %v", err)
	}
	if len(users) != 1 || users[0].Email != "owner@example.com" {
		t.Fatalf("expected list to contain owner@example.com, got %#v", users)
	}
}

func TestOperatorUserServiceGetUserByEmailNormalizesInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC)
	service := NewOperatorUserService(&stubOperatorUserRepo{
		user: models.User{
			ID:                  7,
			DisplayName:         "Owner",
			Email:               "owner@example.com",
			Role:                models.RoleOwner,
			OnboardingCompleted: true,
			CreatedAt:           now,
		},
		found: true,
	})

	user, err := service.GetUserByEmail(context.Background(), " Owner@Example.com ")
	if err != nil {
		t.Fatalf("GetUserByEmail() unexpected error: %v", err)
	}
	if user.ID != 7 || user.Email != "owner@example.com" || user.DisplayName != "Owner" {
		t.Fatalf("unexpected user summary: %#v", user)
	}
}

func TestOperatorUserServiceDeleteUserByEmail(t *testing.T) {
	t.Parallel()

	repo := &stubOperatorUserRepo{
		user:  models.User{ID: 9, Email: "owner@example.com", Role: models.RoleOwner},
		found: true,
	}
	service := NewOperatorUserService(repo)

	user, err := service.DeleteUserByEmail(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("DeleteUserByEmail() unexpected error: %v", err)
	}
	if !repo.deleteWasCalled || repo.deletedUserID != 9 {
		t.Fatalf("expected delete for user id 9, got called=%t id=%d", repo.deleteWasCalled, repo.deletedUserID)
	}
	if user.ID != 9 || user.Email != "owner@example.com" {
		t.Fatalf("unexpected deleted user summary: %#v", user)
	}
}

func TestOperatorUserServiceDeleteUserByEmailErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		repo *stubOperatorUserRepo
		err  error
	}{
		{
			name: "invalid email",
			repo: &stubOperatorUserRepo{},
			err:  ErrOperatorUserEmailInvalid,
		},
		{
			name: "not found",
			repo: &stubOperatorUserRepo{},
			err:  ErrOperatorUserNotFound,
		},
		{
			name: "delete failed",
			repo: &stubOperatorUserRepo{
				user:      models.User{ID: 10, Email: "owner@example.com"},
				found:     true,
				deleteErr: errors.New("db down"),
			},
			err: ErrOperatorUserDeleteFailed,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			service := NewOperatorUserService(testCase.repo)
			_, err := service.DeleteUserByEmail(context.Background(), "not-an-email")
			if testCase.name != "invalid email" {
				_, err = service.DeleteUserByEmail(context.Background(), "owner@example.com")
			}
			if !errors.Is(err, testCase.err) {
				t.Fatalf("expected error %v, got %v", testCase.err, err)
			}
		})
	}
}
