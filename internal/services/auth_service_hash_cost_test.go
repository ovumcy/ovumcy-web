package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// mustBcryptCost fails the test if hash is not a parseable bcrypt hash and
// returns its embedded cost otherwise.
func mustBcryptCost(t *testing.T, hash string) int {
	t.Helper()
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost(%q): %v", hash, err)
	}
	return cost
}

// TestPasswordHashCostIsAboveDefault documents the intent behind the named
// constant: production hashing must be stronger than bcrypt.DefaultCost, and
// this test fails if someone lowers the constant to (or below) the default.
func TestPasswordHashCostIsAboveDefault(t *testing.T) {
	if passwordHashCost <= bcrypt.DefaultCost {
		t.Fatalf("passwordHashCost (%d) must exceed bcrypt.DefaultCost (%d)", passwordHashCost, bcrypt.DefaultCost)
	}
}

// TestTimingEqualizationHashesMatchTargetCost pins the placeholder hashes used
// by the timing equalizers to the production cost. A cheaper placeholder makes
// the equalized early-return paths (missing account, OIDC-only account,
// duplicate-email registration) measurably faster than a real bcrypt compare
// at passwordHashCost, reintroducing the account-enumeration timing oracle the
// equalizers exist to close.
func TestTimingEqualizationHashesMatchTargetCost(t *testing.T) {
	for name, hash := range map[string]string{
		"recoveryCodeTimingEqualizationHash": recoveryCodeTimingEqualizationHash,
		"credentialsTimingEqualizationHash":  credentialsTimingEqualizationHash,
	} {
		if got := mustBcryptCost(t, hash); got != passwordHashCost {
			t.Fatalf("%s cost = %d, want passwordHashCost (%d)", name, got, passwordHashCost)
		}
	}
}

// TestNewPasswordHashesUseConfiguredCost asserts every service path that mints
// a fresh password or recovery-code hash stamps it at passwordHashCost, not the
// library default. Each sub-case reads the cost straight out of the produced
// bcrypt hash.
func TestNewPasswordHashesUseConfiguredCost(t *testing.T) {
	t.Run("BuildOwnerUserWithRecovery hashes password and recovery code at target cost", func(t *testing.T) {
		service := NewAuthService(&stubAuthUserRepo{})
		user, recoveryCode, err := service.BuildOwnerUserWithRecovery("owner@example.com", "StrongPass1", time.Now())
		if err != nil {
			t.Fatalf("BuildOwnerUserWithRecovery: %v", err)
		}
		if got := mustBcryptCost(t, user.PasswordHash); got != passwordHashCost {
			t.Fatalf("password hash cost = %d, want %d", got, passwordHashCost)
		}
		if recoveryCode == "" {
			t.Fatal("expected a non-empty recovery code")
		}
		if got := mustBcryptCost(t, user.RecoveryCodeHash); got != passwordHashCost {
			t.Fatalf("recovery code hash cost = %d, want %d", got, passwordHashCost)
		}
	})

	t.Run("GenerateRecoveryCodeHash uses target cost", func(t *testing.T) {
		_, hash, err := GenerateRecoveryCodeHash()
		if err != nil {
			t.Fatalf("GenerateRecoveryCodeHash: %v", err)
		}
		if got := mustBcryptCost(t, hash); got != passwordHashCost {
			t.Fatalf("recovery code hash cost = %d, want %d", got, passwordHashCost)
		}
	})

	t.Run("ForceResetPasswordByEmail writes hash at target cost", func(t *testing.T) {
		repo := &stubAuthUserRepo{
			existsByEmail:   true,
			findByEmailUser: models.User{ID: 5, Email: "owner@example.com", Role: models.RoleOwner},
		}
		service := NewAuthService(repo)
		if err := service.ForceResetPasswordByEmail(context.Background(), "owner@example.com", "StrongPass1"); err != nil {
			t.Fatalf("ForceResetPasswordByEmail: %v", err)
		}
		if got := mustBcryptCost(t, repo.updatedPasswordHash); got != passwordHashCost {
			t.Fatalf("force-reset hash cost = %d, want %d", got, passwordHashCost)
		}
	})

	t.Run("ResetPasswordAndRotateRecoveryCode writes hash at target cost", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		service := NewAuthService(repo)
		user := &models.User{ID: 9, Email: "owner@example.com"}
		if _, err := service.ResetPasswordAndRotateRecoveryCode(context.Background(), user, "StrongPass1"); err != nil {
			t.Fatalf("ResetPasswordAndRotateRecoveryCode: %v", err)
		}
		if got := mustBcryptCost(t, user.PasswordHash); got != passwordHashCost {
			t.Fatalf("reset+rotate hash cost = %d, want %d", got, passwordHashCost)
		}
	})
}

