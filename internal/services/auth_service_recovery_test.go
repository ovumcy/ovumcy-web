package services

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type stubAuthUserRepo struct {
	existsByEmail         bool
	existsByEmailErr      error
	findByEmailUser       models.User
	findByEmailErr        error
	user                  models.User
	findByIDErr           error
	createErr             error
	createCalled          bool
	createdUser           models.User
	saveErr               error
	updateRecoveryCodeErr error
	listErr               error
	listUsers             []models.User
	updatedUserID         uint
	updatedRecoveryHash   string
	saveCalled            bool
}

func (stub *stubAuthUserRepo) ExistsByNormalizedEmail(string) (bool, error) {
	if stub.existsByEmailErr != nil {
		return false, stub.existsByEmailErr
	}
	return stub.existsByEmail, nil
}

func (stub *stubAuthUserRepo) FindByNormalizedEmail(string) (models.User, error) {
	if stub.findByEmailErr != nil {
		return models.User{}, stub.findByEmailErr
	}
	return stub.findByEmailUser, nil
}

func (stub *stubAuthUserRepo) FindByID(uint) (models.User, error) {
	if stub.findByIDErr != nil {
		return models.User{}, stub.findByIDErr
	}
	return stub.user, nil
}

func (stub *stubAuthUserRepo) Create(user *models.User) error {
	if stub.createErr != nil {
		return stub.createErr
	}
	stub.createCalled = true
	stub.createdUser = *user
	return nil
}

func (stub *stubAuthUserRepo) Save(user *models.User) error {
	if stub.saveErr != nil {
		return stub.saveErr
	}
	stub.saveCalled = true
	stub.user = *user
	return nil
}

func (stub *stubAuthUserRepo) UpdateRecoveryCodeHash(userID uint, recoveryHash string) error {
	if stub.updateRecoveryCodeErr != nil {
		return stub.updateRecoveryCodeErr
	}
	stub.updatedUserID = userID
	stub.updatedRecoveryHash = recoveryHash
	return nil
}

func (stub *stubAuthUserRepo) ListWithRecoveryCodeHash() ([]models.User, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	if stub.listUsers != nil {
		return stub.listUsers, nil
	}
	return []models.User{stub.user}, nil
}

var serviceRecoveryCodePattern = regexp.MustCompile(`^OVUM-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}$`)

func TestAuthServiceValidateRegistrationCredentials(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{})

	if err := service.ValidateRegistrationCredentials("", ""); !errors.Is(err, ErrAuthRegisterInvalid) {
		t.Fatalf("expected ErrAuthRegisterInvalid for empty passwords, got %v", err)
	}
	if err := service.ValidateRegistrationCredentials("StrongPass1", "AnotherPass2"); !errors.Is(err, ErrAuthPasswordMismatch) {
		t.Fatalf("expected ErrAuthPasswordMismatch, got %v", err)
	}
	if err := service.ValidateRegistrationCredentials("12345678", "12345678"); !errors.Is(err, ErrAuthWeakPassword) {
		t.Fatalf("expected ErrAuthWeakPassword, got %v", err)
	}
	if err := service.ValidateRegistrationCredentials("StrongPass1", "StrongPass1"); err != nil {
		t.Fatalf("expected successful validation, got %v", err)
	}
}

