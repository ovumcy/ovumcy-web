package services

import (
	"context"
	"encoding/base32"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// The lazy re-encryption opens a legacy TOTP ciphertext, checks the code,
// claims the step, and only then writes the resealed secret. A re-enrollment
// that lands inside that window must win: an unconditional write would put the
// replaced secret back without a session-version bump, so the authenticator
// the owner just retired would pass 2FA again. These cases drive the REAL
// repository on both drivers and place a real re-enrollment at a chosen point
// around the re-encryption write.

// reencryptRaceReEnrolledSeed is the re-enrolled authenticator's base32
// secret: the RFC 6238 test seed. It is built rather than written as a literal
// because a base32 string beside a secret-named identifier trips the secret
// scanner's generic rule.
var reencryptRaceReEnrolledSeed = base32.StdEncoding.EncodeToString([]byte("12345678901234567890"))

// interleavingReencryptRepo is the real repository with one real re-enrollment
// run just before or just after the re-encryption write, and the write's
// outcome recorded. Each hook fires once.
type interleavingReencryptRepo struct {
	*db.UserRepository
	beforeUpgrade func()
	afterUpgrade  func()
	upgraded      *bool
}

func (repo *interleavingReencryptRepo) UpgradeTOTPSecretCiphertextCAS(ctx context.Context, userID uint, oldCiphertext string, newCiphertext string) (bool, error) {
	if hook := repo.beforeUpgrade; hook != nil {
		repo.beforeUpgrade = nil
		hook()
	}
	applied, err := repo.UserRepository.UpgradeTOTPSecretCiphertextCAS(ctx, userID, oldCiphertext, newCiphertext)
	// A failed write leaves upgraded nil: ValidateCode swallows the error, so
	// only this record tells a lost race from a broken predicate.
	if err == nil {
		repo.upgraded = &applied
	}
	if hook := repo.afterUpgrade; hook != nil {
		repo.afterUpgrade = nil
		hook()
	}
	return applied, err
}

// unconditionalReencryptRepo is the pre-CAS writer, built in the test: it
// conditions the upgrade on whatever the row holds at write time, which is an
// unconditional write with extra steps.
type unconditionalReencryptRepo struct {
	*interleavingReencryptRepo
	database *gorm.DB
}

func (repo *unconditionalReencryptRepo) UpgradeTOTPSecretCiphertextCAS(ctx context.Context, userID uint, _ string, newCiphertext string) (bool, error) {
	if hook := repo.beforeUpgrade; hook != nil {
		repo.beforeUpgrade = nil
		hook()
	}
	var current models.User
	if err := repo.database.First(&current, userID).Error; err != nil {
		return false, err
	}
	return repo.UserRepository.UpgradeTOTPSecretCiphertextCAS(ctx, userID, current.TOTPSecret, newCiphertext)
}

// seedReencryptRaceOwner stores the recorded pre-aad ciphertext, so the next
// successful code check attempts the re-encryption.
func seedReencryptRaceOwner(t *testing.T, database *gorm.DB, email string) models.User {
	t.Helper()

	return createTwoOwnerUser(t, database, email, func(user *models.User) {
		user.LocalAuthEnabled = true
		user.TOTPSecret = legacyTOTPCiphertext
		user.TOTPEnabled = true
		user.AuthSessionVersion = 1
	})
}

// reEnrollReencryptRaceOwner is the settings re-enrollment as it ships: new
// secret sealed for this user, step reset, session version bumped.
func reEnrollReencryptRaceOwner(t *testing.T, repo *db.UserRepository, userID uint) {
	t.Helper()

	current, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("load the owner before re-enrolling: %v", err)
	}
	if err := NewTOTPService(repo, []byte(legacyTOTPSecretKey), nil).EnableTOTP(context.Background(), userID, current.AuthSessionVersion, reencryptRaceReEnrolledSeed); err != nil {
		t.Fatalf("re-enroll: %v", err)
	}
}

func currentTOTPCode(t *testing.T, rawSecret string) string {
	t.Helper()

	code, err := totp.GenerateCode(rawSecret, nowForTOTPTest())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return code
}

// validateAgainstStoredSecret runs the 2FA check the way a later login does:
// against the ciphertext the row holds now.
func validateAgainstStoredSecret(t *testing.T, service *TOTPService, database *gorm.DB, userID uint, rawSecret string) (bool, error) {
	t.Helper()

	stored := readTwoOwnerUser(t, database, userID)
	return service.ValidateCode(context.Background(), userID, stored.TOTPSecret, currentTOTPCode(t, rawSecret))
}

