package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUsersCreateRefusesWithoutAConfirmedFence is WEB-538's operator half: an
// account created after a backup is dropped by the restore and its id handed
// out again, so from a shell that cannot see the server's fence the creation is
// refused with no row written rather than recorded only in the database.
func TestUsersCreateRefusesWithoutAConfirmedFence(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)

	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "owner@example.com"},
		"",
		strings.NewReader("StrongPass1\n"),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "CALENDAR_FEED_FENCE_PATH") || !strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("expected the operator fence refusal, got %v", err)
	}
	users, err := mustCLIUsersService(t, databasePath).ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("a refused creation must write no row, found %d", len(users))
	}
}

// TestUsersCreateSkipIfExistsNeedsNoFence keeps --skip-if-exists idempotent for
// install scripts: an address already taken creates nothing, so it is answered
// before the fence gate and needs no fence.
func TestUsersCreateSkipIfExistsNeedsNoFence(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "owner@example.com", "--skip-if-exists"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("expected the existing account to be skipped without a fence, got %v", err)
	}
	if !strings.Contains(output.String(), "already exists") {
		t.Fatalf("expected the skip message, got %q", output.String())
	}
}
