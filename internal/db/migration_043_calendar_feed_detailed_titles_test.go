package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Migration 043 (WEB-379) adds the detailed-titles opt-in. An upgraded row must
// read as neutral, and the flag must follow the link it was chosen for: written
// by every mint and rotate, never left from the previous link. Cited by
// SECURITY.md's Calendar Feed Subscription rows.

func TestMigration043LeavesEveryUpgradedFeedNeutral(t *testing.T) {
	const column = "calendar_feed_detailed_titles"
	databasePath := filepath.Join(t.TempDir(), "ovumcy-migration-043.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)
	repo := NewUserRepository(database)

	user := createUserForTimezoneTest(t, repo, "migration-043@example.com")
	if err := database.Exec(
		`UPDATE users SET calendar_feed_selector = ? WHERE id = ?`,
		"SELECTOR16CHARSXX", user.ID,
	).Error; err != nil {
		t.Fatalf("seed the pre-upgrade row: %v", err)
	}
	if err := database.Exec(`ALTER TABLE users DROP COLUMN ` + column).Error; err != nil {
		t.Fatalf("drop %s to rewind the schema: %v", column, err)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "043").Error; err != nil {
		t.Fatalf("delete the migration 043 record: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("expected migration 043 to apply to a populated users table: %v", err)
	}
	reopenedSQLDB, err := reopened.DB()
	if err != nil {
		t.Fatalf("get reopened sql db handle: %v", err)
	}
	t.Cleanup(func() { _ = reopenedSQLDB.Close() })
	assertAllEmbeddedMigrationsApplied(t, reopened)

	upgraded, err := NewUserRepository(reopened).FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("reload the upgraded row: %v", err)
	}
	if upgraded.CalendarFeedDetailedTitles {
		t.Fatal("the upgrade opted an existing feed into detailed titles")
	}
	if upgraded.CalendarFeedSelector == "" {
		t.Fatal("the seeded row lost data across the upgrade, so the assertion above could be reporting an empty row")
	}
}

func TestSaveCalendarFeedTokenWritesTheTitleChoiceOfEachLink(t *testing.T) {
	ctx := context.Background()
	repo := openWebhookRepoForTest(t)
	userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-detailed-titles@example.com")

	detailedOf := func() bool {
		t.Helper()
		user, err := repo.FindByID(ctx, userID)
		if err != nil {
			t.Fatalf("reload user: %v", err)
		}
		settings, err := repo.LoadSettingsByID(ctx, userID)
		if err != nil {
			t.Fatalf("load settings: %v", err)
		}
		if settings.CalendarFeedDetailedTitles != user.CalendarFeedDetailedTitles {
			t.Fatalf("the settings projection does not load the title choice")
		}
		return user.CalendarFeedDetailedTitles
	}

	for i, want := range []bool{true, false, true} {
		if err := repo.SaveCalendarFeedToken(ctx, userID, models.CalendarFeedTokenColumns{
			Selector:       selector + string(rune('a'+i)),
			VerifierHash:   "bcrypt-hash",
			VerifierMAC:    "verifier-mac",
			DetailedTitles: want,
		}); err != nil {
			t.Fatalf("SaveCalendarFeedToken #%d: %v", i, err)
		}
		if got := detailedOf(); got != want {
			t.Fatalf("mint #%d stored detailed=%v, want %v", i, got, want)
		}
	}
}
