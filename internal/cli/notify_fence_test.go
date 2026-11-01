package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestNotifyRefusesARestoredDatabaseUntilTheServerBoots is WEB-408's restore
// fixture: an owner's endpoint is in a backup, the owner removes it after the
// backup point — the only change made — and the backup is restored with the
// server still stopped. notify must not deliver from that database — the
// endpoint in front of it is one only the server's ContinuityBroken boot pass
// clears — and CALENDAR_FEED_FENCE_OPTOUT must not change that, because a
// disagreeing pair is evidence of a restore, not an absent fence. Once the
// server's boot pass has run, notify delivers again and the endpoint is gone.
func TestNotifyRefusesARestoredDatabaseUntilTheServerBoots(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "live.db")
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
	owner := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)
	if err := runWebhookCommandWithFence(config, testNotifySecretKey, fencePath, []string{"set", owner.Email, "--enabled=true", "--url-stdin"}, strings.NewReader(testWebhookURLWithToken+"\n"), &bytes.Buffer{}); err != nil {
		t.Fatalf("webhook set: %v", err)
	}
	backupPath := backupNotifySQLite(t, databasePath)

	// The removal alone moves the fence: nothing else changes after the backup
	// point, which is the shape a restore used to be invisible in.
	withNotifyRepositories(t, databasePath, fencePath, func(repositories *db.Repositories, _ *services.CalendarFeedRestoreFence) {
		if err := repositories.Users.RemoveWebhookDestination(context.Background(), owner.ID); err != nil {
			t.Fatalf("remove webhook: %v", err)
		}
	})
	restoreNotifySQLite(t, backupPath, databasePath)

	for _, optOut := range []bool{false, true} {
		var output bytes.Buffer
		err := runNotifyOperatorCommand(config, fencePath, testNotifySecretKey, "en", time.UTC, false, optOut, nil, &output)
		if err == nil {
			t.Fatalf("optOut=%t: notify delivered from a restored database the server has not booted over", optOut)
		}
		for _, want := range []string{"hold different tokens", "the server must boot over this database", "Nothing was sent"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("optOut=%t: refusal %q does not say %q", optOut, err, want)
			}
		}
		if output.Len() != 0 {
			t.Fatalf("optOut=%t: a refused notify printed %q", optOut, output.String())
		}
	}
	if url := notifyStoredWebhookURL(t, databasePath, owner.ID); url == "" {
		t.Fatal("fixture: the restored row should still carry the removed endpoint")
	}

	withNotifyRepositories(t, databasePath, fencePath, func(_ *db.Repositories, fence *services.CalendarFeedRestoreFence) {
		outcome, err := fence.Enforce(context.Background())
		if err != nil || !outcome.ContinuityBroken {
			t.Fatalf("server boot over the restore: outcome %+v, err %v", outcome, err)
		}
	})

	var output bytes.Buffer
	if err := runNotifyOperatorCommand(config, fencePath, testNotifySecretKey, "en", time.UTC, false, false, nil, &output); err != nil {
		t.Fatalf("notify after the server's boot pass: %v", err)
	}
	if url := notifyStoredWebhookURL(t, databasePath, owner.ID); url != "" {
		t.Fatal("the server's boot pass should have cleared the restored endpoint")
	}
}

