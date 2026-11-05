package db

import (
	"testing"
	"time"
)

// Migration 044 (WEB-489) empties oidc_logout_states: every row written before
// the id_token_hint was sealed holds a provider-signed ID token in cleartext,
// and the upgraded server refuses it anyway. The re-run lands the DELETE on a
// database where it has already been applied once, which is the runner's
// idempotency and the restored-pre-044-backup case together.
//
// It runs on both engines: the two dialects ship separate files, and the
// Postgres DELETE would otherwise only ever meet an empty table.
func TestMigration044PurgesCleartextOIDCLogoutStates(t *testing.T) {
	forEachMigrationReplayStore(t, func(t *testing.T, store migrationReplayStore) {
		assertMigration044PurgesCleartextOIDCLogoutStates(t, store)
	})
}

func assertMigration044PurgesCleartextOIDCLogoutStates(t *testing.T, store migrationReplayStore) {
	database := store.open(t)
	repo := NewUserRepository(database)
	owner := createUserForTimezoneTest(t, repo, "migration-044@example.com")
	second := createUserForTimezoneTest(t, repo, "migration-044-second@example.com")

	// The purge is the whole table, not a predicate: rows of different owners,
	// a NULL-owner row and a value that already looks sealed all have to go, so
	// a DELETE narrowed in either dialect file fails here.
	expiresAt := time.Now().Add(time.Hour).UTC()
	rows := []struct {
		sessionID string
		userID    any
		hint      string
	}{
		{sessionID: "pre-044-owner", userID: owner.ID, hint: "eyJhbGciOiJSUzI1NiJ9.cleartext.signature"},
		{sessionID: "pre-044-second-owner", userID: second.ID, hint: "eyJhbGciOiJSUzI1NiJ9.second.signature"},
		{sessionID: "pre-044-null-owner", userID: nil, hint: "eyJhbGciOiJSUzI1NiJ9.null.signature"},
		{sessionID: "pre-044-sealed-looking", userID: owner.ID, hint: "lrgm--EZDVh9PfMzTzbqEOUSbL6wv-ON6ff_FoHofL"},
	}
	for _, row := range rows {
		if err := database.Exec(
			`INSERT INTO oidc_logout_states
			   (session_id, user_id, end_session_endpoint, id_token_hint, post_logout_redirect_url, expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			row.sessionID, row.userID, "https://idp.example.com/logout", row.hint, "https://app.example.com/", expiresAt,
		).Error; err != nil {
			t.Fatalf("insert %s logout state: %v", row.sessionID, err)
		}
	}
	var seeded int64
	for _, row := range rows {
		seeded += countOIDCLogoutStatesBySessionID(t, database, row.sessionID)
	}
	if seeded != int64(len(rows)) {
		t.Fatalf("expected %d seeded logout states, got %d", len(rows), seeded)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "044").Error; err != nil {
		t.Fatalf("delete the migration 044 record: %v", err)
	}
	closeForMigrationReplay(t, database)

	reopened := store.open(t)
	store.assertMigrationsApplied(t, reopened)

	var remaining int64
	if err := reopened.Table("oidc_logout_states").Count(&remaining).Error; err != nil {
		t.Fatalf("count logout states: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("migration 044 left %d logout state(s), each with a cleartext id_token_hint", remaining)
	}
	if _, err := NewUserRepository(reopened).FindByID(t.Context(), owner.ID); err != nil {
		t.Fatalf("the purge reached past its table: the owner row is gone: %v", err)
	}
}
