package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Migration 045 (WEB-381) adds the period-span and fertile-window opt-ins. An
// upgraded row must read as opted out of both, and each flag must follow the
// link it was chosen for: written by every mint and rotate, never left from the
// previous link. Cited by SECURITY.md's Calendar Feed Subscription rows.

// It runs on both engines: each dialect ships its own ADD COLUMN pair, and a
// populated users table is what each NOT NULL DEFAULT has to fill.
func TestMigration045LeavesEveryUpgradedFeedWithoutSpans(t *testing.T) {
	forEachMigrationReplayStore(t, func(t *testing.T, store migrationReplayStore) {
		assertMigration045LeavesEveryUpgradedFeedWithoutSpans(t, store)
	})
}

func assertMigration045LeavesEveryUpgradedFeedWithoutSpans(t *testing.T, store migrationReplayStore) {
	database := store.open(t)
	repo := NewUserRepository(database)

	user := createUserForTimezoneTest(t, repo, "migration-045@example.com")
	if err := database.Exec(
		`UPDATE users SET calendar_feed_selector = ? WHERE id = ?`,
		"SELECTOR16CHARSXX", user.ID,
	).Error; err != nil {
		t.Fatalf("seed the pre-upgrade row: %v", err)
	}
	for _, column := range []string{"calendar_feed_period_span", "calendar_feed_fertile_window"} {
		if err := database.Exec(`ALTER TABLE users DROP COLUMN ` + column).Error; err != nil {
			t.Fatalf("drop %s to rewind the schema: %v", column, err)
		}
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "045").Error; err != nil {
		t.Fatalf("delete the migration 045 record: %v", err)
	}
	closeForMigrationReplay(t, database)

	// store.open fails the test itself when the migration cannot apply to the
	// populated users table.
	reopened := store.open(t)
	store.assertMigrationsApplied(t, reopened)

	upgraded, err := NewUserRepository(reopened).FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("reload the upgraded row: %v", err)
	}
	if upgraded.CalendarFeedPeriodSpan {
		t.Fatal("the upgrade opted an existing feed into period spans")
	}
	if upgraded.CalendarFeedFertileWindow {
		t.Fatal("the upgrade opted an existing feed into the fertile window")
	}
	if upgraded.CalendarFeedSelector == "" {
		t.Fatal("the seeded row lost data across the upgrade, so the assertions above could be reporting an empty row")
	}
}

func TestSaveCalendarFeedTokenWritesTheSpanChoicesOfEachLink(t *testing.T) {
	ctx := context.Background()
	repo := openWebhookRepoForTest(t)
	userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-prediction-spans@example.com")

	type choice struct{ periodSpan, fertileWindow bool }
	storedOf := func() choice {
		t.Helper()
		user, err := repo.FindByID(ctx, userID)
		if err != nil {
			t.Fatalf("reload user: %v", err)
		}
		settings, err := repo.LoadSettingsByID(ctx, userID)
		if err != nil {
			t.Fatalf("load settings: %v", err)
		}
		if settings.CalendarFeedPeriodSpan != user.CalendarFeedPeriodSpan ||
			settings.CalendarFeedFertileWindow != user.CalendarFeedFertileWindow {
			t.Fatalf("the settings projection does not load the span choices")
		}
		return choice{periodSpan: user.CalendarFeedPeriodSpan, fertileWindow: user.CalendarFeedFertileWindow}
	}

	// Each flag moves on its own across the mints, so a write that crossed the
	// two columns, or left one from the previous link, reads wrong at least once.
	for i, want := range []choice{{true, false}, {false, true}, {true, true}, {false, false}} {
		if err := repo.SaveCalendarFeedToken(ctx, userID, models.CalendarFeedTokenColumns{
			Selector:      selector + string(rune('a'+i)),
			VerifierHash:  "bcrypt-hash",
			VerifierMAC:   "verifier-mac",
			PeriodSpan:    want.periodSpan,
			FertileWindow: want.fertileWindow,
		}); err != nil {
			t.Fatalf("SaveCalendarFeedToken #%d: %v", i, err)
		}
		if got := storedOf(); got != want {
			t.Fatalf("mint #%d stored %+v, want %+v", i, got, want)
		}
	}
}
