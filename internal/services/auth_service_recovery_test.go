package services

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type stubAuthUserRepo struct {
	existsByEmail            bool
	existsByEmailErr         error
	findByEmailUser          models.User
	findByEmailErr           error
	findByEmailOptionalEmail string
	findByEmailOptionalUser  models.User
	findByEmailOptionalFound bool
	findByEmailOptionalErr   error
	findAllByEmailUsers      []models.User
	findAllByEmailErr        error
	findByIDOptionalUser     models.User
	findByIDOptionalFound    bool
	findByIDOptionalErr      error
	user                     models.User
	findByIDErr              error
	createErr                error
	createCalled             bool
	createdUser              models.User
	updatePasswordErr        error
	updatePasswordCalled     bool
	forceResetErr            error
	forceResetCalled         bool
	updateRecoveryPassErr    error
	updateRecoveryCalled     bool
	bumpSessionErr           error
	bumpSessionCalled        bool
	updateRecoveryCodeErr    error
	updatedUserID            uint
	updatedRecoveryHash      string
	updatedPasswordHash      string
	updatedMustChange        bool
	updateHashOnlyErr        error
	updateHashOnlyCalls      int
	updateHashOnlyUserID     uint
	updateHashOnlyHash       string
	claimRevealErr           error
	claimRevealUserID        uint
}

func (stub *stubAuthUserRepo) ExistsByNormalizedEmail(context.Context, string) (bool, error) {
	if stub.existsByEmailErr != nil {
		return false, stub.existsByEmailErr
	}
	return stub.existsByEmail, nil
}

func (stub *stubAuthUserRepo) FindByNormalizedEmail(context.Context, string) (models.User, error) {
	if stub.findByEmailErr != nil {
		return models.User{}, stub.findByEmailErr
	}
	return stub.findByEmailUser, nil
}

func (stub *stubAuthUserRepo) FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error) {
	if stub.findByEmailOptionalErr != nil {
		return models.User{}, false, stub.findByEmailOptionalErr
	}
	if stub.findByEmailOptionalEmail != "" && stub.findByEmailOptionalEmail != email {
		return models.User{}, false, nil
	}
	if stub.findByEmailOptionalFound {
		return stub.findByEmailOptionalUser, true, nil
	}
	if stub.user.ID != 0 || stub.user.Email != "" || stub.user.RecoveryCodeHash != "" || stub.user.PasswordHash != "" {
		return stub.user, true, nil
	}
	return models.User{}, false, nil
}

func (stub *stubAuthUserRepo) FindAllByNormalizedEmail(context.Context, string) ([]models.User, error) {
	if stub.findAllByEmailErr != nil {
		return nil, stub.findAllByEmailErr
	}
	return stub.findAllByEmailUsers, nil
}

func (stub *stubAuthUserRepo) FindByID(context.Context, uint) (models.User, error) {
	if stub.findByIDErr != nil {
		return models.User{}, stub.findByIDErr
	}
	return stub.user, nil
}

func (stub *stubAuthUserRepo) FindByIDOptional(context.Context, uint) (models.User, bool, error) {
	if stub.findByIDOptionalErr != nil {
		return models.User{}, false, stub.findByIDOptionalErr
	}
	if stub.findByIDOptionalFound {
		return stub.findByIDOptionalUser, true, nil
	}
	if stub.user.ID != 0 {
		return stub.user, true, nil
	}
	return models.User{}, false, nil
}

func (stub *stubAuthUserRepo) Create(ctx context.Context, user *models.User) error {
	if stub.createErr != nil {
		return stub.createErr
	}
	stub.createCalled = true
	stub.createdUser = *user
	return nil
}

func (stub *stubAuthUserRepo) UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, recoveryHash string) error {
	if stub.updateRecoveryCodeErr != nil {
		return stub.updateRecoveryCodeErr
	}
	stub.updatedUserID = userID
	stub.updatedRecoveryHash = recoveryHash
	// The real UPDATE NULLs recovery_code_revealed_at in the same statement, so
	// a fresh code arrives with its one-time reveal armed.
	stub.user.RecoveryCodeRevealedAt = nil
	stub.user.AuthSessionVersion = NormalizeAuthSessionVersion(stub.user.AuthSessionVersion) + 1
	return nil
}