func TestAuthServiceRegisterOwner(t *testing.T) {
	createdAt := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)

	t.Run("success", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		service := NewAuthService(repo)

		user, recoveryCode, err := service.RegisterOwner("owner@example.com", "StrongPass1", "StrongPass1", createdAt)
		if err != nil {
			t.Fatalf("RegisterOwner() unexpected error: %v", err)
		}
		if user.Email != "owner@example.com" {
			t.Fatalf("expected owner@example.com, got %q", user.Email)
		}
		if user.Role != models.RoleOwner {
			t.Fatalf("expected owner role, got %q", user.Role)
		}
		if !user.CreatedAt.Equal(createdAt) {
			t.Fatalf("expected createdAt %s, got %s", createdAt, user.CreatedAt)
		}
		if recoveryCode == "" {
			t.Fatalf("expected non-empty recovery code")
		}
		if repo.createCalled {
			t.Fatalf("did not expect Create() in RegisterOwner, persistence belongs to registration workflow")
		}
	})

	t.Run("validation mismatch", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		service := NewAuthService(repo)
		if _, _, err := service.RegisterOwner("owner@example.com", "StrongPass1", "WrongPass2", createdAt); !errors.Is(err, ErrAuthPasswordMismatch) {
			t.Fatalf("expected ErrAuthPasswordMismatch, got %v", err)
		}
	})

	t.Run("email exists", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmail: true}
		service := NewAuthService(repo)
		if _, _, err := service.RegisterOwner("owner@example.com", "StrongPass1", "StrongPass1", createdAt); !errors.Is(err, ErrAuthEmailExists) {
			t.Fatalf("expected ErrAuthEmailExists, got %v", err)
		}
		if repo.createCalled {
			t.Fatalf("did not expect Create() when email already exists")
		}
	})

	t.Run("exists check fails", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmailErr: errors.New("db down")}
		service := NewAuthService(repo)
		if _, _, err := service.RegisterOwner("owner@example.com", "StrongPass1", "StrongPass1", createdAt); !errors.Is(err, ErrAuthRegisterFailed) {
			t.Fatalf("expected ErrAuthRegisterFailed, got %v", err)
		}
	})

}

func TestAuthServiceValidateResetPasswordInput(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{})

	if err := service.ValidateResetPasswordInput("", ""); !errors.Is(err, ErrAuthResetInvalid) {
		t.Fatalf("expected ErrAuthResetInvalid for empty input, got %v", err)
	}
	if err := service.ValidateResetPasswordInput("StrongPass1", "AnotherPass2"); !errors.Is(err, ErrAuthPasswordMismatch) {
		t.Fatalf("expected ErrAuthPasswordMismatch, got %v", err)
	}
	if err := service.ValidateResetPasswordInput("12345678", "12345678"); !errors.Is(err, ErrAuthWeakPassword) {
		t.Fatalf("expected ErrAuthWeakPassword, got %v", err)
	}
	if err := service.ValidateResetPasswordInput("StrongPass1", "StrongPass1"); err != nil {
		t.Fatalf("expected valid reset password input, got %v", err)
	}
}

func TestAuthServiceForceResetPasswordByEmail(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash original password: %v", err)
		}

		repo := &stubAuthUserRepo{
			existsByEmail: true,
			findByEmailUser: models.User{
				ID:                 18,
				Email:              "owner@example.com",
				PasswordHash:       string(originalHash),
				MustChangePassword: false,
			},
		}
		service := NewAuthService(repo)

		if err := service.ForceResetPasswordByEmail(" Owner@Example.com ", "EvenStronger2"); err != nil {
			t.Fatalf("ForceResetPasswordByEmail() unexpected error: %v", err)
		}
		if !repo.saveCalled {
			t.Fatal("expected Save() to be called")
		}
		if !repo.user.MustChangePassword {
			t.Fatal("expected MustChangePassword=true after forced reset")
		}
		if bcrypt.CompareHashAndPassword([]byte(repo.user.PasswordHash), []byte("EvenStronger2")) != nil {
			t.Fatal("expected saved password hash to match new password")
		}
	})

	t.Run("missing password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByEmail("owner@example.com", " "); !errors.Is(err, ErrAuthResetInvalid) {
			t.Fatalf("expected ErrAuthResetInvalid, got %v", err)
		}
	})

	t.Run("weak password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByEmail("owner@example.com", "12345678"); !errors.Is(err, ErrAuthWeakPassword) {
			t.Fatalf("expected ErrAuthWeakPassword, got %v", err)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmail: false}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByEmail("missing@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthUserNotFound) {
			t.Fatalf("expected ErrAuthUserNotFound, got %v", err)
		}
		if repo.saveCalled {
			t.Fatal("did not expect Save() when user is missing")
		}
	})

	t.Run("lookup failure", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmailErr: errors.New("db down")}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByEmail("owner@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthUserLookupFailed) {
			t.Fatalf("expected ErrAuthUserLookupFailed, got %v", err)
		}
	})

	t.Run("save failure", func(t *testing.T) {
		originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash original password: %v", err)
		}

		repo := &stubAuthUserRepo{
			existsByEmail: true,
			findByEmailUser: models.User{
				ID:           18,
				Email:        "owner@example.com",
				PasswordHash: string(originalHash),
			},
			saveErr: errors.New("write failed"),
		}
		service := NewAuthService(repo)

		if err := service.ForceResetPasswordByEmail("owner@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthPasswordUpdate) {
			t.Fatalf("expected ErrAuthPasswordUpdate, got %v", err)
		}
	})
}

