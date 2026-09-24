package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// The opportunistic rehash reads the hash, proves the password against it,
// spends a cost-12 bcrypt, and only then writes. A password change or reset
// that lands inside that window must win: an unconditional write would put
// the old password back over it without a session-version bump, so the
// credential the owner just replaced would authenticate again. These cases
// drive the REAL repository on both drivers and place a real password change
// at a chosen point around the rehash write.

const (
	rehashRaceOldPassword = "LegacyPass1!"
	rehashRaceNewPassword = "ChangedPass2!"
)

// interleavingRehashRepo is the real repository with one real credential
// write run just before or just after the rehash write. Each hook fires once:
// the logins that verify the outcome must not replay it.
type interleavingRehashRepo struct {
	*db.UserRepository
	beforeUpgrade func()
	afterUpgrade  func()
}

func (repo *interleavingRehashRepo) UpgradePasswordHashCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string) (bool, error) {
	if hook := repo.beforeUpgrade; hook != nil {
		repo.beforeUpgrade = nil
		hook()
	}
	applied, err := repo.UserRepository.UpgradePasswordHashCAS(ctx, userID, oldPasswordHash, newPasswordHash)
	if hook := repo.afterUpgrade; hook != nil {
		repo.afterUpgrade = nil
		hook()
	}
	return applied, err
}

// unconditionalRehashRepo is the pre-CAS writer, built in the test: it
// conditions the upgrade on whatever the row holds at write time, which is an
// unconditional write with extra steps.
type unconditionalRehashRepo struct {
	*interleavingRehashRepo
	database *gorm.DB
}

func (repo *unconditionalRehashRepo) UpgradePasswordHashCAS(ctx context.Context, userID uint, _ string, newPasswordHash string) (bool, error) {
	if hook := repo.beforeUpgrade; hook != nil {
		repo.beforeUpgrade = nil
		hook()
	}
	var current models.User
	if err := repo.database.First(&current, userID).Error; err != nil {
		return false, err
	}
	return repo.UserRepository.UpgradePasswordHashCAS(ctx, userID, current.PasswordHash, newPasswordHash)
}

// seedRehashRaceOwner stores rehashRaceOldPassword at bcrypt.DefaultCost, below
// passwordHashCost, so the next login attempts the upgrade.
func seedRehashRaceOwner(t *testing.T, database *gorm.DB, email string) models.User {
	t.Helper()

	legacyHash := mintBcryptHashAtCost(t, rehashRaceOldPassword, bcrypt.DefaultCost)
	return createTwoOwnerUser(t, database, email, func(user *models.User) {
		user.PasswordHash = legacyHash
		user.LocalAuthEnabled = true
		user.AuthSessionVersion = 1
	})
}

// changeRehashRaceOwnerPassword is the settings password change as it ships:
// new hash, session version bumped.
func changeRehashRaceOwnerPassword(t *testing.T, repo *db.UserRepository, userID uint) {
	t.Helper()

	newHash := mintBcryptHashAtCost(t, rehashRaceNewPassword, passwordHashCost)
	if err := repo.UpdatePasswordAndRevokeSessions(context.Background(), userID, newHash, false); err != nil {
		t.Fatalf("change password: %v", err)
	}
}

// requireOnlyNewPasswordAuthenticates checks the stored credential through the
// service, the way a later login sees it.
func requireOnlyNewPasswordAuthenticates(t *testing.T, service *AuthService, email string) {
	t.Helper()

	if _, err := service.AuthenticateCredentials(context.Background(), email, rehashRaceOldPassword); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("the replaced password still authenticates after the rehash (err %v): the upgrade restored the old credential", err)
	}
	if _, err := service.AuthenticateCredentials(context.Background(), email, rehashRaceNewPassword); err != nil {
		t.Fatalf("the new password no longer authenticates: %v", err)
	}
}

// TestRehashRace runs every interleaving against one database per driver;
// each case seeds its own owner, so the cases share the database but no row.
func TestRehashRace(t *testing.T) {
	drivers := []struct {
		name string
		open func(t *testing.T) *gorm.DB
	}{
		{"sqlite", func(t *testing.T) *gorm.DB { return newTwoOwnerIntegrationDatabase(t, "rehash-race") }},
		{"postgres", func(t *testing.T) *gorm.DB {
			return newTwoOwnerIntegrationDatabaseWithConfig(t, db.Config{
				Driver:      db.DriverPostgres,
				PostgresURL: testdb.StartPostgresDSN(t, "ovumcy_rehash_race_test"),
			})
		}},
	}
	for _, driver := range drivers {
		t.Run(driver.name, func(t *testing.T) {
			database := driver.open(t)
			t.Run("loses to a change between compare and write", func(t *testing.T) {
				testRehashLosesToAChangeBetweenCompareAndWrite(t, database)
			})
			t.Run("negative control: an unconditional upgrade restores the replaced password", func(t *testing.T) {
				testRehashRaceDetectsAnUnconditionalUpgrade(t, database)
			})
			t.Run("an upgrade before a change keeps the change", func(t *testing.T) {
				testRehashBeforeAChangeKeepsTheChange(t, database)
			})
			t.Run("positive control: an uncontended stale hash is upgraded", func(t *testing.T) {
				testRehashUpgradesAnUncontendedStaleHash(t, database)
			})
		})
	}
}

