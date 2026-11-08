package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// snapshottingFence is a seam in the fence API, not in production code: it
// wraps the real fence and takes the backup at the moment the advance hands
// its writer the token — after the fence file moved, before the writer's
// transaction opens.
type snapshottingFence struct {
	CalendarFeedFence
	snapshot func()
}

func (fence *snapshottingFence) AdvanceOrRefuse(ctx context.Context, commit func(marker string) error) error {
	return fence.CalendarFeedFence.AdvanceOrRefuse(ctx, func(marker string) error {
		fence.snapshot()
		return commit(marker)
	})
}

// TestALiveBackupTakenMidRevocationIsReadAsARestore is WEB-505's regression,
// on both dialects. A backup taken while a sign-out was committing used to
// hold the new fence marker beside the un-revoked row — the marker committed
// on its own, ahead of the row — and restoring it read as continuity: no
// revocation, the old cookie and password live again. The marker now commits
// in the row's own transaction, so a backup taken after the file write and
// before that transaction holds the OLD marker against the new file and the
// boot revokes; one taken after the transaction holds both halves at the new
// token beside the revoked row, and the boot finds continuity.
func TestALiveBackupTakenMidRevocationIsReadAsARestore(t *testing.T) {
	dialects := map[string]func(t *testing.T) *gorm.DB{
		"sqlite": func(t *testing.T) *gorm.DB {
			return openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "intermediate-snapshot.db"))
		},
		"postgres": func(t *testing.T) *gorm.DB {
			return openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		},
	}
	for dialect, open := range dialects {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()
			database := open(t)
			repositories := NewRepositories(database)
			file := security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))
			fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
			var backup restoreSnapshot
			var user = createUserForTimezoneTest(t, repositories.Users, "intermediate@example.com")
			seam := &snapshottingFence{CalendarFeedFence: fence, snapshot: func() { backup = takeRestoreSnapshot(t, database, user.ID) }}
			repositories = repositories.WithCalendarFeedFence(seam)
			if outcome, err := fence.EnforceRequired(ctx); err != nil || !outcome.FirstBoot {
				t.Fatalf("first boot: %+v, %v", outcome, err)
			}
			auth := services.NewAuthService(repositories.Users)
			now := time.Now()
			cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(restoreAcrossRevocationSecret, user.ID, user.Role, user.AuthSessionVersion, time.Hour, now)
			if err != nil {
				t.Fatalf("mint cookie: %v", err)
			}

			if err := repositories.Users.BumpAuthSessionVersion(ctx, user.ID); err != nil {
				t.Fatalf("sign-out: %v", err)
			}
			if backup.user.ID != user.ID || backup.user.AuthSessionVersion != user.AuthSessionVersion {
				t.Fatalf("fixture: the seam must snapshot the un-revoked row, got %+v", backup.user)
			}
			after := takeRestoreSnapshot(t, database, user.ID)

			// After the transaction: both halves at the new token, row revoked.
			if outcome, err := fence.EnforceRequired(ctx); err != nil || outcome.ContinuityBroken {
				t.Fatalf("a backup-free boot after the sign-out must find continuity at the new token: %+v, %v", outcome, err)
			}
			restoreRestoreSnapshot(t, database, after)
			if outcome, err := fence.EnforceRequired(ctx); err != nil || outcome.ContinuityBroken {
				t.Fatalf("a backup taken after the transaction must restore as continuity: %+v, %v", outcome, err)
			}

			// Between the file write and the transaction: the old marker.
			restoreRestoreSnapshot(t, database, backup)
			if _, _, err := auth.ResolveAuthSession(ctx, restoreAcrossRevocationSecret, cookie, now); err != nil {
				t.Fatalf("fixture: before the boot, the restored row should accept the revoked cookie again, got %v", err)
			}
			outcome, err := fence.EnforceRequired(ctx)
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.ContinuityBroken || outcome.Revoked.LocalSignIns == 0 {
				t.Fatalf("a backup taken mid-revocation must be read as a restore and revoked, got %+v", outcome)
			}
			if _, _, err := auth.ResolveAuthSession(ctx, restoreAcrossRevocationSecret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("the cookie the sign-out revoked must not resolve after the boot, got %v", err)
			}
		})
	}
}
