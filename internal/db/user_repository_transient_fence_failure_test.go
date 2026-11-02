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
// shape of a momentary ENOSPC or EIO on the fence mount.
type failingOnceFenceFile struct {
	*security.CalendarFeedFenceFile
	failNext bool
}

func (file *failingOnceFenceFile) Write(value string) error {
	if file.failNext {
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

// TestASignOutOverAFailingFenceFileDivergesAndTheNextBootRevokes is the
// diverge path of AdvanceOrDiverge end to end (WEB-454), on both dialects: a
// sign-out that meets a failing fence file is NOT refused, moves the database
// half of the fence alone, and the next boot reads the disagreement as broken
// continuity and refuses the cookie the sign-out revoked. A writer that
// swallowed the failure and left both halves agreeing would leave that boot a
// no-op.
func TestASignOutOverAFailingFenceFileDivergesAndTheNextBootRevokes(t *testing.T) {
	dialects := map[string]func(t *testing.T) *gorm.DB{
		"sqlite": func(t *testing.T) *gorm.DB {
			return openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "diverging-fence.db"))
		},
		"postgres": func(t *testing.T) *gorm.DB {
			return openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		},
	}
	secret := []byte("diverging-fence-test-secret-k001")
	for name, open := range dialects {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repositories := NewRepositories(open(t))
			file := &failingOnceFenceFile{CalendarFeedFenceFile: security.NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "calendar-feed.fence"))}
			fence := services.NewCalendarFeedRestoreFence(repositories.AppState, repositories.Users, file)
			repositories = repositories.WithCalendarFeedFence(fence)
			if outcome, err := fence.EnforceRequired(ctx); err != nil || !outcome.FirstBoot {
				t.Fatalf("first boot: %+v, %v", outcome, err)
			}

			user := createUserForTimezoneTest(t, repositories.Users, "diverging-fence@example.com")
			auth := services.NewAuthService(repositories.Users)
			now := time.Now()
			cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(secret, user.ID, user.Role, user.AuthSessionVersion, time.Hour, now)
			if err != nil {
				t.Fatalf("mint a cookie: %v", err)
			}
			fileBefore, _, err := file.Read()
			if err != nil {
				t.Fatalf("read the fence file: %v", err)
			}

			file.failNext = true
			if err := repositories.Users.BumpAuthSessionVersion(ctx, user.ID); err != nil {
				t.Fatalf("a sign-out must not be refused over the fence file, got %v", err)
			}
			if after := reloadUserForWebhookRevocationTest(t, repositories.Users, user.ID); after.AuthSessionVersion <= user.AuthSessionVersion {
				t.Fatalf("the sign-out must still revoke, session version stayed %d", after.AuthSessionVersion)
			}
			fileAfter, _, err := file.Read()
			if err != nil {
				t.Fatalf("read the fence file: %v", err)
			}
			stored, _, err := repositories.AppState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
			if err != nil {
				t.Fatalf("read the database half: %v", err)
			}
			if fileAfter != fileBefore || stored == fileAfter {
				t.Fatalf("the failed file write must leave the file where it was and move the database half alone: file moved %t, halves agree %t", fileAfter != fileBefore, stored == fileAfter)
			}

			outcome, err := fence.EnforceRequired(ctx)
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.ContinuityBroken || outcome.Revoked.Sessions == 0 {
				t.Fatalf("the boot after a diverged sign-out must run the full pass, got %+v", outcome)
			}
			if _, _, err := auth.ResolveAuthSession(ctx, secret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
				t.Fatalf("the signed-out cookie must not resolve after the boot, got %v", err)
			}
		})
	}
}
