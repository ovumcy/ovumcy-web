package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// failingOnceFenceFile is the real fence file whose next Write fails once, the
// shape of a momentary ENOSPC or EIO on the fence mount; failing makes every
// Write fail, the shape of a fence volume that stays broken.
type failingOnceFenceFile struct {
	*security.CalendarFeedFenceFile
	failNext bool
	failing  bool
}

func (file *failingOnceFenceFile) Write(value string) error {
	if file.failNext || file.failing {
		file.failNext = false
		return errors.New("no space left on device")
	}
	return file.CalendarFeedFenceFile.Write(value)
}

// TestATransientFenceWriteFailureRevokesNoPasswordOnTheNextBoot is WEB-414's
// check, on both dialects, over the real fence file and app_state: a feed
// revocation and a webhook removal that meet a failing fence file are refused
// with the row untouched, and the boot after the failure clears reads
// continuity — no ContinuityBroken pass, every password and session intact.
// Before WEB-414 the database half moved alone and that boot revoked every
// local password on the instance.
func TestATransientFenceWriteFailureRevokesNoPasswordOnTheNextBoot(t *testing.T) {
	dialects := map[string]func(t *testing.T) *gorm.DB{
		"sqlite": func(t *testing.T) *gorm.DB {
			return openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "transient-fence.db"))
		},
		"postgres": func(t *testing.T) *gorm.DB {
			return openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		},
	}
	for name, open := range dialects {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repositories := NewRepositories(open(t))
			file := &failingOnceFenceFile{CalendarFeedFenceFile: security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))}
			fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
			repositories = repositories.WithCalendarFeedFence(fence)
			repo := repositories.Users
			if outcome, err := fence.EnforceRequired(ctx); err != nil || !outcome.FirstBoot {
				t.Fatalf("first boot: %+v, %v", outcome, err)
			}

			user := createUserForTimezoneTest(t, repo, "transient-fence@example.com")
			_, columns, err := services.GenerateCalendarFeedToken([]byte("transient-fence-test-secret-k001"))
			if err != nil {
				t.Fatalf("GenerateCalendarFeedToken: %v", err)
			}
			if err := repo.SaveCalendarFeedToken(ctx, user.ID, columns); err != nil {
				t.Fatalf("arm the feed: %v", err)
			}
			if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).
				Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
				t.Fatalf("store a webhook: %v", err)
			}
			before := reloadUserForWebhookRevocationTest(t, repo, user.ID)

			file.failNext = true
			if err := repo.ClearCalendarFeedToken(ctx, user.ID); !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
				t.Fatalf("ClearCalendarFeedToken must refuse a failed fence write, got %v", err)
			}
			file.failNext = true
			if err := repo.RemoveWebhookDestination(ctx, user.ID); !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
				t.Fatalf("RemoveWebhookDestination must refuse a failed fence write, got %v", err)
			}
			if after := reloadUserForCalendarFeed(t, repo, user.ID); after.CalendarFeedSelector != columns.Selector || after.WebhookURL != before.WebhookURL {
				t.Fatalf("a refused write must leave the row untouched, got selector %q url %q", after.CalendarFeedSelector, after.WebhookURL)
			}

			outcome, err := fence.EnforceRequired(ctx)
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if outcome != (services.CalendarFeedRestoreFenceOutcome{}) {
				t.Fatalf("the boot after a transient failure must read continuity, got %+v", outcome)
			}
			after := reloadUserForWebhookRevocationTest(t, repo, user.ID)
			if after.PasswordHash != before.PasswordHash || !after.LocalAuthEnabled || after.AuthSessionVersion != before.AuthSessionVersion {
				t.Fatal("no password or session may be revoked over a transient fence failure")
			}

			// The retry the refusal asked for goes through and leaves the halves agreeing.
			if err := repo.ClearCalendarFeedToken(ctx, user.ID); err != nil {
				t.Fatalf("retry ClearCalendarFeedToken: %v", err)
			}
			if outcome, err := fence.EnforceRequired(ctx); err != nil || outcome != (services.CalendarFeedRestoreFenceOutcome{}) {
				t.Fatalf("the boot after the retry must read continuity, got %+v, %v", outcome, err)
			}
		})
	}
}

// signOutFenceFixture is a fenced instance past its first boot with one owner
// and a cookie minted at that owner's session version.
type signOutFenceFixture struct {
	repositories *Repositories
	file         *failingOnceFenceFile
	fence        *services.CalendarFeedRestoreFence
	auth         *services.AuthService
	user         models.User
	cookie       string
	now          time.Time
}

var signOutFenceSecret = []byte("diverging-fence-test-secret-k001")

func newSignOutFenceFixture(t *testing.T, database *gorm.DB) signOutFenceFixture {
	t.Helper()
	ctx := context.Background()
	repositories := NewRepositories(database)
	file := &failingOnceFenceFile{CalendarFeedFenceFile: security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))}
	fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
	repositories = repositories.WithCalendarFeedFence(fence)
	if outcome, err := fence.EnforceRequired(ctx); err != nil || !outcome.FirstBoot {
		t.Fatalf("first boot: %+v, %v", outcome, err)
	}
	user := createUserForTimezoneTest(t, repositories.Users, "sign-out-fence@example.com")
	auth := services.NewAuthService(repositories.Users)
	now := time.Now()
	cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(signOutFenceSecret, user.ID, user.Role, user.AuthSessionVersion, time.Hour, now)
	if err != nil {
		t.Fatalf("mint a cookie: %v", err)
	}
	return signOutFenceFixture{repositories: repositories,file: file, fence: fence, auth: auth, user: user, cookie: cookie, now: now}
}

