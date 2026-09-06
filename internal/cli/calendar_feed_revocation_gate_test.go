package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// TestUsersDeleteRefusesAndDeletesNothingWithoutAConfirmedFence covers the
// realistic shapes an operator's shell can be in when a fence cannot be
// confirmed, and pins for each one BOTH halves of the refusal: that the
// account is exactly where it was, and that the message names this state
// rather than a neighbouring one. Asserting only "Nothing was changed" would
// pass with every case rendering the same sentence, which is the failure mode
// the per-state texts exist to prevent — two of these shapes previously shared
// a state, and the suite was green.
//
// Each case supplies its own fence path rather than an environment variable,
// so all of them run in parallel.
func TestUsersDeleteRefusesAndDeletesNothingWithoutAConfirmedFence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// fencePath returns the path the command is given. It runs after the
		// account exists, so a case can arm the fence against that database
		// first and then disturb one half of it.
		fencePath  func(t *testing.T, databasePath string) string
		wantState  string
		wantRemedy string
	}{
		{
			name:       "unset",
			fencePath:  func(*testing.T, string) string { return "" },
			wantState:  "is not set in this shell",
			wantRemedy: "docker exec",
		},
		{
			name: "a relative path, copied out of a compose file into a shell whose working directory is not the server's",
			fencePath: func(*testing.T, string) string {
				return filepath.Join("fence", "calendar-feed.fence")
			},
			wantState:  "a relative path that resolves against this process's own working directory",
			wantRemedy: "Reconfigure the SERVER",
		},
		{
			// The database is armed FIRST: an unmounted volume with nothing
			// recorded anywhere is the never-armed shape below, not this one.
			// Without the arming, this case and that one produced the same
			// refusal and the difference between them went untested.
			name: "the database is armed but the fence volume was never mounted",
			fencePath: func(t *testing.T, databasePath string) string {
				armOperatorFence(t, databasePath)
				return filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence")
			},
			wantState:  "no fence value is visible from this process",
			wantRemedy: "disarms every armed calendar feed on the instance and rewrites both halves",
		},
		{
			// A directory, not a file: the one Read failure an operator can
			// actually produce on a path that exists. A missing directory is
			// reported as absent, never as a read failure, so it cannot reach
			// this state — which is why the old text's "whose directory does
			// not exist" described something unreachable.
			name: "the path names a directory rather than a fence file",
			fencePath: func(t *testing.T, _ string) string {
				return t.TempDir()
			},
			wantState:  "cannot read or write",
			wantRemedy: "docker exec",
		},
		{
			name: "the file and the database marker disagree",
			fencePath: func(t *testing.T, databasePath string) string {
				fencePath := armOperatorFence(t, databasePath)
				if err := os.WriteFile(fencePath, []byte("a-different-generation\n"), 0o600); err != nil {
					t.Fatalf("rewrite fence file: %v", err)
				}
				return fencePath
			},
			wantState:  "hold different tokens",
			wantRemedy: "Start the server once",
		},
		{
			name: "neither half has ever recorded a token",
			fencePath: func(t *testing.T, _ string) string {
				return filepath.Join(t.TempDir(), "calendar-feed.fence")
			},
			wantState:  "has ever recorded a marker",
			wantRemedy: "writable fence",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			databasePath := createCLIUsersDatabase(t)
			createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
			fencePath := testCase.fencePath(t, databasePath)

			var output bytes.Buffer
			err := runUsersCommand(
				db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
				[]string{"delete", "owner@example.com", "--yes"},
				fencePath,
				strings.NewReader(""),
				&output,
			)
			if err == nil {
				t.Fatal("expected the deletion to be refused")
			}
			for _, want := range []string{testCase.wantState, testCase.wantRemedy, "Nothing was changed"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("expected the refusal to contain %q, got %v", want, err)
				}
			}
			if remaining := listCLIUserEmails(t, databasePath); len(remaining) != 1 || remaining[0] != "owner@example.com" {
				t.Fatalf("expected the account to survive a refused revocation, got %#v", remaining)
			}
		})
	}
}

// journalingUserRepository wraps a real *db.UserRepository and journals
// exactly one moment: the instant DeleteAccountAndRelatedData is invoked. It
// promotes every other OperatorUserRepository method unchanged, so
// runUsersDelete's own resolve/confirm/delete sequence runs against a real
// SQLite-backed row throughout — only the row write itself is instrumented.
type journalingUserRepository struct {
	*db.UserRepository
	journal *[]string
}

func (repo *journalingUserRepository) DeleteAccountAndRelatedData(ctx context.Context, userID uint) error {
	*repo.journal = append(*repo.journal, "row-deleted")
	return repo.UserRepository.DeleteAccountAndRelatedData(ctx, userID)
}

