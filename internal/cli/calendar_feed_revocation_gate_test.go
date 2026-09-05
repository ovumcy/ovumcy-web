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
// it was: unset, a volume that was never mounted, a file that disagrees with
// the database marker (what a restore that has not yet been booted against
// looks like), and a path that is set but neither half has ever recorded
// anything (the server has not booted with this fence configured yet).
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

// TestUsersDeleteAdvancesTheFenceBeforeTheRow proves the ordering the task
// names, with a journal rather than a real database: a real end-to-end run
// cannot isolate it cleanly, because DeleteAccountAndRelatedData's own
// best-effort advance (internal/db/user_repository.go's
// advanceCalendarFeedFence, called again right after the row is gone) writes
// the SAME fence a second time — so a single before/after read of the fence
// file sees a fresh token whether or not confirmOperatorFeedRevocation ran at
// all, and cannot be the proof. A journaled fake fence has no such confound:
// it records WHEN each write happened, not only that one did.
//
// "row-deleted" stands for opts.delete(service) — the account row itself,
// plus that same best-effort repository-level advance, simulated here as a
// second fence.Advance on the identical fence object, exactly as
// DeleteAccountAndRelatedData performs it on the one bootstrap.BuildRepositories
// attaches.
func TestUsersDeleteAdvancesTheFenceBeforeTheRow(t *testing.T) {
	journal := []string{}
	appState := &fakeConfirmFenceAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken},
		journal: &journal,
	}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true, journal: &journal}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	if err := confirmOperatorFeedRevocation(context.Background(), "/app/fence/calendar-feed.fence", fence, &bytes.Buffer{}); err != nil {
		t.Fatalf("confirmOperatorFeedRevocation: %v", err)
	}
	journal = append(journal, "row-deleted")
	if err := fence.Advance(context.Background()); err != nil {
		t.Fatalf("simulated post-delete Advance: %v", err)
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