func requireOnlyReEnrolledSecretPasses(t *testing.T, service *TOTPService, database *gorm.DB, userID uint) {
	t.Helper()

	if valid, err := validateAgainstStoredSecret(t, service, database, userID, legacyTOTPRawSecret); err != nil || valid {
		t.Fatalf("the replaced authenticator still passes after the re-encryption (valid %v, err %v): the upgrade restored the old secret", valid, err)
	}
	if valid, err := validateAgainstStoredSecret(t, service, database, userID, reencryptRaceReEnrolledSeed); err != nil || !valid {
		t.Fatalf("the re-enrolled authenticator no longer passes (valid %v, err %v)", valid, err)
	}
}

// TestTOTPReencryptRace runs every interleaving against one database per
// driver; each case seeds its own owner, so the cases share the database but
// no row.
func TestTOTPReencryptRace(t *testing.T) {
	drivers := []struct {
		name string
		open func(t *testing.T) *gorm.DB
	}{
		{"sqlite", func(t *testing.T) *gorm.DB { return newTwoOwnerIntegrationDatabase(t, "totp-reencrypt-race") }},
		{"postgres", func(t *testing.T) *gorm.DB {
			return newTwoOwnerIntegrationDatabaseWithConfig(t, db.Config{
				Driver:      db.DriverPostgres,
				PostgresURL: testdb.StartPostgresDSN(t, "ovumcy_totp_reencrypt_race_test"),
			})
		}},
	}
	for _, driver := range drivers {
		t.Run(driver.name, func(t *testing.T) {
			database := driver.open(t)
			t.Run("loses to a re-enrollment between check and write", func(t *testing.T) {
				testReencryptLosesToAReEnrollmentBetweenCheckAndWrite(t, database)
			})
			t.Run("negative control: an unconditional upgrade restores the replaced secret", func(t *testing.T) {
				testReencryptRaceDetectsAnUnconditionalUpgrade(t, database)
			})
			t.Run("an upgrade before a re-enrollment keeps the re-enrollment", func(t *testing.T) {
				testReencryptBeforeAReEnrollmentKeepsIt(t, database)
			})
			t.Run("positive control: an uncontended legacy secret is resealed", func(t *testing.T) {
				testReencryptResealsAnUncontendedLegacySecret(t, database)
			})
		})
	}
}