func (stub *stubAuthUserRepo) UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error {
	if stub.updatePasswordErr != nil {
		return stub.updatePasswordErr
	}
	stub.updatePasswordCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedMustChange = mustChangePassword
	stub.user.ID = userID
	stub.user.PasswordHash = passwordHash
	stub.user.LocalAuthEnabled = true
	stub.user.MustChangePassword = mustChangePassword
	stub.user.AuthSessionVersion = NormalizeAuthSessionVersion(stub.user.AuthSessionVersion) + 1
	return nil
}

func (stub *stubAuthUserRepo) ForceResetPasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string) error {
	if stub.forceResetErr != nil {
		return stub.forceResetErr
	}
	stub.forceResetCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedMustChange = true
	stub.user.ID = userID
	stub.user.PasswordHash = passwordHash
	stub.user.LocalAuthEnabled = true
	stub.user.MustChangePassword = true
	// Operator reset force-clears the feed token in the same atomic update.
	stub.user.CalendarFeedSelector = ""
	stub.user.CalendarFeedVerifierHash = ""
	stub.user.AuthSessionVersion = NormalizeAuthSessionVersion(stub.user.AuthSessionVersion) + 1
	return nil
}

func (stub *stubAuthUserRepo) UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error {
	if stub.updateRecoveryPassErr != nil {
		return stub.updateRecoveryPassErr
	}
	stub.updateRecoveryCalled = true
	stub.updatedUserID = userID
	stub.updatedPasswordHash = passwordHash
	stub.updatedRecoveryHash = recoveryHash
	stub.updatedMustChange = mustChangePassword
	stub.user.ID = userID
	stub.user.PasswordHash = passwordHash
	stub.user.RecoveryCodeHash = recoveryHash
	// Mirrors the same-statement NULL of recovery_code_revealed_at: the code
	// this write mints arrives with its one-time reveal armed.
	stub.user.RecoveryCodeRevealedAt = nil
	stub.user.LocalAuthEnabled = true
	stub.user.MustChangePassword = mustChangePassword
	stub.user.AuthSessionVersion = NormalizeAuthSessionVersion(stub.user.AuthSessionVersion) + 1
	return nil
}

func (stub *stubAuthUserRepo) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string, recoveryHash string) error {
	return stub.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, userID, newPasswordHash, recoveryHash, false)
}

func (stub *stubAuthUserRepo) UpdatePasswordHashOnly(ctx context.Context, userID uint, passwordHash string) error {
	stub.updateHashOnlyCalls++
	if stub.updateHashOnlyErr != nil {
		return stub.updateHashOnlyErr
	}
	stub.updateHashOnlyUserID = userID
	stub.updateHashOnlyHash = passwordHash
	stub.user.PasswordHash = passwordHash
	return nil
}

// ClaimRecoveryCodeReveal models the real compare-and-set: the first call
// consumes the reveal, every later one loses because the mark is already set.
// The mint methods above reset it to nil the way their UPDATE statements NULL
// recovery_code_revealed_at.
func (stub *stubAuthUserRepo) ClaimRecoveryCodeReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error) {
	if stub.claimRevealErr != nil {
		return false, stub.claimRevealErr
	}
	stub.claimRevealUserID = userID
	if stub.user.RecoveryCodeRevealedAt != nil {
		return false, nil
	}
	claimedAt := revealedAt.UTC()
	stub.user.RecoveryCodeRevealedAt = &claimedAt
	return true, nil
}

