package services

import (
	"errors"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestValidatePasswordChangeRejectsInvalidInput(t *testing.T) {
	service := NewSettingsService(nil)

	err := service.ValidatePasswordChange("hash", " ", "NewPass1", "NewPass1")
	if !errors.Is(err, ErrSettingsPasswordChangeInvalidInput) {
		t.Fatalf("expected ErrSettingsPasswordChangeInvalidInput, got %v", err)
	}
}

func TestValidatePasswordChangeRejectsMismatch(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = service.ValidatePasswordChange(string(passwordHash), "StrongPass1", "NewPass1", "OtherPass1")
	if !errors.Is(err, ErrSettingsPasswordMismatch) {
		t.Fatalf("expected ErrSettingsPasswordMismatch, got %v", err)
	}
}

func TestValidatePasswordChangeRejectsInvalidCurrentPassword(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = service.ValidatePasswordChange(string(passwordHash), "WrongPass1", "NewPass1", "NewPass1")
	if !errors.Is(err, ErrSettingsInvalidCurrentPassword) {
		t.Fatalf("expected ErrSettingsInvalidCurrentPassword, got %v", err)
	}
}

func TestValidatePasswordChangeRejectsUnchangedPassword(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = service.ValidatePasswordChange(string(passwordHash), "StrongPass1", "StrongPass1", "StrongPass1")
	if !errors.Is(err, ErrSettingsNewPasswordMustDiffer) {
		t.Fatalf("expected ErrSettingsNewPasswordMustDiffer, got %v", err)
	}
}

func TestValidatePasswordChangeRejectsWeakPassword(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	err = service.ValidatePasswordChange(string(passwordHash), "StrongPass1", "12345678", "12345678")
	if !errors.Is(err, ErrSettingsWeakPassword) {
		t.Fatalf("expected ErrSettingsWeakPassword, got %v", err)
	}
}

func TestValidatePasswordChangeAcceptsValidInput(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := service.ValidatePasswordChange(string(passwordHash), "StrongPass1", "EvenStronger2", "EvenStronger2"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestChangePasswordUpdatesHashedPassword(t *testing.T) {
	repo := &stubSettingsUserRepo{}
	service := NewSettingsService(repo)

	currentHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := &models.User{
		ID:                 42,
		PasswordHash:       string(currentHash),
		AuthSessionVersion: 3,
	}

	err = service.ChangePassword(user, "StrongPass1", "EvenStronger2", "EvenStronger2")
	if err != nil {
		t.Fatalf("expected successful ChangePassword, got %v", err)
	}
	if !repo.updatePasswordCalled {
		t.Fatal("expected UpdatePassword call")
	}
	if repo.updatedUserID != 42 {
		t.Fatalf("expected updated user id 42, got %d", repo.updatedUserID)
	}
	if repo.updatedMustChangePassword {
		t.Fatal("expected mustChangePassword=false")
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.updatedPasswordHash), []byte("EvenStronger2")) != nil {
		t.Fatalf("expected stored hash to match new password")
	}
	if user.AuthSessionVersion != 4 {
		t.Fatalf("expected auth session version to increment to 4, got %d", user.AuthSessionVersion)
	}
}

func TestChangePasswordPropagatesValidationErrorWithoutUpdate(t *testing.T) {
	repo := &stubSettingsUserRepo{}
	service := NewSettingsService(repo)

	currentHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := &models.User{
		ID:                 42,
		PasswordHash:       string(currentHash),
		AuthSessionVersion: 1,
	}

	err = service.ChangePassword(user, "WrongPass1", "EvenStronger2", "EvenStronger2")
	if !errors.Is(err, ErrSettingsInvalidCurrentPassword) {
		t.Fatalf("expected ErrSettingsInvalidCurrentPassword, got %v", err)
	}
	if repo.updatePasswordCalled {
		t.Fatal("expected no UpdatePassword call on validation error")
	}
}

func TestChangePasswordWrapsUpdateError(t *testing.T) {
	repo := &stubSettingsUserRepo{
		updatePasswordErr: errors.New("write failure"),
	}
	service := NewSettingsService(repo)

	currentHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := &models.User{
		ID:           42,
		PasswordHash: string(currentHash),
	}

	err = service.ChangePassword(user, "StrongPass1", "EvenStronger2", "EvenStronger2")
	if !errors.Is(err, ErrSettingsPasswordUpdateFailed) {
		t.Fatalf("expected ErrSettingsPasswordUpdateFailed, got %v", err)
	}
}

type stubSettingsUserRepo struct {
	updatePasswordCalled      bool
	updatedUserID             uint
	updatedPasswordHash       string
	updatedMustChangePassword bool
	updatePasswordErr         error
}

func (stub *stubSettingsUserRepo) UpdateDisplayName(uint, string) error {
	return nil
}

func (stub *stubSettingsUserRepo) UpdateRecoveryCodeHash(uint, string) error {
	return nil
}

func (stub *stubSettingsUserRepo) UpdatePasswordAndRevokeSessions(userID uint, passwordHash string, mustChangePassword bool) error {
	stub.updatePasswordCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedMustChangePassword = mustChangePassword
	return stub.updatePasswordErr
}

func (stub *stubSettingsUserRepo) UpdateByID(uint, map[string]any) error {
	return nil
}

func (stub *stubSettingsUserRepo) LoadSettingsByID(uint) (models.User, error) {
	return models.User{}, nil
}

func (stub *stubSettingsUserRepo) ClearAllDataAndResetSettings(uint) error {
	return nil
}

func (stub *stubSettingsUserRepo) DeleteAccountAndRelatedData(uint) error {
	return nil
}
