package cli

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUsersSetEmailRefusesWithoutAConfirmedFence is WEB-418's operator half: the
// re-home revokes every session of the account, so from a shell that cannot see
// the server's fence it is refused with the row untouched rather than recorded
// only in the database, where a restored backup would undo it.
func TestUsersSetEmailRefusesWithoutAConfirmedFence(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	before := loadCLIUsersRow(t, databasePath, user.ID)

	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", strconv.FormatUint(uint64(user.ID), 10), "renamed@example.com"},
		"",
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "CALENDAR_FEED_FENCE_PATH") || !strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("expected the operator fence refusal, got %v", err)
	}
	after := loadCLIUsersRow(t, databasePath, user.ID)
	if after.Email != before.Email || after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("a refused re-home must leave the row untouched: before %q/%d after %q/%d", before.Email, before.AuthSessionVersion, after.Email, after.AuthSessionVersion)
	}
}