func (stub *stubAuthUserRepo) BumpAuthSessionVersion(ctx context.Context, userID uint) error {
	if stub.bumpSessionErr != nil {
		return stub.bumpSessionErr
	}
	stub.bumpSessionCalled = true
	stub.updatedUserID = userID
	stub.user.ID = userID
	stub.user.AuthSessionVersion = NormalizeAuthSessionVersion(stub.user.AuthSessionVersion) + 1
	return nil
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

		user, recoveryCode, err := service.RegisterOwner(context.Background(), "owner@example.com", "StrongPass1", "StrongPass1", createdAt)
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
		if _, _, err := service.RegisterOwner(context.Background(), "owner@example.com", "StrongPass1", "WrongPass2", createdAt); !errors.Is(err, ErrAuthPasswordMismatch) {
			t.Fatalf("expected ErrAuthPasswordMismatch, got %v", err)
		}
	})

	t.Run("email exists", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmail: true}
		service := NewAuthService(repo)
		if _, _, err := service.RegisterOwner(context.Background(), "owner@example.com", "StrongPass1", "StrongPass1", createdAt); !errors.Is(err, ErrAuthEmailExists) {
			t.Fatalf("expected ErrAuthEmailExists, got %v", err)
		}
		if repo.createCalled {
			t.Fatalf("did not expect Create() when email already exists")
		}
	})

	t.Run("exists check fails", func(t *testing.T) {
		repo := &stubAuthUserRepo{existsByEmailErr: errors.New("db down")}
		service := NewAuthService(repo)
		if _, _, err := service.RegisterOwner(context.Background(), "owner@example.com", "StrongPass1", "StrongPass1", createdAt); !errors.Is(err, ErrAuthRegisterFailed) {
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
			findAllByEmailUsers: []models.User{{
				ID:                 18,
				Email:              "owner@example.com",
				PasswordHash:       string(originalHash),
				LocalAuthEnabled:   true,
				MustChangePassword: false,
			}},
		}
		service := NewAuthService(repo)

		if err := service.ForceResetPasswordByEmail(context.Background(), " Owner@Example.com ", "EvenStronger2"); err != nil {
			t.Fatalf("ForceResetPasswordByEmail() unexpected error: %v", err)
		}
		if !repo.forceResetCalled {
			t.Fatal("expected ForceResetPasswordAndRevokeSessions() to be called")
		}
		if repo.updatePasswordCalled {
			t.Fatal("operator reset must NOT use the routine UpdatePasswordAndRevokeSessions path")
		}
		if !repo.user.MustChangePassword {
			t.Fatal("expected MustChangePassword=true after forced reset")
		}
		if bcrypt.CompareHashAndPassword([]byte(repo.user.PasswordHash), []byte("EvenStronger2")) != nil {
			t.Fatal("expected saved password hash to match new password")
		}
		if repo.user.AuthSessionVersion != 2 {
			t.Fatalf("expected auth session version to increment to 2, got %d", repo.user.AuthSessionVersion)
		}
	})

	t.Run("missing password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", " "); !errors.Is(err, ErrAuthResetInvalid) {
			t.Fatalf("expected ErrAuthResetInvalid, got %v", err)
		}
	})

	t.Run("weak password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", "12345678"); !errors.Is(err, ErrAuthWeakPassword) {
			t.Fatalf("expected ErrAuthWeakPassword, got %v", err)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByEmail(context.Background(), "missing@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthUserNotFound) {
			t.Fatalf("expected ErrAuthUserNotFound, got %v", err)
		}
		if repo.forceResetCalled {
			t.Fatal("did not expect password update when user is missing")
		}
	})

	t.Run("lookup failure", func(t *testing.T) {
		repo := &stubAuthUserRepo{findAllByEmailErr: errors.New("db down")}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthUserLookupFailed) {
			t.Fatalf("expected ErrAuthUserLookupFailed, got %v", err)
		}
	})

	t.Run("save failure", func(t *testing.T) {
		originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash original password: %v", err)
		}

		repo := &stubAuthUserRepo{
			findAllByEmailUsers: []models.User{{
				ID:               18,
				Email:            "owner@example.com",
				PasswordHash:     string(originalHash),
				LocalAuthEnabled: true,
			}},
			forceResetErr: errors.New("write failed"),
		}
		service := NewAuthService(repo)

		if err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", "EvenStronger2"); !errors.Is(err, ErrAuthPasswordUpdate) {
			t.Fatalf("expected ErrAuthPasswordUpdate, got %v", err)
		}
	})

	// TestAuthServiceForceResetPasswordByEmail/ambiguous-address is the
	// finding-DB-2 regression: two rows sharing one normalized address (the
	// exact shape RenormalizeUserEmail leaves standing) must be refused, not
	// silently resolved to one of them.
	t.Run("ambiguous address", func(t *testing.T) {
		repo := &stubAuthUserRepo{
			findAllByEmailUsers: []models.User{
				{ID: 5, Email: "owner@example.com"},
				{ID: 18, Email: "owner@example.com"},
			},
		}
		service := NewAuthService(repo)

		err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", "EvenStronger2")
		var ambiguous *AmbiguousEmailError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("expected *AmbiguousEmailError, got %v", err)
		}
		if got, want := ambiguous.IDs, []uint{5, 18}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("expected ambiguous ids %v, got %v", want, got)
		}
		if repo.forceResetCalled {
			t.Fatal("did not expect password update on an ambiguous address")
		}
	})
}