// testRehashLosesToAChangeBetweenCompareAndWrite is the race: the login has
// proven the old password against the old hash, and the change lands before
// the rehash write.
func testRehashLosesToAChangeBetweenCompareAndWrite(t *testing.T, database *gorm.DB) {
	owner := seedRehashRaceOwner(t, database, "race-between@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &interleavingRehashRepo{UserRepository: realRepo}
	repo.beforeUpgrade = func() { changeRehashRaceOwnerPassword(t, realRepo, owner.ID) }
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials(context.Background(), owner.Email, rehashRaceOldPassword)
	if err != nil {
		t.Fatalf("a login whose compare succeeded must not fail on a lost rehash: %v", err)
	}
	if user.PasswordHash != owner.PasswordHash {
		t.Fatal("the returned user carries an upgraded hash the database never stored")
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if stored.AuthSessionVersion != user.AuthSessionVersion+1 {
		t.Fatalf("stored auth_session_version = %d, want %d: the session minted from the stale read must be revoked by the change",
			stored.AuthSessionVersion, user.AuthSessionVersion+1)
	}
	requireOnlyNewPasswordAuthenticates(t, service, owner.Email)
}

// testRehashRaceDetectsAnUnconditionalUpgrade is the negative control for the
// race case: the same interleaving against a writer that ignores the verified
// hash must bring the replaced password back. If it does not, the race case is
// not measuring the predicate and its green means nothing.
func testRehashRaceDetectsAnUnconditionalUpgrade(t *testing.T, database *gorm.DB) {
	owner := seedRehashRaceOwner(t, database, "race-unconditional@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &unconditionalRehashRepo{interleavingRehashRepo: &interleavingRehashRepo{UserRepository: realRepo}, database: database}
	repo.beforeUpgrade = func() { changeRehashRaceOwnerPassword(t, realRepo, owner.ID) }
	service := NewAuthService(repo)

	if _, err := service.AuthenticateCredentials(context.Background(), owner.Email, rehashRaceOldPassword); err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}
	// Without the change the old password authenticates anyway, and the
	// control would pass without reaching the write it guards.
	if stored := readTwoOwnerUser(t, database, owner.ID); stored.AuthSessionVersion != owner.AuthSessionVersion+1 {
		t.Fatalf("auth_session_version = %d, want %d: the password change never ran, so this control proves nothing",
			stored.AuthSessionVersion, owner.AuthSessionVersion+1)
	}
	if _, err := service.AuthenticateCredentials(context.Background(), owner.Email, rehashRaceOldPassword); err != nil {
		t.Fatalf("an unconditional upgrade did not restore the replaced password (err %v): the race scenario does not reach the write it guards", err)
	}
}

// testRehashBeforeAChangeKeepsTheChange is the reverse order: the upgrade
// applies, then the change replaces it.
func testRehashBeforeAChangeKeepsTheChange(t *testing.T, database *gorm.DB) {
	owner := seedRehashRaceOwner(t, database, "race-after@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &interleavingRehashRepo{UserRepository: realRepo}
	repo.afterUpgrade = func() { changeRehashRaceOwnerPassword(t, realRepo, owner.ID) }
	service := NewAuthService(repo)

	user, err := service.AuthenticateCredentials(context.Background(), owner.Email, rehashRaceOldPassword)
	if err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}
	if got := mustBcryptCost(t, user.PasswordHash); got != passwordHashCost {
		t.Fatalf("the uncontended upgrade was not applied: returned hash cost %d, want %d", got, passwordHashCost)
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if stored.AuthSessionVersion != user.AuthSessionVersion+1 {
		t.Fatalf("stored auth_session_version = %d, want %d: bumped by the change only",
			stored.AuthSessionVersion, user.AuthSessionVersion+1)
	}
	requireOnlyNewPasswordAuthenticates(t, service, owner.Email)
}

// testRehashUpgradesAnUncontendedStaleHash is the positive control: with no
// competing write the upgrade lands, keeps the password, and revokes nothing.
// Without it, a predicate that never matches would pass both race cases.
func testRehashUpgradesAnUncontendedStaleHash(t *testing.T, database *gorm.DB) {
	owner := seedRehashRaceOwner(t, database, "no-race@example.com")
	service := NewAuthService(db.NewUserRepository(database))

	user, err := service.AuthenticateCredentials(context.Background(), owner.Email, rehashRaceOldPassword)
	if err != nil {
		t.Fatalf("AuthenticateCredentials: %v", err)
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if got := mustBcryptCost(t, stored.PasswordHash); got != passwordHashCost {
		t.Fatalf("stored hash cost = %d after an uncontended login, want %d", got, passwordHashCost)
	}
	if stored.PasswordHash != user.PasswordHash {
		t.Fatal("the returned user and the stored row disagree on the upgraded hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(rehashRaceOldPassword)) != nil {
		t.Fatal("the upgraded hash no longer verifies the password it was minted from")
	}
	if stored.AuthSessionVersion != owner.AuthSessionVersion {
		t.Fatalf("auth_session_version = %d after the upgrade, want %d unchanged", stored.AuthSessionVersion, owner.AuthSessionVersion)
	}
}
