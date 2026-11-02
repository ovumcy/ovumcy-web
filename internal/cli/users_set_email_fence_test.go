package cli

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

// changedUnderRepairUsers loses the re-home's compare-and-set, the shape of a
// row that moved between the operator's listing and the write.
type changedUnderRepairUsers struct {
	*db.UserRepository
}

func (changedUnderRepairUsers) SetUserEmailByIDAndRevokeSessions(context.Context, uint, string, string) (bool, error) {
	return false, nil
}

// TestUsersSetEmailSaysTheFenceMovedWhenTheWriteFailsAfterTheGate pins the one
// outcome where the fence advanced and the account did not change: the message
// must say both, not only "the command did nothing".
func TestUsersSetEmailSaysTheFenceMovedWhenTheWriteFailsAfterTheGate(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)

	withNotifyRepositories(t, databasePath, fencePath, func(repositories *db.Repositories, fence *services.CalendarFeedRestoreFence) {
		service := services.NewOperatorUserService(changedUnderRepairUsers{repositories.Users}, services.NewAuthService(repositories.Users))
		err := runUsersSetEmail(service, fencePath, fence, []string{"--id", strconv.FormatUint(uint64(user.ID), 10), "renamed@example.com"}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "changed while the repair ran") || !strings.Contains(err.Error(), "fence was already advanced") {
			t.Fatalf("expected the lost compare-and-set wrapped as a committed fence advance, got %v", err)
		}
	})
}