func TestAuthServiceBuildOwnerUserWithRecovery(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{})
	createdAt := time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)

	user, recoveryCode, err := service.BuildOwnerUserWithRecovery("owner@example.com", "StrongPass1", createdAt)
	if err != nil {
		t.Fatalf("BuildOwnerUserWithRecovery() unexpected error: %v", err)
	}
	if user.Email != "owner@example.com" {
		t.Fatalf("expected email owner@example.com, got %q", user.Email)
	}
	if user.Role != models.RoleOwner {
		t.Fatalf("expected owner role, got %q", user.Role)
	}
	if user.CycleLength != models.DefaultCycleLength || user.PeriodLength != models.DefaultPeriodLength {
		t.Fatalf("expected default cycle/period lengths, got %d/%d", user.CycleLength, user.PeriodLength)
	}
	if !user.AutoPeriodFill {
		t.Fatalf("expected AutoPeriodFill=true")
	}
	if !user.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected CreatedAt preserved, got %s", user.CreatedAt)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatalf("expected password hash for StrongPass1")
	}
	if !serviceRecoveryCodePattern.MatchString(recoveryCode) {
		t.Fatalf("expected recovery code format, got %q", recoveryCode)
	}
	if user.RecoveryCodeHash == "" {
		t.Fatalf("expected non-empty recovery hash")
	}
}

func TestAuthServiceAuthenticateCredentials(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailUser: models.User{
			ID:           77,
			Email:        "login@example.com",
			PasswordHash: string(passwordHash),
		},
	}
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials("login@example.com", "StrongPass1")
	if err != nil {
		t.Fatalf("AuthenticateCredentials() unexpected error: %v", err)
	}
	if user.ID != 77 {
		t.Fatalf("expected user id 77, got %d", user.ID)
	}

	if _, err := service.AuthenticateCredentials("login@example.com", "WrongPass2"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for wrong password, got %v", err)
	}

	repo.findByEmailErr = errors.New("user not found")
	if _, err := service.AuthenticateCredentials("missing@example.com", "StrongPass1"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for missing user, got %v", err)
	}
}

func TestAuthServiceFindUserByEmailAndRecoveryCode(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}

	repo := &stubAuthUserRepo{
		listUsers: []models.User{
			{
				ID:               22,
				Email:            "owner@example.com",
				RecoveryCodeHash: recoveryHash,
			},
		},
	}
	service := NewAuthService(repo)

	user, err := service.FindUserByEmailAndRecoveryCode("Owner@Example.com", recoveryCode)
	if err != nil {
		t.Fatalf("FindUserByEmailAndRecoveryCode() unexpected error: %v", err)
	}
	if user == nil || user.ID != 22 {
		t.Fatalf("expected user id 22, got %#v", user)
	}
}

func TestAuthServiceFindUserByEmailAndRecoveryCodeRejectsMismatch(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}

	repo := &stubAuthUserRepo{
		listUsers: []models.User{
			{
				ID:               22,
				Email:            "owner@example.com",
				RecoveryCodeHash: recoveryHash,
			},
		},
	}
	service := NewAuthService(repo)

	if _, err := service.FindUserByEmailAndRecoveryCode("other@example.com", recoveryCode); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Fatalf("expected ErrRecoveryCodeNotFound for mismatched email, got %v", err)
	}
}