func TestAuthServiceForceResetPasswordByID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash original password: %v", err)
		}

		repo := &stubAuthUserRepo{
			findByIDOptionalFound: true,
			findByIDOptionalUser: models.User{
				ID:                 18,
				Email:              "owner@example.com",
				PasswordHash:       string(originalHash),
				LocalAuthEnabled:   true,
				MustChangePassword: false,
			},
		}
		service := NewAuthService(repo)

		if err := service.ForceResetPasswordByID(context.Background(), 18, "EvenStronger2"); err != nil {
			t.Fatalf("ForceResetPasswordByID() unexpected error: %v", err)
		}
		if !repo.forceResetCalled {
			t.Fatal("expected ForceResetPasswordAndRevokeSessions() to be called")
		}
		if repo.updatedUserID != 18 {
			t.Fatalf("expected reset to target id 18, got %d", repo.updatedUserID)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByID(context.Background(), 0, "EvenStronger2"); !errors.Is(err, ErrAuthUserIDRequired) {
			t.Fatalf("expected ErrAuthUserIDRequired, got %v", err)
		}
	})

	t.Run("missing password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByID(context.Background(), 18, " "); !errors.Is(err, ErrAuthResetInvalid) {
			t.Fatalf("expected ErrAuthResetInvalid, got %v", err)
		}
	})

	// TestAuthServiceForceResetPasswordByID/weak_password mirrors
	// ForceResetPasswordByEmail's own "weak password" subtest: the id form
	// has its own authPasswordPolicyError(ValidatePasswordStrength(...)) check
	// (a distinct line from the email form's, even though the logic is
	// identical), which nothing else in this file's suite reaches.
	t.Run("weak password", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		if err := service.ForceResetPasswordByID(context.Background(), 18, "12345678"); !errors.Is(err, ErrAuthWeakPassword) {
			t.Fatalf("expected ErrAuthWeakPassword, got %v", err)
		}
	})

	t.Run("id not found", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByID(context.Background(), 9, "EvenStronger2"); !errors.Is(err, ErrAuthUserNotFound) {
			t.Fatalf("expected ErrAuthUserNotFound, got %v", err)
		}
		if repo.forceResetCalled {
			t.Fatal("did not expect password update when id is missing")
		}
	})

	t.Run("lookup failure", func(t *testing.T) {
		repo := &stubAuthUserRepo{findByIDOptionalErr: errors.New("db down")}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByID(context.Background(), 18, "EvenStronger2"); !errors.Is(err, ErrAuthUserLookupFailed) {
			t.Fatalf("expected ErrAuthUserLookupFailed, got %v", err)
		}
	})

	t.Run("save failure", func(t *testing.T) {
		repo := &stubAuthUserRepo{
			findByIDOptionalFound: true,
			findByIDOptionalUser:  models.User{ID: 18, Email: "owner@example.com"},
			forceResetErr:         errors.New("write failed"),
		}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByID(context.Background(), 18, "EvenStronger2"); !errors.Is(err, ErrAuthPasswordUpdate) {
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
	// Off by default, per SECURITY.md's Art. 25 row; the whole producer set is
	// swept in TestOwnerAccountConstructorsLeaveAutoPeriodFillOff.
	if user.AutoPeriodFill != models.DefaultAutoPeriodFill {
		t.Fatalf("expected AutoPeriodFill=%t, got %t", models.DefaultAutoPeriodFill, user.AutoPeriodFill)
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
	if !user.LocalAuthEnabled {
		t.Fatal("expected LocalAuthEnabled=true for owner registration")
	}
	if user.AuthSessionVersion != 1 {
		t.Fatalf("expected auth session version 1, got %d", user.AuthSessionVersion)
	}
}

func TestAuthServiceAuthenticateCredentials(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailUser: models.User{
			ID:               77,
			Email:            "login@example.com",
			PasswordHash:     string(passwordHash),
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "StrongPass1")
	if err != nil {
		t.Fatalf("AuthenticateCredentials() unexpected error: %v", err)
	}
	if user.ID != 77 {
		t.Fatalf("expected user id 77, got %d", user.ID)
	}

	if _, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "WrongPass2"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for wrong password, got %v", err)
	}

	repo.findByEmailErr = errors.New("user not found")
	if _, err := service.AuthenticateCredentials(context.Background(), "missing@example.com", "StrongPass1"); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected ErrAuthInvalidCreds for missing user, got %v", err)
	}
}

func TestAuthServiceFindUserByEmailRecoveryCodeAndPassword(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailOptionalEmail: "owner@example.com",
		findByEmailOptionalFound: true,
		findByEmailOptionalUser: models.User{
			ID:               22,
			Email:            "owner@example.com",
			PasswordHash:     string(passwordHash),
			RecoveryCodeHash: recoveryHash,
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	user, err := service.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "Owner@Example.com", recoveryCode, "StrongPass1")
	if err != nil {
		t.Fatalf("FindUserByEmailRecoveryCodeAndPassword() unexpected error: %v", err)
	}
	if user == nil || user.ID != 22 {
		t.Fatalf("expected user id 22, got %#v", user)
	}
}

// TestAuthServiceFindUserByEmailRecoveryCodeAndPasswordRejectsWrongPassword
// pins the second operand: a correct recovery code alone must not resolve an
// account, or the recovery code is again a single-secret takeover credential.
func TestAuthServiceFindUserByEmailRecoveryCodeAndPasswordRejectsWrongPassword(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailOptionalEmail: "owner@example.com",
		findByEmailOptionalFound: true,
		findByEmailOptionalUser: models.User{
			ID:               22,
			Email:            "owner@example.com",
			PasswordHash:     string(passwordHash),
			RecoveryCodeHash: recoveryHash,
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	for name, password := range map[string]string{"wrong": "NotThePassword9", "empty": ""} {
		if _, err := service.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "Owner@Example.com", recoveryCode, password); !errors.Is(err, ErrRecoveryCodeNotFound) {
			t.Fatalf("expected ErrRecoveryCodeNotFound for a %s password, got %v", name, err)
		}
	}
}

func TestAuthServiceFindUserByEmailRecoveryCodeAndPasswordRejectsMismatch(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailOptionalEmail: "owner@example.com",
		findByEmailOptionalFound: true,
		findByEmailOptionalUser: models.User{
			ID:               22,
			Email:            "owner@example.com",
			RecoveryCodeHash: recoveryHash,
			LocalAuthEnabled: true,
		},
	}
	service := NewAuthService(repo)

	if _, err := service.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "other@example.com", recoveryCode, "StrongPass1"); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Fatalf("expected ErrRecoveryCodeNotFound for mismatched email, got %v", err)
	}
}