// TestNotifyFenceGateOptOutWaivesOnlyAnAbsentFence walks the shapes a notify
// shell can meet without a restore in front of it. Each is refused with
// nothing sent; CALENDAR_FEED_FENCE_OPTOUT waives the ones that carry no
// evidence either way — which an opted-out instance is in on every start — and
// never a relative path, which names no fence the server could share.
func TestNotifyFenceGateOptOutWaivesOnlyAnAbsentFence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		fencePath  func(t *testing.T, databasePath string) string
		wantReason string
		waived     bool
	}{
		{"unset", func(*testing.T, string) string { return "" }, security.CalendarFeedFencePathEnv + " is not set", true},
		{"relative", func(*testing.T, string) string { return filepath.Join("fence", "calendar-feed.fence") }, "a relative path", false},
		{"never armed", func(t *testing.T, _ string) string { return filepath.Join(t.TempDir(), "calendar-feed.fence") }, "nor the database holds a marker", true},
		{"unreachable", func(t *testing.T, _ string) string { return t.TempDir() }, "cannot read or write", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			databasePath := createCLIUsersDatabase(t)
			config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
			fencePath := tc.fencePath(t, databasePath)

			var refused bytes.Buffer
			err := runNotifyOperatorCommand(config, fencePath, testNotifySecretKey, "en", time.UTC, false, false, []string{"--dry-run"}, &refused)
			if err == nil || !strings.Contains(err.Error(), tc.wantReason) || !strings.Contains(err.Error(), "Nothing was sent") {
				t.Fatalf("want a refusal naming %q, got %v", tc.wantReason, err)
			}
			if refused.Len() != 0 {
				t.Fatalf("a refused notify printed %q", refused.String())
			}

			var output bytes.Buffer
			err = runNotifyOperatorCommand(config, fencePath, testNotifySecretKey, "en", time.UTC, false, true, []string{"--dry-run"}, &output)
			if !tc.waived {
				if err == nil || !strings.Contains(err.Error(), tc.wantReason) {
					t.Fatalf("the opt-out must not waive %s, got %v", tc.name, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the opt-out should waive %s, got %v", tc.name, err)
			}
			if !strings.Contains(output.String(), security.CalendarFeedFenceOptOutEnv+"=true") || !strings.Contains(output.String(), "Webhook notify pass complete") {
				t.Fatalf("a waived gate should warn and then run the pass, got %q", output.String())
			}
		})
	}
}

// TestNotifyFenceGateAcceptsAnAgreeingFenceWithoutWriting pins the read-only
// half: a fence the server armed passes silently, and neither half moves.
func TestNotifyFenceGateAcceptsAnAgreeingFenceWithoutWriting(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
	fencePath := armOperatorFence(t, databasePath)
	before, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("read fence: %v", err)
	}

	var output bytes.Buffer
	if err := runNotifyOperatorCommand(config, fencePath, testNotifySecretKey, "en", time.UTC, false, false, []string{"--dry-run"}, &output); err != nil {
		t.Fatalf("notify over an agreeing fence: %v", err)
	}
	if strings.Contains(output.String(), "calendar-feed restore fence") {
		t.Fatalf("an agreeing fence should pass without a fence line, got %q", output.String())
	}
	after, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("read fence: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("notify moved the fence file; the gate must write nothing")
	}
}

// TestNotifyFenceGateRejectsANilFenceInsteadOfPanicking: every production
// caller gets its fence from buildRepositories; a nil one is wiring, refused
// plainly rather than dereferenced.
func TestNotifyFenceGateRejectsANilFenceInsteadOfPanicking(t *testing.T) {
	t.Parallel()

	err := confirmNotifyFenceContinuity(context.Background(), filepath.Join(t.TempDir(), "calendar-feed.fence"), nil, false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("want the wiring error, got %v", err)
	}
}

// withNotifyRepositories opens databasePath with the fence at fencePath, as the
// server would, and closes it before returning so the file can be replaced.
func withNotifyRepositories(t *testing.T, databasePath, fencePath string, run func(repositories *db.Repositories, fence *services.CalendarFeedRestoreFence)) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	repositories, fence := buildRepositories(database, fencePath)
	run(repositories, fence)
}

func backupNotifySQLite(t *testing.T, databasePath string) string {
	t.Helper()

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.Exec("VACUUM INTO ?", backupPath); err != nil {
		t.Fatalf("backup: %v", err)
	}
	return backupPath
}

// restoreNotifySQLite replaces the live database with the backup, the way an
// operator restores with the app stopped: the WAL and shared-memory files of
// the replaced database go with it.
func restoreNotifySQLite(t *testing.T, backupPath, databasePath string) {
	t.Helper()

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(databasePath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", databasePath+suffix, err)
		}
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if err := os.WriteFile(databasePath, backup, 0o600); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func notifyStoredWebhookURL(t *testing.T, databasePath string, userID uint) string {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var url string
	if err := database.Model(&models.User{}).Where("id = ?", userID).Pluck("webhook_url", &url).Error; err != nil {
		t.Fatalf("read webhook_url: %v", err)
	}
	return url
}