// TestUsersDeleteAdvancesTheFenceBeforeTheRow drives the REAL runUsersDelete
// end to end against a real SQLite row, through a fence whose halves are
// fakes only so the test can journal WHEN each write happened, not only that
// it did. The row write itself is journaled by journalingUserRepository
// above rather than by a hand-inserted marker between two direct calls,
// because a hand-inserted marker cannot go red when runUsersDelete's own
// internal ordering changes — only a wrapper actually sitting on the call
// runUsersDelete makes can.
//
// DeleteAccountAndRelatedData's own best-effort post-op advance
// (internal/db/user_repository.go's advanceCalendarFeedFence, called again
// right after the row is gone) writes the SAME fence a second time, through
// the calendarFeedFence the journalingUserRepository's embedded
// *db.UserRepository was built with — so the expected journal captures BOTH
// advances, one on each side of the row write.
func TestUsersDeleteAdvancesTheFenceBeforeTheRow(t *testing.T) {
	journal := []string{}
	appState := &fakeConfirmFenceAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken},
		journal: &journal,
	}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true, journal: &journal}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	databasePath := createCLIUsersDatabase(t)
	created := createCLIUsersUser(t, databasePath, "fence-order@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())

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
	users := &journalingUserRepository{UserRepository: repositories.Users, journal: &journal}
	service := services.NewOperatorUserService(users, services.NewAuthService(users.UserRepository))

	if err := runUsersDelete(service, "/app/fence/calendar-feed.fence", fence, []string{"fence-order@example.com", "--yes"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("runUsersDelete: %v", err)
	}

	want := []string{"anchor", "app_state", "row-deleted", "anchor", "app_state"}
	if len(journal) != len(want) {
		t.Fatalf("expected the fence to advance before the row write, got %v, want %v", journal, want)
	}
	for i := range want {
		if journal[i] != want[i] {
			t.Fatalf("expected the fence to advance before the row write, got %v, want %v", journal, want)
		}
	}

	if remaining := listCLIUserEmails(t, databasePath); len(remaining) != 0 {
		t.Fatalf("expected the account to be gone after a confirmed revocation, got %#v (id=%d)", remaining, created.ID)
	}
}

// journalingPasswordResetRepository is journalingUserRepository's counterpart
// for the reset-password path: it journals exactly the instant
// ForceResetPasswordAndRevokeSessions is invoked, which is the credential
// write runResetPassword performs, and promotes every other method from the
// real *db.UserRepository it wraps unchanged.
type journalingPasswordResetRepository struct {
	*db.UserRepository
	journal *[]string
}

func (repo *journalingPasswordResetRepository) ForceResetPasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string) error {
	*repo.journal = append(*repo.journal, "row-write")
	return repo.UserRepository.ForceResetPasswordAndRevokeSessions(ctx, userID, passwordHash)
}

// TestResetPasswordAdvancesTheFenceBeforeTheRow is
// TestUsersDeleteAdvancesTheFenceBeforeTheRow's reset-password counterpart,
// driving the REAL runResetPassword — the seam runResetPasswordCommand splits
// out for exactly this reason — against a real SQLite row, through a fence
// whose halves are fakes only so the test can journal WHEN each write
// happened.
//
// ForceResetPasswordAndRevokeSessions carries the same best-effort
// post-op advance DeleteAccountAndRelatedData does (both call
// advanceCalendarFeedFenceBestEffort), so the expected journal has the same
// shape: the gate's own advance, the credential write, then that write's own
// advance.
func TestResetPasswordAdvancesTheFenceBeforeTheRow(t *testing.T) {
	journal := []string{}
	appState := &fakeConfirmFenceAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken},
		journal: &journal,
	}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true, journal: &journal}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "reset-fence-order@example.com", "StrongPass1")

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
	users := &journalingPasswordResetRepository{UserRepository: repositories.Users, journal: &journal}
	authService := services.NewAuthService(users)
	operatorUsers := services.NewOperatorUserService(users, authService)

	opts := resetPasswordOptions{email: "reset-fence-order@example.com"}
	if err := runResetPassword(operatorUsers, authService, "/app/fence/calendar-feed.fence", fence, opts, "reset-fence-order@example.com", "EvenStronger2", &bytes.Buffer{}); err != nil {
		t.Fatalf("runResetPassword: %v", err)
	}

	want := []string{"anchor", "app_state", "row-write", "anchor", "app_state"}
	if len(journal) != len(want) {
		t.Fatalf("expected the fence to advance before the row write, got %v, want %v", journal, want)
	}
	for i := range want {
		if journal[i] != want[i] {
			t.Fatalf("expected the fence to advance before the row write, got %v, want %v", journal, want)
		}
	}

	if anchor.written == "" || anchor.written == confirmFenceTestToken {
		t.Fatalf("expected a fresh token to have been minted, got %q", anchor.written)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != anchor.written {
		t.Fatalf("expected both halves to hold the same fresh token, file %q app_state %q", anchor.written, got)
	}

	updated := loadCLIResetUser(t, databasePath, "reset-fence-order@example.com")
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("EvenStronger2")) != nil {
		t.Fatal("expected the stored hash to reflect the reset")
	}
	if !updated.MustChangePassword {
		t.Fatal("expected must_change_password to be set by a forced reset")
	}
}