func TestAuthServiceFindUserByEmailRecoveryCodeAndPasswordRejectsMissingUser(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{})

	if _, err := service.FindUserByEmailRecoveryCodeAndPassword(context.Background(), "missing@example.com", "OVUM-ABCD-2345-EFGH", "StrongPass1"); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Fatalf("expected ErrRecoveryCodeNotFound for missing user, got %v", err)
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
			ID:               42,
			PasswordHash:     string(passwordHash),
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	token, err := service.BuildPasswordResetToken(secret, 42, repo.user.PasswordHash, PasswordResetTokenPurposeRecovery, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	user, err := service.ResolveUserByResetToken(context.Background(), secret, token, now.Add(1*time.Minute))
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
			ID:               42,
			PasswordHash:     string(changedHash),
			LocalAuthEnabled: true,
		},
	}
	service := NewAuthService(repo)
	token, err := service.BuildPasswordResetToken(secret, 42, string(originalHash), PasswordResetTokenPurposeRecovery, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	if _, err := service.ResolveUserByResetToken(context.Background(), secret, token, now.Add(1*time.Minute)); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got %v", err)
	}
}

func TestAuthServiceRegenerateRecoveryCode(t *testing.T) {
	repo := &stubAuthUserRepo{}
	service := NewAuthService(repo)

	recoveryCode, err := service.RegenerateRecoveryCode(context.Background(), 55)
	if err != nil {
		t.Fatalf("RegenerateRecoveryCode() unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatalf("expected non-empty recovery code")
	}
	if repo.updatedUserID != 55 {
		t.Fatalf("expected UpdateRecoveryCodeHashAndRevokeSessions to be called for user 55, got %d", repo.updatedUserID)
	}
	if repo.updatedRecoveryHash == "" {
		t.Fatalf("expected non-empty recovery hash update")
	}
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected AuthSessionVersion to be bumped to 2, got %d", repo.user.AuthSessionVersion)
	}
}

