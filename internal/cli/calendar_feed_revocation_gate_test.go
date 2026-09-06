package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// TestUsersDeleteRefusesAndDeletesNothingWithoutAConfirmedFence covers the
// realistic shapes an operator's shell can be in when a fence cannot be
// confirmed, and pins that every one of them leaves the account exactly where
// it was: unset, a relative path, a volume that was never mounted, a file
// that disagrees with the database marker (what a restore that has not yet
// been booted against looks like), and a path that is set but neither half
// has ever recorded anything (the server has not booted with this fence
// configured yet).
//
// Not run with t.Parallel(): several cases call t.Setenv (directly or through
// armOperatorFence), which panics after t.Parallel.
func TestUsersDeleteRefusesAndDeletesNothingWithoutAConfirmedFence(t *testing.T) {
	cases := []struct {
		name  string
		setUp func(t *testing.T, databasePath string)
	}{
		{
			name: "unset",
			setUp: func(t *testing.T, _ string) {
				t.Setenv(security.CalendarFeedFencePathEnv, "")
			},
		},
		{
			name: "a relative path, copied out of a compose file into a shell whose working directory is not the server's",
			setUp: func(t *testing.T, _ string) {
				t.Setenv(security.CalendarFeedFencePathEnv, filepath.Join("fence", "calendar-feed.fence"))
			},
		},
		{
			name: "a fence volume that was never mounted",
			setUp: func(t *testing.T, _ string) {
				t.Setenv(security.CalendarFeedFencePathEnv, filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence"))
			},
		},
		{
			name: "the file and the database marker disagree",
			setUp: func(t *testing.T, databasePath string) {
				fencePath := armOperatorFence(t, databasePath)
				if err := os.WriteFile(fencePath, []byte("a-different-generation\n"), 0o600); err != nil {
					t.Fatalf("rewrite fence file: %v", err)
				}
			},
		},
		{
			name: "neither half has ever recorded a token",
			setUp: func(t *testing.T, _ string) {
				t.Setenv(security.CalendarFeedFencePathEnv, filepath.Join(t.TempDir(), "calendar-feed.fence"))
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := createCLIUsersDatabase(t)
			createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
			testCase.setUp(t, databasePath)

			var output bytes.Buffer
			err := runUsersCommand(
				db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
				[]string{"delete", "owner@example.com", "--yes"},
				strings.NewReader(""),
				&output,
			)
			if err == nil {
				t.Fatal("expected the deletion to be refused")
			}
			if !strings.Contains(err.Error(), "Nothing was changed") {
				t.Fatalf("expected the refusal to say nothing was changed, got %v", err)
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

// TestResetPasswordRefusesWithoutAConfirmedFenceAndLeavesTheHashAlone pins the
// same refusal for the forced reset path, and — because the fence check now
// runs before ForceResetPassword* rather than only warning after a successful
// one — that the stored password hash and must_change_password are both
// exactly where the account left them.
func TestResetPasswordRefusesWithoutAConfirmedFenceAndLeavesTheHashAlone(t *testing.T) {
	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-unfenced@example.com", "StrongPass1")
	t.Setenv(security.CalendarFeedFencePathEnv, "")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-unfenced@example.com"},
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
