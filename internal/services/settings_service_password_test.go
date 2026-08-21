package services

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
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

// TestSettingsPasswordFormsSeparateTooLongFromWeak carries the too-long/weak
// split into the settings layer. Both password-setting entry points are
// covered, because they translate the shared policy verdict independently:
// ValidatePasswordChange for an account that already has a local password, and
// PrepareLocalPasswordHash for an SSO-only account enabling one. Collapsing
// either back onto ErrSettingsWeakPassword puts the composition message on a
// length failure — the defect this PR exists to remove — and no assertion on
// the change-password form would notice.
func TestSettingsPasswordFormsSeparateTooLongFromWeak(t *testing.T) {
	service := NewSettingsService(nil)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	// 37 characters, 73 bytes, with uppercase, lowercase and a digit: length is
	// the only rule it breaks.
	tooLong := "Пароль1" + strings.Repeat("ы", 30)
	if runes, bytes := len([]rune(tooLong)), len(tooLong); runes > maxPasswordBytes || bytes <= maxPasswordBytes {
		t.Fatalf("test setup: passphrase is %d runes / %d bytes, want <= %d runes and > %d bytes", runes, bytes, maxPasswordBytes, maxPasswordBytes)
	}

	err = service.ValidatePasswordChange(string(passwordHash), "StrongPass1", tooLong, tooLong)
	if !errors.Is(err, ErrSettingsPasswordTooLong) {
		t.Fatalf("ValidatePasswordChange: expected ErrSettingsPasswordTooLong, got %v", err)
	}
	if errors.Is(err, ErrSettingsWeakPassword) {
		t.Fatal("ValidatePasswordChange: the length refusal must not also read as the composition one")
	}

	if _, err := service.PrepareLocalPasswordHash(&models.User{}, tooLong, tooLong); !errors.Is(err, ErrSettingsPasswordTooLong) {
		t.Fatalf("PrepareLocalPasswordHash: expected ErrSettingsPasswordTooLong, got %v", err)
	}

	// The composition failures keep the weak sentinel on both entry points.
	if err := service.ValidatePasswordChange(string(passwordHash), "StrongPass1", "12345678", "12345678"); !errors.Is(err, ErrSettingsWeakPassword) {
		t.Fatalf("ValidatePasswordChange: expected ErrSettingsWeakPassword for a composition failure, got %v", err)
	}
	if _, err := service.PrepareLocalPasswordHash(&models.User{}, "12345678", "12345678"); !errors.Is(err, ErrSettingsWeakPassword) {
		t.Fatalf("PrepareLocalPasswordHash: expected ErrSettingsWeakPassword for a composition failure, got %v", err)
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

	err = service.ChangePassword(context.Background(), ReauthAttempt{ClientKey: "test-client"}, user, "StrongPass1", "EvenStronger2", "EvenStronger2")
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

	err = service.ChangePassword(context.Background(), ReauthAttempt{ClientKey: "test-client"}, user, "WrongPass1", "EvenStronger2", "EvenStronger2")
	if !errors.Is(err, ErrSettingsInvalidCurrentPassword) {
		t.Fatalf("expected ErrSettingsInvalidCurrentPassword, got %v", err)
	}
	if repo.updatePasswordCalled {
		t.Fatal("expected no UpdatePassword call on validation error")
	}
}

// TestChangePasswordRefusesOnceReauthBudgetSpent covers the fourth password-gated
// path. The three erasure flows share validateSettingsActionPassword, but the
// change-password form reaches the credential check through its own service
// method — so without this the budget could be dropped here alone and the form
// would quietly remain a faster password oracle than the login form.
func TestChangePasswordRefusesOnceReauthBudgetSpent(t *testing.T) {
	repo := &stubSettingsUserRepo{}
	service := NewSettingsService(repo)
	service.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 2, time.Minute)

	currentHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &models.User{ID: 42, PasswordHash: string(currentHash), AuthSessionVersion: 1}
	attempt := ReauthAttempt{
		ClientKey: "203.0.113.10",
		UserID:    user.ID,
		Now:       time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	for i := 1; i <= 2; i++ {
		err := service.ChangePassword(context.Background(), attempt, user, "WrongPass1", "EvenStronger2", "EvenStronger2")
		if !errors.Is(err, ErrSettingsInvalidCurrentPassword) {
			t.Fatalf("wrong current password attempt %d: got %v", i, err)
		}
	}

	err = service.ChangePassword(context.Background(), attempt, user, "StrongPass1", "EvenStronger2", "EvenStronger2")
	if !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Fatalf("correct password past the budget: got %v, want ErrSettingsReauthRateLimited", err)
	}
	if repo.updatePasswordCalled {
		t.Fatal("a rate-limited change must not reach the repository")
	}
	if user.AuthSessionVersion != 1 {
		t.Fatalf("a rate-limited change must not bump auth_session_version, got %d", user.AuthSessionVersion)
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

	err = service.ChangePassword(context.Background(), ReauthAttempt{ClientKey: "test-client"}, user, "StrongPass1", "EvenStronger2", "EvenStronger2")
	if !errors.Is(err, ErrSettingsPasswordUpdateFailed) {
		t.Fatalf("expected ErrSettingsPasswordUpdateFailed, got %v", err)
	}
}

type stubSettingsUserRepo struct {
	updatePasswordCalled      bool
	updatedUserID             uint
	updatedPasswordHash       string
	updatedRecoveryHash       string
	updatedMustChangePassword bool
	updatePasswordErr         error
}

func (stub *stubSettingsUserRepo) UpdateDisplayName(context.Context, uint, string) error {
	return nil
}

func (stub *stubSettingsUserRepo) UpdateInterfaceLanguage(context.Context, uint, string) (bool, error) {
	return true, nil
}

func (stub *stubSettingsUserRepo) UpdateUserTimezone(context.Context, uint, string) error {
	return nil
}

func (stub *stubSettingsUserRepo) UpdateReminderLeadDays(context.Context, uint, int) error {
	return nil
}

func (stub *stubSettingsUserRepo) UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error {
	stub.updatePasswordCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedMustChangePassword = mustChangePassword
	return stub.updatePasswordErr
}

func (stub *stubSettingsUserRepo) UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error {
	stub.updatePasswordCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedRecoveryHash = recoveryHash
	stub.updatedMustChangePassword = mustChangePassword
	return stub.updatePasswordErr
}

func (stub *stubSettingsUserRepo) UpdateByID(context.Context, uint, map[string]any) error {
	return nil
}

func (stub *stubSettingsUserRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

func (stub *stubSettingsUserRepo) ClearAllDataAndResetSettings(context.Context, uint) error {
	return nil
}

func (stub *stubSettingsUserRepo) DeleteAccountAndRelatedData(context.Context, uint) error {
	return nil
}

func TestPrepareAndFinalizeLocalPasswordSetup(t *testing.T) {
	repo := &stubSettingsUserRepo{}
	service := NewSettingsService(repo)
	user := &models.User{
		ID:                 77,
		LocalAuthEnabled:   false,
		AuthSessionVersion: 5,
	}

	preparedHash, err := service.PrepareLocalPasswordHash(user, "EvenStronger2", "EvenStronger2")
	if err != nil {
		t.Fatalf("PrepareLocalPasswordHash() unexpected error: %v", err)
	}
	if preparedHash == "" {
		t.Fatal("expected non-empty prepared hash")
	}
	if repo.updatePasswordCalled {
		t.Fatal("Prepare must not touch the database")
	}
	if user.LocalAuthEnabled {
		t.Fatal("Prepare must not flip LocalAuthEnabled")
	}

	recoveryCode, err := service.FinalizeLocalPasswordSetup(context.Background(), user, preparedHash)
	if err != nil {
		t.Fatalf("FinalizeLocalPasswordSetup() unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected recovery code from finalize")
	}
	if !repo.updatePasswordCalled {
		t.Fatal("expected password+recovery update on finalize")
	}
	// The id the update carries is the whole of its owner scoping:
	// UpdatePasswordRecoveryCodeAndRevokeSessions writes the password hash, the
	// recovery hash, local_auth_enabled and the auth_session_version bump under
	// `WHERE id = ?` alone. Leaving it unasserted lets a wrong id overwrite
	// another owner's credential and revoke their sessions with every other
	// assertion here still green.
	if repo.updatedUserID != 77 {
		t.Fatalf("expected the update to target the acting owner 77, got %d", repo.updatedUserID)
	}
	if repo.updatedRecoveryHash == "" {
		t.Fatal("expected persisted recovery hash on finalize")
	}
	if !user.LocalAuthEnabled {
		t.Fatal("expected LocalAuthEnabled=true after finalize")
	}
	if user.AuthSessionVersion != 6 {
		t.Fatalf("expected auth session version to increment to 6, got %d", user.AuthSessionVersion)
	}
}

func TestFinalizeLocalPasswordSetupRejectsWhenAlreadyEnabled(t *testing.T) {
	repo := &stubSettingsUserRepo{}
	service := NewSettingsService(repo)
	user := &models.User{ID: 88, LocalAuthEnabled: true}

	_, err := service.FinalizeLocalPasswordSetup(context.Background(), user, "some-bcrypt-hash")
	if !errors.Is(err, ErrSettingsPasswordChangeInvalidInput) {
		t.Fatalf("expected ErrSettingsPasswordChangeInvalidInput, got %v", err)
	}
	if repo.updatePasswordCalled {
		t.Fatal("must not touch DB when local auth already enabled")
	}
}

// TestFinalizeLocalPasswordSetupScopesTheWriteToTheActingOwner drives the
// local-password enrollment through the REAL user repository with two
// independent owners in one database.
//
// The stub above records whichever id it is handed, so every owner reaches the
// same fields; only a second row can tell the acting owner from a literal 1.
// Owner A is created first and therefore holds id 1 — the value a dropped or
// hard-coded owner id degenerates to — and the enrollment runs for owner B, the
// SSO-only account. Owner A's row must stay byte-for-byte unchanged:
// UpdatePasswordRecoveryCodeAndRevokeSessions is scoped by `WHERE id = ?`
// alone, so a wrong id rewrites A's credential and, through the
// auth_session_version bump, revokes A's sessions.
func TestFinalizeLocalPasswordSetupScopesTheWriteToTheActingOwner(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-local-password-two-owner")
	service := NewSettingsService(db.NewUserRepository(database))

	ownerA := createTwoOwnerUser(t, database, "local-password-owner-a@example.com", withLocalCredentials(t, "OwnerAPass1"))
	ownerB := createTwoOwnerUser(t, database, "local-password-owner-b@example.com", func(user *models.User) {
		// Owner B is the SSO-only account enrolling a local password: no
		// password hash, no recovery code, local auth still off.
		user.PasswordHash = ""
		user.RecoveryCodeHash = ""
		user.LocalAuthEnabled = false
		user.AuthSessionVersion = 1
	})
	requireDistinctTwoOwnerFixture(t, ownerA, ownerB)

	before := readTwoOwnerUser(t, database, ownerA.ID)

	acting := readTwoOwnerUser(t, database, ownerB.ID)
	preparedHash, err := service.PrepareLocalPasswordHash(&acting, "EvenStronger2", "EvenStronger2")
	if err != nil {
		t.Fatalf("PrepareLocalPasswordHash() unexpected error: %v", err)
	}
	recoveryCode, err := service.FinalizeLocalPasswordSetup(context.Background(), &acting, preparedHash)
	if err != nil {
		t.Fatalf("FinalizeLocalPasswordSetup() unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected a recovery code for owner B")
	}

	// Isolation: owner B's enrollment must not have reached owner A's row.
	after := readTwoOwnerUser(t, database, ownerA.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("owner A's row changed while owner B enrolled a local password — the update is not scoped to the acting owner:\nbefore: %+v\nafter:  %+v", before, after)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("OwnerAPass1")); err != nil {
		t.Fatalf("owner A's password no longer verifies after owner B's enrollment: %v", err)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("owner A's auth_session_version moved from %d to %d — another owner's enrollment revoked their sessions", before.AuthSessionVersion, after.AuthSessionVersion)
	}

	// Positive anchor: the enrollment must actually have landed on owner B, or
	// the isolation half above would pass on a service that writes nothing.
	gotB := readTwoOwnerUser(t, database, ownerB.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(gotB.PasswordHash), []byte("EvenStronger2")); err != nil {
		t.Fatalf("expected owner B's new password to verify: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(gotB.RecoveryCodeHash), []byte(recoveryCode)); err != nil {
		t.Fatalf("expected owner B's stored recovery hash to match the returned code: %v", err)
	}
	if !gotB.LocalAuthEnabled {
		t.Fatal("expected owner B's local_auth_enabled to be set by the enrollment")
	}
	if gotB.AuthSessionVersion != 2 {
		t.Fatalf("expected owner B's auth_session_version to be bumped to 2, got %d", gotB.AuthSessionVersion)
	}
}
