package db

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var unmountedFenceFeedSecret = []byte("unmounted-fence-feed-test-key001")

// armFeedAndWebhookForFenceTest arms the fixture owner's feed through the
// repository and stores a webhook endpoint beside it.
func armFeedAndWebhookForFenceTest(t *testing.T, fixture signOutFenceFixture) models.CalendarFeedTokenColumns {
	t.Helper()
	repo := fixture.repositories.Users
	_, columns, err := services.GenerateCalendarFeedToken(unmountedFenceFeedSecret)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(context.Background(), fixture.user.ID, columns); err != nil {
		t.Fatalf("arm the feed: %v", err)
	}
	if err := repo.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).
		Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
		t.Fatalf("store a webhook: %v", err)
	}
	return columns
}

func unmountFenceVolume(t *testing.T, fixture signOutFenceFixture) (remount func()) {
	t.Helper()
	unmounted := fixture.mount + ".unmounted"
	if err := os.Rename(fixture.mount, unmounted); err != nil {
		t.Fatalf("unmount the fence volume: %v", err)
	}
	return func() {
		if err := os.Rename(unmounted, fixture.mount); err != nil {
			t.Fatalf("mount the fence volume again: %v", err)
		}
	}
}

// TestAFeedOrWebhookWriteOverAnUnmountedFenceVolumeIsRefused is WEB-469 for
// every writer that advances through Advance, on both dialects. A fence volume
// gone from under a running server reads as no token and fails every write;
// before WEB-469 Advance read that as halves that already disagree, moved the
// database half alone and let the row write through. Mounted again, the file
// still holds the token a pre-write backup carries. Each write is now refused
// with the row and both halves untouched, and goes through once the volume is
// back.
func TestAFeedOrWebhookWriteOverAnUnmountedFenceVolumeIsRefused(t *testing.T) {
	writers := map[string]func(ctx context.Context, repo *UserRepository, user models.User) error{
		"ClearCalendarFeedToken": func(ctx context.Context, repo *UserRepository, user models.User) error {
			return repo.ClearCalendarFeedToken(ctx, user.ID)
		},
		"SaveCalendarFeedToken": func(ctx context.Context, repo *UserRepository, user models.User) error {
			_, rotated, err := services.GenerateCalendarFeedToken(unmountedFenceFeedSecret)
			if err != nil {
				t.Fatalf("GenerateCalendarFeedToken: %v", err)
			}
			return repo.SaveCalendarFeedToken(ctx, user.ID, rotated)
		},
		"RemoveWebhookDestination": func(ctx context.Context, repo *UserRepository, user models.User) error {
			return repo.RemoveWebhookDestination(ctx, user.ID)
		},
		"SaveWebhookSettings": func(ctx context.Context, repo *UserRepository, user models.User) error {
			return repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{ExpectedConfigVersion: user.WebhookConfigVersion})
		},
	}
	for dialect, open := range signOutFenceDialects() {
		for name, write := range writers {
			t.Run(dialect+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				fixture := newSignOutFenceFixture(t, open(t))
				repo := fixture.repositories.Users
				armFeedAndWebhookForFenceTest(t, fixture)
				before := reloadUserForCalendarFeed(t, repo, fixture.user.ID)
				_, storedBefore := fixture.halves(t)

				remount := unmountFenceVolume(t, fixture)
				if err := write(ctx, repo, before); !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
					t.Fatalf("%s over an unmounted fence volume must be refused, got %v", name, err)
				}
				after := reloadUserForCalendarFeed(t, repo, fixture.user.ID)
				if after.CalendarFeedSelector != before.CalendarFeedSelector || after.WebhookURL != before.WebhookURL ||
					after.WebhookEnabled != before.WebhookEnabled || after.WebhookConfigVersion != before.WebhookConfigVersion {
					t.Fatalf("a refused %s must leave the row untouched", name)
				}
				remount()
				if fileAfter, storedAfter := fixture.halves(t); storedAfter != storedBefore || fileAfter != storedBefore {
					t.Fatalf("a refused %s must write neither half: file moved %t, database moved %t", name, fileAfter != storedBefore, storedAfter != storedBefore)
				}

				if err := write(ctx, repo, before); err != nil {
					t.Fatalf("%s once the volume is back: %v", name, err)
				}
				if fileAfter, storedAfter := fixture.halves(t); fileAfter != storedAfter || storedAfter == storedBefore {
					t.Fatalf("the retried %s must move both halves to one fresh token", name)
				}
			})
		}
	}
}

// TestARestoreBeforeTheBootCannotReviveAFeedRevokedOverAnUnmountedFenceVolume
// is WEB-469's repro, on both dialects. A backup is taken, the fence volume
// drops out, the owner revokes the feed and removes the webhook, the volume is
// mounted again and the backup restored before any boot. Whatever each
// revocation answered, a reported success must hold across that restore: when
// Advance moved the database half alone, the remounted file agreed with the
// restored app_state, the boot read continuity and the revoked feed and
// endpoint were live again.
func TestARestoreBeforeTheBootCannotReviveAFeedRevokedOverAnUnmountedFenceVolume(t *testing.T) {
	for name, open := range signOutFenceDialects() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			database := open(t)
			fixture := newSignOutFenceFixture(t, database)
			repo := fixture.repositories.Users
			columns := armFeedAndWebhookForFenceTest(t, fixture)
			backup := takeRestoreSnapshot(t, database, fixture.user.ID)

			remount := unmountFenceVolume(t, fixture)
			revokeErr := repo.ClearCalendarFeedToken(ctx, fixture.user.ID)
			removeErr := repo.RemoveWebhookDestination(ctx, fixture.user.ID)
			remount()
			restoreRestoreSnapshot(t, database, backup)

			outcome, err := fixture.fence.EnforceRequired(ctx)
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			after := reloadUserForCalendarFeed(t, repo, fixture.user.ID)
			if revokeErr == nil && after.CalendarFeedSelector == columns.Selector {
				t.Fatalf("a feed revocation reported done must not be revived by a restore before the boot (boot %+v)", outcome)
			}
			if removeErr == nil && after.WebhookURL != "" {
				t.Fatalf("a webhook removal reported done must not be revived by a restore before the boot (boot %+v)", outcome)
			}
			for label, err := range map[string]error{"revoke": revokeErr, "remove": removeErr} {
				if err != nil && !errors.Is(err, services.ErrCalendarFeedFenceWriteFailed) {
					t.Fatalf("%s: a failure here must be the fence refusal, got %v", label, err)
				}
			}
		})
	}
}
