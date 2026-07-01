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
	createErr       error
	createWasCalled bool
	createdUser     *models.User
	createdSymptoms []models.SymptomType
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

func (stub *stubOperatorUserRepo) CreateUserWithSymptoms(_ context.Context, user *models.User, symptoms []models.SymptomType) error {
	stub.createWasCalled = true
	stub.createdUser = user
	stub.createdSymptoms = symptoms
	if stub.createErr != nil {
		return stub.createErr
	}
	user.ID = 1
	return nil
}

type stubOwnerBuilder struct {
	user models.User
	code string
	err  error
}

func (stub *stubOwnerBuilder) BuildOwnerUserWithRecovery(email string, _ string, _ time.Time) (models.User, string, error) {
	if stub.err != nil {
		return models.User{}, "", stub.err
	}
	user := stub.user
	user.Email = email
	return user, stub.code, nil
}

type fakeUniqueConstraintError struct{}

func (fakeUniqueConstraintError) Error() string            { return "UNIQUE constraint failed: users.email" }
func (fakeUniqueConstraintError) UniqueConstraint() string { return "users.email" }

func TestOperatorUserServiceListUsers(t *testing.T) {
	t.Parallel()

	service := NewOperatorUserService(&stubOperatorUserRepo{
		listUsers: []models.OperatorUserSummary{
			{ID: 1, Email: "owner@example.com", Role: models.RoleOwner},
		},
	}, nil)

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
	}, nil)

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
	service := NewOperatorUserService(repo, nil)

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
			service := NewOperatorUserService(testCase.repo, nil)
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

func TestOperatorUserServiceCreateOwnerProvisionsOwnerWithSymptoms(t *testing.T) {
	t.Parallel()

	repo := &stubOperatorUserRepo{}
	builder := &stubOwnerBuilder{
		user: models.User{Role: models.RoleOwner, AuthSessionVersion: 1},
		code: "OVUM-AAAA-BBBB-CCCC",
	}
	service := NewOperatorUserService(repo, builder)

	summary, recoveryCode, err := service.CreateOwner(context.Background(), " Owner@Example.com ", "StrongPass1", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateOwner() unexpected error: %v", err)
	}
	if !repo.createWasCalled {
		t.Fatal("expected CreateUserWithSymptoms to be called")
	}
	if recoveryCode != "OVUM-AAAA-BBBB-CCCC" {
		t.Fatalf("expected recovery code to be returned, got %q", recoveryCode)
	}
	if summary.Email != "owner@example.com" {
		t.Fatalf("expected normalized email, got %q", summary.Email)
	}
	if len(repo.createdSymptoms) == 0 {
		t.Fatal("expected built-in symptoms to be seeded")
	}
	if repo.createdUser == nil || repo.createdUser.Role != models.RoleOwner {
		t.Fatalf("expected owner role on created user, got %#v", repo.createdUser)
	}
}

func TestOperatorUserServiceCreateOwnerAllowsMultipleOwners(t *testing.T) {
	t.Parallel()

	// Household self-hosting: creating a second independent owner is allowed.
	// Only a duplicate email is rejected (by the unique index — see next test).
	repo := &stubOperatorUserRepo{}
	builder := &stubOwnerBuilder{user: models.User{Role: models.RoleOwner}, code: "OVUM-DDDD-EEEE-FFFF"}
	service := NewOperatorUserService(repo, builder)

	summary, _, err := service.CreateOwner(context.Background(), "daughter@example.com", "StrongPass1", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateOwner() unexpected error for a second owner: %v", err)
	}
	if !repo.createWasCalled {
		t.Fatal("expected the second owner account to be created")
	}
	if summary.Email != "daughter@example.com" {
		t.Fatalf("expected normalized email, got %q", summary.Email)
	}
}

func TestOperatorUserServiceCreateOwnerMapsDuplicateEmail(t *testing.T) {
	t.Parallel()

	repo := &stubOperatorUserRepo{createErr: fakeUniqueConstraintError{}}
	builder := &stubOwnerBuilder{user: models.User{Role: models.RoleOwner}, code: "OVUM-AAAA-BBBB-CCCC"}
	service := NewOperatorUserService(repo, builder)

	_, _, err := service.CreateOwner(context.Background(), "owner@example.com", "StrongPass1", time.Now().UTC())
	if !errors.Is(err, ErrOperatorUserEmailExists) {
		t.Fatalf("expected ErrOperatorUserEmailExists from a duplicate email, got %v", err)
	}
}

func TestOperatorUserServiceCreateOwnerWrapsBuilderError(t *testing.T) {
	t.Parallel()

	repo := &stubOperatorUserRepo{}
	builder := &stubOwnerBuilder{err: errors.New("hash failure")}
	service := NewOperatorUserService(repo, builder)

	_, _, err := service.CreateOwner(context.Background(), "owner@example.com", "StrongPass1", time.Now().UTC())
	if !errors.Is(err, ErrOperatorUserCreateFailed) {
		t.Fatalf("expected ErrOperatorUserCreateFailed from builder error, got %v", err)
	}
	if repo.createWasCalled {
		t.Fatal("expected no persistence when the builder fails")
	}
}

func TestOperatorUserServiceCreateOwnerWrapsPersistenceError(t *testing.T) {
	t.Parallel()

	repo := &stubOperatorUserRepo{createErr: errors.New("db unavailable")}
	builder := &stubOwnerBuilder{user: models.User{Role: models.RoleOwner}, code: "OVUM-AAAA-BBBB-CCCC"}
	service := NewOperatorUserService(repo, builder)

	_, _, err := service.CreateOwner(context.Background(), "owner@example.com", "StrongPass1", time.Now().UTC())
	if !errors.Is(err, ErrOperatorUserCreateFailed) {
		t.Fatalf("expected ErrOperatorUserCreateFailed from a non-unique persistence error, got %v", err)
	}
}

func TestOperatorUserServiceCreateOwnerValidatesInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		email    string
		password string
		want     error
	}{
		{name: "invalid email", email: "not-an-email", password: "StrongPass1", want: ErrOperatorUserEmailInvalid},
		{name: "empty email", email: "  ", password: "StrongPass1", want: ErrOperatorUserEmailRequired},
		{name: "weak password", email: "owner@example.com", password: "weak", want: ErrOperatorUserPasswordWeak},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := &stubOperatorUserRepo{}
			service := NewOperatorUserService(repo, &stubOwnerBuilder{})
			_, _, err := service.CreateOwner(context.Background(), testCase.email, testCase.password, time.Now().UTC())
			if !errors.Is(err, testCase.want) {
				t.Fatalf("expected %v, got %v", testCase.want, err)
			}
			if repo.createWasCalled {
				t.Fatal("expected no creation on validation failure")
			}
		})
	}
}