// TestAuthServiceClaimRecoveryCodeRevealIsSingleUseAndOwnerBound pins the seam
// the reveal handlers gate on: the first claim wins, a replay loses, and a claim
// naming no account is refused BEFORE it reaches the repository — an absent
// owner id is invalid input, never a claim that skips the comparison.
func TestAuthServiceClaimRecoveryCodeRevealIsSingleUseAndOwnerBound(t *testing.T) {
	repo := &stubAuthUserRepo{}
	service := NewAuthService(repo)
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)

	claimed, err := service.ClaimRecoveryCodeReveal(context.Background(), 42, now)
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal() unexpected error: %v", err)
	}
	if !claimed {
		t.Fatal("expected the first claim to consume the reveal")
	}
	if repo.claimRevealUserID != 42 {
		t.Fatalf("expected the claim to name owner 42, got %d", repo.claimRevealUserID)
	}

	replayed, err := service.ClaimRecoveryCodeReveal(context.Background(), 42, now)
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal() replay error: %v", err)
	}
	if replayed {
		t.Fatal("expected a replayed claim to lose against the mark already set")
	}

	unattributed := &stubAuthUserRepo{}
	if _, err := NewAuthService(unattributed).ClaimRecoveryCodeReveal(context.Background(), 0, now); !errors.Is(err, ErrAuthUserRequired) {
		t.Fatalf("expected ErrAuthUserRequired for a claim naming no account, got %v", err)
	}
	if unattributed.claimRevealUserID != 0 || unattributed.user.RecoveryCodeRevealedAt != nil {
		t.Fatal("a claim naming no account must never reach the repository")
	}
}

// TestAuthServiceClaimRecoveryCodeRevealSurfacesStorageFailure keeps the
// storage error distinguishable from "already claimed": the handler refuses on
// either, but conflating them here would hide a database outage behind a
// perfectly ordinary-looking replay.
func TestAuthServiceClaimRecoveryCodeRevealSurfacesStorageFailure(t *testing.T) {
	storageFailure := errors.New("claim failed")
	repo := &stubAuthUserRepo{claimRevealErr: storageFailure}

	claimed, err := NewAuthService(repo).ClaimRecoveryCodeReveal(context.Background(), 42, time.Now())
	if !errors.Is(err, storageFailure) {
		t.Fatalf("expected the storage failure to surface, got %v", err)
	}
	if claimed {
		t.Fatal("a claim that could not be recorded must never report success")
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

	token, _, err := service.BuildAuthSessionTokenWithSessionID(secret, 42, models.RoleOwner, 1, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionTokenWithSessionID() unexpected error: %v", err)
	}

	user, err := service.ResolveUserByAuthSessionToken(context.Background(), secret, token, now.Add(1*time.Minute))
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

	token, _, err := service.BuildAuthSessionTokenWithSessionID(secret, 42, models.RoleOwner, 1, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionTokenWithSessionID() unexpected error: %v", err)
	}

	if _, err := service.ResolveUserByAuthSessionToken(context.Background(), secret, token, now.Add(1*time.Minute)); !errors.Is(err, ErrAuthSessionTokenRevoked) {
		t.Fatalf("expected ErrAuthSessionTokenRevoked, got %v", err)
	}
}

func TestAuthServiceRevokeAuthSessions(t *testing.T) {
	repo := &stubAuthUserRepo{
		user: models.User{
			ID:                 42,
			AuthSessionVersion: 1,
		},
	}
	service := NewAuthService(repo)

	if err := service.RevokeAuthSessions(context.Background(), 42); err != nil {
		t.Fatalf("RevokeAuthSessions() unexpected error: %v", err)
	}
	if !repo.bumpSessionCalled {
		t.Fatal("expected BumpAuthSessionVersion() to be called")
	}
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected auth session version to increment to 2, got %d", repo.user.AuthSessionVersion)
	}
}