// TestAuthenticateCredentialsRehashesStaleCost is the opportunistic-rehash
// contract: a valid login against a below-target (legacy cost-10) hash upgrades
// the stored hash to passwordHashCost via UpdatePasswordHashOnly (which does NOT
// bump auth_session_version — the session that just authenticated must survive).
func TestAuthenticateCredentialsRehashesStaleCost(t *testing.T) {
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo := &stubAuthUserRepo{
		findByEmailUser: models.User{
			ID:                 77,
			Email:              "login@example.com",
			PasswordHash:       string(legacyHash),
			LocalAuthEnabled:   true,
			Role:               models.RoleOwner,
			AuthSessionVersion: 3,
		},
	}
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "StrongPass1")
	if err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}
	if repo.updateHashOnlyCalls != 1 {
		t.Fatalf("expected exactly 1 opportunistic rehash write, got %d", repo.updateHashOnlyCalls)
	}
	if repo.updateHashOnlyUserID != 77 {
		t.Fatalf("rehash targeted user %d, want 77", repo.updateHashOnlyUserID)
	}
	if got := mustBcryptCost(t, repo.updateHashOnlyHash); got != passwordHashCost {
		t.Fatalf("rehash wrote cost %d, want %d", got, passwordHashCost)
	}
	// The returned user reflects the upgraded hash so a caller that reissues a
	// cookie / persists the struct carries it forward.
	if got := mustBcryptCost(t, user.PasswordHash); got != passwordHashCost {
		t.Fatalf("returned user hash cost = %d, want %d", got, passwordHashCost)
	}
	// The upgraded hash still verifies the same password.
	if bcrypt.CompareHashAndPassword([]byte(repo.updateHashOnlyHash), []byte("StrongPass1")) != nil {
		t.Fatal("upgraded hash no longer verifies the original password")
	}
}

// TestAuthenticateCredentialsSkipsRehashAtTargetCost proves the upgrade is a
// no-op once the stored hash already meets the target cost — no needless write
// on every subsequent login.
func TestAuthenticateCredentialsSkipsRehashAtTargetCost(t *testing.T) {
	currentHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), passwordHashCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo := &stubAuthUserRepo{
		findByEmailUser: models.User{
			ID:               77,
			Email:            "login@example.com",
			PasswordHash:     string(currentHash),
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	if _, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "StrongPass1"); err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}
	if repo.updateHashOnlyCalls != 0 {
		t.Fatalf("expected no rehash write at target cost, got %d", repo.updateHashOnlyCalls)
	}
}

// TestAuthenticateCredentialsWrongPasswordDoesNotRehash guards the ordering: a
// failed CompareHashAndPassword must never trigger a rehash of a stale hash
// (the plaintext was not proven).
func TestAuthenticateCredentialsWrongPasswordDoesNotRehash(t *testing.T) {
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo := &stubAuthUserRepo{
		findByEmailUser: models.User{
			ID:               77,
			Email:            "login@example.com",
			PasswordHash:     string(legacyHash),
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	if _, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "WrongPass2"); err == nil {
		t.Fatal("expected error for wrong password")
	}
	if repo.updateHashOnlyCalls != 0 {
		t.Fatalf("expected no rehash write on wrong password, got %d", repo.updateHashOnlyCalls)
	}
}

// TestRehashPasswordIfStaleDefensiveBranches covers the guard clauses of the
// rehash helper directly: a nil/unsaved user, an unparseable stored hash, and a
// password bcrypt refuses to hash (>72 bytes) must all silently skip the write.
func TestRehashPasswordIfStaleDefensiveBranches(t *testing.T) {
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	t.Run("nil user is a no-op", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		NewAuthService(repo).rehashPasswordIfStale(context.Background(), nil, "StrongPass1")
		if repo.updateHashOnlyCalls != 0 {
			t.Fatalf("expected no write for nil user, got %d", repo.updateHashOnlyCalls)
		}
	})

	t.Run("unsaved user (ID 0) is a no-op", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		user := &models.User{ID: 0, PasswordHash: string(legacyHash)}
		NewAuthService(repo).rehashPasswordIfStale(context.Background(), user, "StrongPass1")
		if repo.updateHashOnlyCalls != 0 {
			t.Fatalf("expected no write for unsaved user, got %d", repo.updateHashOnlyCalls)
		}
	})

	t.Run("unparseable stored hash is a no-op", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		user := &models.User{ID: 7, PasswordHash: "not-a-bcrypt-hash"}
		NewAuthService(repo).rehashPasswordIfStale(context.Background(), user, "StrongPass1")
		if repo.updateHashOnlyCalls != 0 {
			t.Fatalf("expected no write for unparseable hash, got %d", repo.updateHashOnlyCalls)
		}
	})

	t.Run("password bcrypt cannot hash is a no-op", func(t *testing.T) {
		repo := &stubAuthUserRepo{}
		user := &models.User{ID: 7, PasswordHash: string(legacyHash)}
		tooLong := strings.Repeat("a", 80) // bcrypt hard-fails above 72 bytes
		NewAuthService(repo).rehashPasswordIfStale(context.Background(), user, tooLong)
		if repo.updateHashOnlyCalls != 0 {
			t.Fatalf("expected no write when re-hashing fails, got %d", repo.updateHashOnlyCalls)
		}
	})
}

// TestAuthenticateCredentialsRehashFailureDoesNotFailLogin proves the rehash is
// best-effort: a write error from UpdatePasswordHashOnly is swallowed and the
// login still succeeds.
func TestAuthenticateCredentialsRehashFailureDoesNotFailLogin(t *testing.T) {
	legacyHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo := &stubAuthUserRepo{
		updateHashOnlyErr: context.DeadlineExceeded,
		findByEmailUser: models.User{
			ID:               77,
			Email:            "login@example.com",
			PasswordHash:     string(legacyHash),
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials(context.Background(), "login@example.com", "StrongPass1")
	if err != nil {
		t.Fatalf("expected login to succeed despite rehash write error, got %v", err)
	}
	if repo.updateHashOnlyCalls != 1 {
		t.Fatalf("expected the rehash write to be attempted once, got %d", repo.updateHashOnlyCalls)
	}
	// On write failure the returned struct keeps the legacy hash (no phantom
	// in-memory upgrade that never hit the DB).
	if got := mustBcryptCost(t, user.PasswordHash); got != bcrypt.DefaultCost {
		t.Fatalf("returned hash cost = %d, want unchanged %d after failed write", got, bcrypt.DefaultCost)
	}
}