// TestResetPasswordRefusesWithoutAConfirmedFenceAndLeavesTheHashAlone pins the
// same refusal for the forced reset path, and — because the fence check now
// runs before ForceResetPassword* rather than only warning after a successful
// one — that the stored password hash and must_change_password are both
// exactly where the account left them.
func TestResetPasswordRefusesWithoutAConfirmedFenceAndLeavesTheHashAlone(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-unfenced@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-unfenced@example.com"},
		"",
		func() ([]byte, error) { return []byte("EvenStronger2"), nil },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("expected the reset to be refused, got %v", err)
	}

	unchanged := loadCLIResetUser(t, databasePath, "cli-reset-unfenced@example.com")
	if bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatal("expected the stored password hash to be untouched by a refused reset")
	}
	if unchanged.MustChangePassword {
		t.Fatal("expected must_change_password to be untouched by a refused reset")
	}
}

// TestResetPasswordReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced is the
// outcome that follows from confirming the fence BEFORE the credential write
// rather than after it: the fence advance is one-shot, so a write that fails
// once the gate has passed leaves the account's password where it was while
// every armed calendar feed on the instance is already revoked. The success
// line on the command's own output is the operator's only record of that
// half-done state, so it is asserted here rather than left implied — a run
// whose output is silent and whose password is unchanged is indistinguishable
// from a refusal.
//
// The error the operator gets must say the same thing the output line does:
// a bare "reset password: ..." after a gate that already spent its one-shot
// advance reads as "the command did nothing", and the operator's next move —
// whether to re-run at all — depends on knowing otherwise.
func TestResetPasswordReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-write-refused@example.com", "StrongPass1")
	fencePath := armOperatorFence(t, databasePath)
	refuseCLIPasswordHashWrites(t, databasePath)

	var output bytes.Buffer
	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-write-refused@example.com"},
		fencePath,
		func() ([]byte, error) { return []byte("EvenStronger2"), nil },
		&output,
	)

	if !errors.Is(err, services.ErrAuthPasswordUpdate) {
		t.Fatalf("expected the refused write to reach the operator as ErrAuthPasswordUpdate, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "reset password: ") {
		t.Fatalf("expected mapResetPasswordError's wording around the write failure, got %v", err)
	}
	for _, want := range []string{
		"restore fence was already advanced",
		"the account itself was not changed",
		"Re-run the command",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("a write that failed past the gate must say %q, got %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("the fence advance already happened, so this is not a refusal: %v", err)
	}

	unchanged := loadCLIResetUser(t, databasePath, "cli-reset-write-refused@example.com")
	if bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatal("expected the stored password hash to survive a refused write")
	}
	if unchanged.MustChangePassword {
		t.Fatal("expected must_change_password to survive a refused write")
	}

	if !strings.Contains(output.String(), "fence advanced at "+fencePath) {
		t.Fatalf("the burned fence generation must still be reported, got %q", output.String())
	}
}

// refuseCLIPasswordHashWrites installs a SQLite trigger that aborts any UPDATE
// carrying password_hash — the single statement
// ForceResetPasswordAndRevokeSessions issues — so the credential write fails
// while the target resolve and the fence advance ahead of it still succeed.
// A trigger rather than a repository double because runResetPasswordCommand
// builds its own repositories from the db.Config it is handed, and no other
// write in the command names that column.
func refuseCLIPasswordHashWrites(t *testing.T, databasePath string) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("refuseCLIPasswordHashWrites: open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("refuseCLIPasswordHashWrites: open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := database.Exec(
		"CREATE TRIGGER refuse_password_hash_writes BEFORE UPDATE OF password_hash ON users " +
			"BEGIN SELECT RAISE(ABORT, 'password_hash writes are refused by this test'); END",
	).Error; err != nil {
		t.Fatalf("refuseCLIPasswordHashWrites: create trigger: %v", err)
	}
}

// TestUsersDeleteReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced is the
// `users delete` half of the post-gate outcome the reset path already pins.
// The row vanishes at the one seam where a real operator's can: while the
// command is blocked reading the typed DELETE confirmation, which sits between
// the resolve and the fence gate. The gate then spends its one-shot advance,
// and the delete finds nothing.
//
// The operator must be told both facts. The account is untouched — there is
// nothing left to touch — but the fence DID move, so a bare "no account
// carries id N" would read as a command that changed nothing at all.
func TestUsersDeleteReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "vanishes@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "--id", strconv.FormatUint(uint64(user.ID), 10)},
		fencePath,
		&deleteRowWhileConfirming{t: t, databasePath: databasePath, userID: user.ID},
		&output,
	)
	if err == nil {
		t.Fatal("expected the delete to fail: the row is gone by the time it runs")
	}
	for _, want := range []string{
		fmt.Sprintf("no account carries id %d", user.ID),
		"restore fence was already advanced",
		"the account itself was not changed",
		"Re-run the command",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("a delete that failed past the gate must say %q, got %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("the fence advance already happened, so this is not a refusal: %v", err)
	}
}

// deleteRowWhileConfirming removes the account the moment the command asks for
// its typed confirmation, then answers DELETE. Reading the confirmation is a
// real blocking step between the resolve and the write, so this reproduces the
// window without reaching inside runUsersDelete.
type deleteRowWhileConfirming struct {
	t            *testing.T
	databasePath string
	userID       uint
	answer       *strings.Reader
}

func (reader *deleteRowWhileConfirming) Read(buffer []byte) (int, error) {
	if reader.answer == nil {
		reader.t.Helper()
		database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: reader.databasePath})
		if err != nil {
			reader.t.Fatalf("deleteRowWhileConfirming: open sqlite: %v", err)
		}
		sqlDB, err := database.DB()
		if err != nil {
			reader.t.Fatalf("deleteRowWhileConfirming: open sql db: %v", err)
		}
		defer func() { _ = sqlDB.Close() }()
		if err := database.Exec("DELETE FROM users WHERE id = ?", reader.userID).Error; err != nil {
			reader.t.Fatalf("deleteRowWhileConfirming: delete row: %v", err)
		}
		reader.answer = strings.NewReader("DELETE\n")
	}
	return reader.answer.Read(buffer)
}

// TestUsersDeleteRefusesAHalfAdvancedFenceOnARealAppState drives the
// half-advanced refusal through a REAL SQLite app_state rather than a double:
// the fence file is written, the marker upsert is refused by a trigger, and
// the command must report the file half as already moved and leave the account
// standing.
//
// It is the shape a full disk or a locked database produces on a live
// instance, and it is the one refusal whose message must NOT end "Nothing was
// changed" — so it is worth proving against the real repository rather than
// only against a fake whose Set returns an error on request.
func TestUsersDeleteRefusesAHalfAdvancedFenceOnARealAppState(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "half-advanced@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)

	armed, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("read the armed fence file: %v", err)
	}
	refuseCLIAppStateWrites(t, databasePath)

	var output bytes.Buffer
	err = runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "half-advanced@example.com", "--yes"},
		fencePath,
		strings.NewReader(""),
		&output,
	)
	if err == nil {
		t.Fatal("expected the deletion to be refused")
	}
	if !errors.Is(err, services.ErrCalendarFeedFenceHalfAdvanced) {
		t.Fatalf("expected the half-advanced sentinel, got %v", err)
	}
	if strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("the fence file moved, so the refusal must not claim nothing changed: %v", err)
	}
	for _, want := range []string{fencePath, "next start disarms every armed calendar feed", "The account was not changed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the half-advanced refusal must contain %q, got %v", want, err)
		}
	}

	after, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("read the fence file after the refusal: %v", err)
	}
	if string(after) == string(armed) {
		t.Fatal("the file half must have moved: without that this is an ordinary refusal, not the half-advanced one")
	}
	if remaining := listCLIUserEmails(t, databasePath); len(remaining) != 1 {
		t.Fatalf("expected the account to survive a refused revocation, got %#v", remaining)
	}
}

// refuseCLIAppStateWrites makes the database half of the fence unwritable
// while leaving it readable, which is what a full disk or a refused write
// looks like from AdvanceConfirmed. AppStateRepository.Set upserts, so both
// verbs have to be refused.
func refuseCLIAppStateWrites(t *testing.T, databasePath string) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("refuseCLIAppStateWrites: open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("refuseCLIAppStateWrites: open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	for _, verb := range []string{"INSERT", "UPDATE"} {
		statement := "CREATE TRIGGER refuse_app_state_" + strings.ToLower(verb) + " BEFORE " + verb + " ON app_state " +
			"BEGIN SELECT RAISE(ABORT, 'app_state writes are refused by this test'); END"
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("refuseCLIAppStateWrites: create %s trigger: %v", verb, err)
		}
	}
}