func (fixture signOutFenceFixture) halves(t *testing.T) (file string, stored string) {
	t.Helper()
	file, _, err := fixture.file.Read()
	if err != nil {
		t.Fatalf("read the fence file: %v", err)
	}
	stored, _, err = fixture.repositories.AppState.Get(context.Background(), models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		t.Fatalf("read the database half: %v", err)
	}
	return file, stored
}

func signOutFenceDialects() map[string]func(t *testing.T) *gorm.DB {
	return map[string]func(t *testing.T) *gorm.DB{
		"sqlite": func(t *testing.T) *gorm.DB {
			return openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "sign-out-fence.db"))
		},
		"postgres": func(t *testing.T) *gorm.DB {
			return openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		},
	}
}

// TestASignOutOverAFenceFileFailingOnceRetriesAndRevokes is WEB-464's
// transient arm end to end, on both dialects: a sign-out that meets one failed
// fence-file write is not refused, and the retry moves BOTH halves to one
// fresh token, so nothing is left for a restore or the next boot to misread.
func TestASignOutOverAFenceFileFailingOnceRetriesAndRevokes(t *testing.T) {
	for name, open := range signOutFenceDialects() {
		t.Run(name, func(t *testing.T) {
			fixture := newSignOutFenceFixture(t, open(t))
			fileBefore, _ := fixture.halves(t)

			fixture.file.failNext = true
			if err := fixture.repositories.Users.BumpAuthSessionVersion(context.Background(), fixture.user.ID); err != nil {
				t.Fatalf("a sign-out over a transient fence failure must go through, got %v", err)
			}
			if after := reloadUserForWebhookRevocationTest(t, fixture.repositories.Users, fixture.user.ID); after.AuthSessionVersion <= fixture.user.AuthSessionVersion {
				t.Fatalf("the sign-out must revoke, session version stayed %d", after.AuthSessionVersion)
			}
			fileAfter, stored := fixture.halves(t)
			if fileAfter == fileBefore || stored != fileAfter {
				t.Fatalf("the retried advance must move both halves to one fresh token: file moved %t, halves agree %t", fileAfter != fileBefore, stored == fileAfter)
			}
		})
	}
}

// TestASignOutOverAPersistentlyFailingFenceFileIsRefused is WEB-464's
// persistent arm, on both dialects: when every retry fails the sign-out is
// refused like Advance — session version, file and database half all where
// they were — and goes through once the fence volume writes again.
func TestASignOutOverAPersistentlyFailingFenceFileIsRefused(t *testing.T) {
	for name, open := range signOutFenceDialects() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newSignOutFenceFixture(t, open(t))
			fileBefore, storedBefore := fixture.halves(t)

			fixture.file.failing = true
			if err := fixture.repositories.Users.BumpAuthSessionVersion(ctx, fixture.user.ID); !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
				t.Fatalf("a sign-out over a persistently failing fence file must be refused, got %v", err)
			}
			if after := reloadUserForWebhookRevocationTest(t, fixture.repositories.Users, fixture.user.ID); after.AuthSessionVersion != fixture.user.AuthSessionVersion {
				t.Fatalf("a refused sign-out must leave the row untouched, session version %d -> %d", fixture.user.AuthSessionVersion, after.AuthSessionVersion)
			}
			if fileAfter, storedAfter := fixture.halves(t); fileAfter != fileBefore || storedAfter != storedBefore {
				t.Fatalf("a refused sign-out must write neither half: file moved %t, database moved %t", fileAfter != fileBefore, storedAfter != storedBefore)
			}

			fixture.file.failing = false
			if err := fixture.repositories.Users.BumpAuthSessionVersion(ctx, fixture.user.ID); err != nil {
				t.Fatalf("the retried sign-out: %v", err)
			}
			if _, _, err := fixture.auth.ResolveAuthSession(ctx, signOutFenceSecret, fixture.cookie, fixture.now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("the retried sign-out must revoke the cookie, got %v", err)
			}
		})
	}
}

// TestARestoreBeforeTheBootCannotReviveASignOutOverAFailingFenceFile is
// WEB-464's repro, on both dialects. A backup is taken, the owner signs out
// while the fence file fails a write, and the backup is restored before any
// boot. When the sign-out moved the database half alone (WEB-454), the file
// still held the backup's token, the restored pair agreed, the boot read
// continuity and the signed-out cookie resolved again.
func TestARestoreBeforeTheBootCannotReviveASignOutOverAFailingFenceFile(t *testing.T) {
	for name, open := range signOutFenceDialects() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			database := open(t)
			fixture := newSignOutFenceFixture(t, database)
			backup := takeRestoreSnapshot(t, database, fixture.user.ID)

			fixture.file.failNext = true
			if err := fixture.repositories.Users.BumpAuthSessionVersion(ctx, fixture.user.ID); err != nil {
				t.Fatalf("sign-out: %v", err)
			}
			restoreRestoreSnapshot(t, database, backup)

			outcome, err := fixture.fence.EnforceRequired(ctx)
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.ContinuityBroken {
				t.Fatalf("the boot after restoring a pre-sign-out backup must read the restore, got %+v", outcome)
			}
			if _, _, err := fixture.auth.ResolveAuthSession(ctx, signOutFenceSecret, fixture.cookie, fixture.now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("the signed-out cookie must not resolve after the restore, got %v", err)
			}
		})
	}
}
