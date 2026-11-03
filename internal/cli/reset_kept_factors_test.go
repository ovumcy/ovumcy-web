package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// TestResetPasswordPlainNamesTheFactorsItKeeps is WEB-432's repro: a plain
// reset leaves a linked identity and two-factor sign-in in place, and a linked
// identity can replace the temporary password while the forced change is
// pending, so the output names both and points at --full. An account with
// neither gets no such line, and --full never prints it.
func TestResetPasswordPlainNamesTheFactorsItKeeps(t *testing.T) {
	for name, tc := range map[string]struct {
		seed     bool
		full     bool
		wantKept string
	}{
		"plain reset of an account with factors": {seed: true, wantKept: "Kept: 1 linked OIDC identity; two-factor sign-in; the recovery code; the webhook endpoint. If the account may be compromised, run again with --full to remove them."},
		"plain reset of an account without":      {},
		"full reset":                             {seed: true, full: true},
	} {
		databasePath := createCLIResetDatabase(t)
		createCLIResetUser(t, databasePath, "kept-factors@example.com", "StrongPass1")
		if tc.seed {
			seedSecondFactors(t, databasePath, loadCLIResetUser(t, databasePath, "kept-factors@example.com").ID, "kept-subject")
		}
		fencePath := armOperatorFence(t, databasePath)

		args := []string{"kept-factors@example.com"}
		if tc.full {
			args = append(args, "--full")
		}
		var output bytes.Buffer
		if err := runResetPasswordCommand(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}, args, fencePath, func() ([]byte, error) { return []byte("EvenStronger2"), nil }, &output); err != nil {
			t.Fatalf("%s: runResetPasswordCommand: %v", name, err)
		}
		if tc.wantKept != "" && !strings.Contains(output.String(), tc.wantKept) {
			t.Fatalf("%s: expected %q in the output, got %q", name, tc.wantKept, output.String())
		}
		if tc.wantKept == "" && strings.Contains(output.String(), "Kept:") {
			t.Fatalf("%s: expected no kept-factors line, got %q", name, output.String())
		}
	}
}

type fakeLinkedIdentityLister struct {
	identities []models.OIDCIdentity
	err        error
}

func (lister fakeLinkedIdentityLister) ListByUser(context.Context, uint) ([]models.OIDCIdentity, error) {
	return lister.identities, lister.err
}

type fakeKeptFactorUser struct {
	user  models.User
	found bool
	err   error
}

func (reader fakeKeptFactorUser) FindByIDOptional(context.Context, uint) (models.User, bool, error) {
	return reader.user, reader.found, reader.err
}

// TestPrintKeptFactorsCountsAndSurvivesAFailedRead covers the arms the command
// test cannot reach on a healthy database: several identities, and a read that
// fails after the reset has already been written, which still tells the
// operator the factor may be kept rather than staying silent.
func TestPrintKeptFactorsCountsAndSurvivesAFailedRead(t *testing.T) {
	plainUser := fakeKeptFactorUser{user: models.User{ID: 7}, found: true}
	for name, tc := range map[string]struct {
		readers keptFactorReaders
		want    string
	}{
		"several identities": {
			readers: keptFactorReaders{users: plainUser, identities: fakeLinkedIdentityLister{identities: make([]models.OIDCIdentity, 3)}},
			want:    "Kept: 3 linked OIDC identities. If the account may be compromised, run again with --full",
		},
		"identity listing failed": {
			readers: keptFactorReaders{users: plainUser, identities: fakeLinkedIdentityLister{err: errors.New("database is locked")}},
			want:    "Kept: any linked OIDC identities (they could not be listed). If the account",
		},
		"account re-read failed": {
			readers: keptFactorReaders{users: fakeKeptFactorUser{err: errors.New("database is locked")}, identities: fakeLinkedIdentityLister{}},
			want:    "Kept: any two-factor sign-in, recovery code and webhook endpoint (the account could not be re-read). If the account",
		},
	} {
		var output bytes.Buffer
		printKeptFactors(&output, tc.readers, 7)
		if !strings.Contains(output.String(), tc.want) {
			t.Fatalf("%s: expected %q, got %q", name, tc.want, output.String())
		}
	}
}

// deletingPasswordResetRepository deletes the account row just before the
// reset's own update, the narrow window between the service's re-read and the
// write, where the update matches no row.
type deletingPasswordResetRepository struct {
	*db.UserRepository
	database *gorm.DB
}

func (repo *deletingPasswordResetRepository) ForceResetPasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, clearSecondFactors bool) error {
	if err := repo.database.Delete(&models.User{}, userID).Error; err != nil {
		return err
	}
	return repo.UserRepository.ForceResetPasswordAndRevokeSessions(ctx, userID, passwordHash, clearSecondFactors)
}

// TestResetPasswordNamesAnAccountDeletedBeforeTheWriteAsNotFound pins that the
// repository's no-row refusal reaches mapResetPasswordError unflattened: the
// service wraps it with %w, so the operator gets the shared not-found wording
// rather than the generic "reset password:" fallthrough.
func TestResetPasswordNamesAnAccountDeletedBeforeTheWriteAsNotFound(t *testing.T) {
	appState := &fakeConfirmFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken}, journal: &[]string{}}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true, journal: &[]string{}}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "deleted-midway@example.com", "StrongPass1")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	repositories := db.NewRepositories(database).WithCalendarFeedFence(fence)
	users := &deletingPasswordResetRepository{UserRepository: repositories.Users, database: database}
	authService := services.NewAuthService(users)
	operatorUsers := services.NewOperatorUserService(users, authService)

	opts := resetPasswordOptions{email: "deleted-midway@example.com"}
	err = runResetPassword(operatorUsers, authService, keptFactorReaders{users: repositories.Users, identities: repositories.OIDCIdentities}, "/app/fence/calendar-feed.fence", fence, opts, "deleted-midway@example.com", "EvenStronger2", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected the reset of a row deleted before the write to fail")
	}
	if want := "user deleted-midway@example.com not found"; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("expected the shared not-found wording %q, got %q", want, err.Error())
	}
}