func TestAuthServiceResolveUserByResetToken(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:           42,
			PasswordHash: string(passwordHash),
		},
	}
	service := NewAuthService(repo)

	token, err := service.BuildPasswordResetToken(secret, 42, repo.user.PasswordHash, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	user, err := service.ResolveUserByResetToken(secret, token, now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("ResolveUserByResetToken() unexpected error: %v", err)
	}
	if user.ID != 42 {
		t.Fatalf("expected user id 42, got %d", user.ID)
	}
}

func TestAuthServiceResolveUserByResetTokenRejectsStateMismatch(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}
	changedHash, err := bcrypt.GenerateFromPassword([]byte("DifferentPass2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash changed password: %v", err)
	}

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:           42,
			PasswordHash: string(changedHash),
		},
	}
	service := NewAuthService(repo)
	token, err := service.BuildPasswordResetToken(secret, 42, string(originalHash), 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	if _, err := service.ResolveUserByResetToken(secret, token, now.Add(1*time.Minute)); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got %v", err)
	}
}

func TestAuthServiceResetPasswordAndRotateRecoveryCode(t *testing.T) {
	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}

	user := models.User{
		ID:                 7,
		PasswordHash:       string(originalHash),
		RecoveryCodeHash:   "old-hash",
		MustChangePassword: true,
	}
	repo := &stubAuthUserRepo{user: user}
	service := NewAuthService(repo)

	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCode(&user, "EvenStronger2")
	if err != nil {
		t.Fatalf("ResetPasswordAndRotateRecoveryCode() unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatalf("expected non-empty recovery code")
	}
	if !repo.saveCalled {
		t.Fatalf("expected Save() to be called")
	}
	if repo.user.MustChangePassword {
		t.Fatalf("expected MustChangePassword=false after reset")
	}
	if repo.user.RecoveryCodeHash == "" || repo.user.RecoveryCodeHash == "old-hash" {
		t.Fatalf("expected rotated recovery code hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.user.PasswordHash), []byte("EvenStronger2")) != nil {
		t.Fatalf("expected password hash updated to new password")
	}
}

func TestAuthServiceRegenerateRecoveryCode(t *testing.T) {
	repo := &stubAuthUserRepo{}
	service := NewAuthService(repo)

	recoveryCode, err := service.RegenerateRecoveryCode(55)
	if err != nil {
		t.Fatalf("RegenerateRecoveryCode() unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatalf("expected non-empty recovery code")
	}
	if repo.updatedUserID != 55 {
		t.Fatalf("expected UpdateRecoveryCodeHash to be called for user 55, got %d", repo.updatedUserID)
	}
	if repo.updatedRecoveryHash == "" {
		t.Fatalf("expected non-empty recovery hash update")
	}
}

func TestAuthServiceResolveUserByAuthSessionToken(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:    42,
			Email: "owner@example.com",
			Role:  models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	token, err := service.BuildAuthSessionToken(secret, 42, models.RoleOwner, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionToken() unexpected error: %v", err)
	}

	user, err := service.ResolveUserByAuthSessionToken(secret, token, now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("ResolveUserByAuthSessionToken() unexpected error: %v", err)
	}
	if user.ID != 42 {
		t.Fatalf("expected user id 42, got %d", user.ID)
	}
}

func TestAuthServiceResolveUserByAuthSessionTokenRejectsRevokedSession(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:                 42,
			Email:              "owner@example.com",
			Role:               models.RoleOwner,
			MustChangePassword: true,
		},
	}
	service := NewAuthService(repo)

	token, err := service.BuildAuthSessionToken(secret, 42, models.RoleOwner, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionToken() unexpected error: %v", err)
	}

	if _, err := service.ResolveUserByAuthSessionToken(secret, token, now.Add(1*time.Minute)); !errors.Is(err, ErrAuthSessionTokenRevoked) {
		t.Fatalf("expected ErrAuthSessionTokenRevoked, got %v", err)
	}
}