// testReencryptLosesToAReEnrollmentBetweenCheckAndWrite is the race: the check
// has opened the legacy ciphertext and claimed the step, and the re-enrollment
// lands before the re-encryption write.
func testReencryptLosesToAReEnrollmentBetweenCheckAndWrite(t *testing.T, database *gorm.DB) {
	owner := seedReencryptRaceOwner(t, database, "totp-race-between@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &interleavingReencryptRepo{UserRepository: realRepo}
	repo.beforeUpgrade = func() { reEnrollReencryptRaceOwner(t, realRepo, owner.ID) }
	service := NewTOTPService(repo, []byte(legacyTOTPSecretKey), nil)

	valid, err := service.ValidateCode(context.Background(), owner.ID, owner.TOTPSecret, currentTOTPCode(t, legacyTOTPRawSecret))
	if err != nil || !valid {
		t.Fatalf("a check whose code matched must not fail on a lost re-encryption: valid %v, err %v", valid, err)
	}
	if repo.upgraded == nil || *repo.upgraded {
		t.Fatal("the re-encryption applied over the re-enrolled secret, never ran, or failed")
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if stored.AuthSessionVersion != owner.AuthSessionVersion+1 {
		t.Fatalf("stored auth_session_version = %d, want %d: the session minted from the stale read must be revoked by the re-enrollment",
			stored.AuthSessionVersion, owner.AuthSessionVersion+1)
	}
	requireOnlyReEnrolledSecretPasses(t, service, database, owner.ID)
}

// testReencryptRaceDetectsAnUnconditionalUpgrade is the negative control for
// the race case: the same interleaving against a writer that ignores the
// opened ciphertext must bring the replaced secret back. If it does not, the
// race case is not measuring the predicate and its green means nothing.
func testReencryptRaceDetectsAnUnconditionalUpgrade(t *testing.T, database *gorm.DB) {
	owner := seedReencryptRaceOwner(t, database, "totp-race-unconditional@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &unconditionalReencryptRepo{interleavingReencryptRepo: &interleavingReencryptRepo{UserRepository: realRepo}, database: database}
	repo.beforeUpgrade = func() { reEnrollReencryptRaceOwner(t, realRepo, owner.ID) }
	service := NewTOTPService(repo, []byte(legacyTOTPSecretKey), nil)

	if valid, err := service.ValidateCode(context.Background(), owner.ID, owner.TOTPSecret, currentTOTPCode(t, legacyTOTPRawSecret)); err != nil || !valid {
		t.Fatalf("ValidateCode: valid %v, err %v", valid, err)
	}
	// Without the re-enrollment the old secret passes anyway, and the control
	// would pass without reaching the write it guards.
	if stored := readTwoOwnerUser(t, database, owner.ID); stored.AuthSessionVersion != owner.AuthSessionVersion+1 {
		t.Fatalf("auth_session_version = %d, want %d: the re-enrollment never ran, so this control proves nothing",
			stored.AuthSessionVersion, owner.AuthSessionVersion+1)
	}
	if valid, err := validateAgainstStoredSecret(t, service, database, owner.ID, legacyTOTPRawSecret); err != nil || !valid {
		t.Fatalf("an unconditional upgrade did not restore the replaced secret (valid %v, err %v): the race scenario does not reach the write it guards", valid, err)
	}
}

// testReencryptBeforeAReEnrollmentKeepsIt is the reverse order: the upgrade
// applies, then the re-enrollment replaces it.
func testReencryptBeforeAReEnrollmentKeepsIt(t *testing.T, database *gorm.DB) {
	owner := seedReencryptRaceOwner(t, database, "totp-race-after@example.com")
	realRepo := db.NewUserRepository(database)
	repo := &interleavingReencryptRepo{UserRepository: realRepo}
	repo.afterUpgrade = func() { reEnrollReencryptRaceOwner(t, realRepo, owner.ID) }
	service := NewTOTPService(repo, []byte(legacyTOTPSecretKey), nil)

	if valid, err := service.ValidateCode(context.Background(), owner.ID, owner.TOTPSecret, currentTOTPCode(t, legacyTOTPRawSecret)); err != nil || !valid {
		t.Fatalf("ValidateCode: valid %v, err %v", valid, err)
	}
	if repo.upgraded == nil || !*repo.upgraded {
		t.Fatal("the uncontended re-encryption was not applied, never ran, or failed")
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if stored.AuthSessionVersion != owner.AuthSessionVersion+1 {
		t.Fatalf("stored auth_session_version = %d, want %d: bumped by the re-enrollment only",
			stored.AuthSessionVersion, owner.AuthSessionVersion+1)
	}
	requireOnlyReEnrolledSecretPasses(t, service, database, owner.ID)
}

// testReencryptResealsAnUncontendedLegacySecret is the positive control: with
// no competing write the upgrade lands, keeps the secret, and revokes nothing.
// Without it, a predicate that never matches would pass both race cases.
func testReencryptResealsAnUncontendedLegacySecret(t *testing.T, database *gorm.DB) {
	owner := seedReencryptRaceOwner(t, database, "totp-no-race@example.com")
	service := NewTOTPService(db.NewUserRepository(database), []byte(legacyTOTPSecretKey), nil)

	if valid, err := service.ValidateCode(context.Background(), owner.ID, owner.TOTPSecret, currentTOTPCode(t, legacyTOTPRawSecret)); err != nil || !valid {
		t.Fatalf("ValidateCode: valid %v, err %v", valid, err)
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	raw, isLegacy, err := security.DecryptField(stored.TOTPSecret, []byte(legacyTOTPSecretKey), aadForTOTPSecret(owner.ID))
	if err != nil {
		t.Fatalf("the resealed secret does not open under this owner's aad: %v", err)
	}
	if isLegacy {
		t.Fatal("the stored secret is still the legacy ciphertext after an uncontended check")
	}
	if raw != legacyTOTPRawSecret {
		t.Fatal("the resealed secret is not the secret it was resealed from")
	}
	if stored.AuthSessionVersion != owner.AuthSessionVersion || !stored.TOTPEnabled {
		t.Fatalf("auth_session_version = %d, totp_enabled = %v after the upgrade, want %d, true unchanged",
			stored.AuthSessionVersion, stored.TOTPEnabled, owner.AuthSessionVersion)
	}
}
