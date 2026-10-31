package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestRestoreFenceRefusesAPreRestoreCookieAndClearsWebhooks replays WEB-399 on
// the real boot wrapper: a cookie minted before a restore, and a webhook
// endpoint stored before it, both live in columns the restore rolls back, so
// the boot that sees the restore has to revoke them as it disarms the feeds.
// The ordinary restart before it is the negative control: a boot that proves
// continuity signs nobody out and keeps every endpoint.
func TestRestoreFenceRefusesAPreRestoreCookieAndClearsWebhooks(t *testing.T) {
	ctx := context.Background()
	secret := []byte("restore-fence-sessions-secret-k1")
	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "fence-sessions.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, fence := bootstrap.BuildRepositories(database, fencePath)
	user := seedOwner(t, repositories, "restore-fence-sessions@example.com", 14)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"webhook_enabled":       true,
		"webhook_url":           "sealed-endpoint",
		"webhook_notify_period": false,
	}).Error; err != nil {
		t.Fatalf("store a webhook: %v", err)
	}
	stored, err := repositories.Users.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	auth := services.NewAuthService(repositories.Users)
	now := time.Now()
	cookie, _, err := auth.BuildAuthSessionTokenWithSessionID(secret, user.ID, stored.Role, stored.AuthSessionVersion, time.Hour, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionTokenWithSessionID: %v", err)
	}

	mustEnforceCalendarFeedRestoreFence(fence, false)
	mustEnforceCalendarFeedRestoreFence(fence, false)
	if _, _, err := auth.ResolveAuthSession(ctx, secret, cookie, now); err != nil {
		t.Fatalf("an ordinary restart must keep the session: %v", err)
	}
	if kept, _ := repositories.Users.FindByID(ctx, user.ID); !kept.WebhookEnabled || kept.WebhookURL == "" {
		t.Fatalf("an ordinary restart must keep the webhook endpoint, got enabled=%v url=%q", kept.WebhookEnabled, kept.WebhookURL)
	}

	// A restored database carries a marker this fence file never wrote;
	// rewriting the file stages the same disagreement from the other side.
	if err := os.WriteFile(fencePath, []byte("a-different-generation\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustEnforceCalendarFeedRestoreFence(fence, false)

	if _, _, err := auth.ResolveAuthSession(ctx, secret, cookie, now); !errors.Is(err, services.ErrAuthSessionTokenRevoked) {
		t.Fatalf("a cookie minted before the restore must be refused, got %v", err)
	}
	after, err := repositories.Users.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if after.WebhookEnabled || after.WebhookURL != "" {
		t.Fatalf("the restore boot must clear the webhook endpoint, got enabled=%v url=%q", after.WebhookEnabled, after.WebhookURL)
	}
	if after.WebhookConfigVersion <= stored.WebhookConfigVersion {
		t.Fatalf("the restore boot must advance the webhook revocation epoch (before %d, after %d)", stored.WebhookConfigVersion, after.WebhookConfigVersion)
	}
	if after.WebhookNotifyPeriod {
		t.Fatal("the restore boot must keep the owner's per-kind opt-ins")
	}
}
