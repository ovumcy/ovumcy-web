package db

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// userScopeColumn is the column that marks a table as holding one owner's rows.
// Erasure completeness is defined against it: every table carrying it must be
// empty for the deleted account.
const userScopeColumn = "user_id"

// erasureUserScopeExemptions lists tables that carry userScopeColumn yet are
// deliberately NOT required to be empty for the deleted account afterwards,
// each with the one-line reason that bounds the surviving rows.
//
// It is empty on purpose. Every user-scoped table in the schema today is erased
// by DeleteAccountAndRelatedData, and an exemption-free sweep is what also
// covers tables added later — an entry here is a hole punched in the GDPR
// erasure guarantee, so it may only be added together with the reason those
// rows are allowed to survive (a bounded TTL, a separate erasure path, rows
// that belong to a different owner).
//
// Note that `users` is not a candidate: it is keyed by `id`, not by
// userScopeColumn, so the derivation below never sees it and the account row
// itself is asserted gone separately.
var erasureUserScopeExemptions = map[string]string{}

// countWhere returns the row count for model matching query, failing the test
// on a query error — folding the repeated count-and-check-err blocks out of the
// erasure scenarios.
func countWhere(t *testing.T, database *gorm.DB, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	requireNoErr(t, database.Model(model).Where(query, args...).Count(&count).Error, "count")
	return count
}

// countUserRowsInTable counts the rows a live table holds for userID, addressing
// the table by the name the schema reports rather than through a model — the
// point of the derivation is to reach tables no model in this test knows about.
func countUserRowsInTable(t *testing.T, database *gorm.DB, table string, userID uint) int64 {
	t.Helper()
	var count int64
	requireNoErr(t, database.Table(table).Where(userScopeColumn+" = ?", userID).Count(&count).Error, "count "+table)
	return count
}

// userScopedTablesFromSchema derives from the LIVE schema every table carrying
// a userScopeColumn column, minus erasureUserScopeExemptions, sorted for a
// stable failure message.
//
// It reads the schema through GORM's migrator (GetTables/ColumnTypes) rather
// than a dialect-specific sqlite_master or information_schema query, so the
// same derivation holds on both supported engines. Deriving instead of listing
// is the whole point: a hand-written list mirrors the implementation it is
// meant to check, and a table added by a later migration but forgotten in
// DeleteAccountAndRelatedData would never enter it.
func userScopedTablesFromSchema(t *testing.T, database *gorm.DB) []string {
	t.Helper()
	migrator := database.Migrator()
	tables, err := migrator.GetTables()
	requireNoErr(t, err, "list tables")

	scoped := make([]string, 0, len(tables))
	for _, table := range tables {
		columns, err := migrator.ColumnTypes(table)
		requireNoErr(t, err, "column types of "+table)
		for _, column := range columns {
			if !strings.EqualFold(column.Name(), userScopeColumn) {
				continue
			}
			if reason, exempt := erasureUserScopeExemptions[table]; exempt {
				t.Logf("skipping user-scoped table %s: %s", table, reason)
				break
			}
			scoped = append(scoped, table)
			break
		}
	}
	sort.Strings(scoped)
	return scoped
}

// TestDeleteAccountAndRelatedDataRemovesAllUserRows proves account erasure is
// complete across every user-scoped table — including register_pickup_tokens
// (which has no foreign key) and oidc_identities — so no orphaned auth-linkage
// rows survive a delete. This guards the GDPR right-to-erasure contract
// independently of whether ON DELETE CASCADE is enforced.
//
// "Every user-scoped table" is derived from the live schema, not listed here:
// the set under test is whatever carries a user_id column after the migrations
// have run. That makes both halves of a forgotten table fail. A table with no
// seed fails before the erasure ("the fixture covers nothing here"), and a
// seeded table whose rows survive fails after it — so a later migration adding
// a user-scoped table turns this test RED whether or not anyone remembers to
// touch it.
//
// The derivation is per DIALECT, because the migration sets are: OpenDatabase
// applies the embedded migrations of the driver it opened, so a table created
// only in migrations/postgres never exists in a SQLite schema and a SQLite-only
// derivation could not see it. The scenario therefore runs once per supported
// engine — SQLite unconditionally, Postgres behind the shared docker-gated
// container helper, which skips when docker is absent. The SQLite arm is what
// keeps the guard non-vacuous on a host without docker.
func TestDeleteAccountAndRelatedDataRemovesAllUserRows(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		dir := t.TempDir()
		database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(dir, "erasure.db")})
		requireNoErr(t, err, "open sqlite")
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		assertAccountErasureLeavesNoUserScopedRows(t, database)
	})

	t.Run("postgres", func(t *testing.T) {
		// startPostgresTestConfig skips the subtest when docker is unavailable;
		// the sqlite arm above still runs, so the guard never goes vacuous.
		assertAccountErasureLeavesNoUserScopedRows(t, openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t)))
	})
}

// assertAccountErasureLeavesNoUserScopedRows runs the whole erasure scenario —
// seed, derive the user-scoped set from the schema, gate that the fixture
// covers it, erase, sweep — against an already-migrated database, so the same
// assertions bind on every dialect the repository ships migrations for.
// It is deliberately NOT a t.Helper: marking it one would report every failure
// at the two call sites above, hiding which of the scenario's assertions broke.
func assertAccountErasureLeavesNoUserScopedRows(t *testing.T, database *gorm.DB) {
	repos := NewRepositories(database)

	user := &models.User{
		Email:            "erase@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	requireNoErr(t, repos.Users.Create(context.Background(), user), "create user")

	seed := []any{
		&models.DailyLog{UserID: user.ID, Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		&models.SymptomType{UserID: user.ID, Name: "custom", Color: "#AABBCC"},
		&models.RegisterPickupToken{Nonce: "nonce-1", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(), CreatedAt: time.Now().UTC()},
		&models.OIDCIdentity{UserID: user.ID, Issuer: "https://idp.example.com", Subject: "subject-1", CreatedAt: time.Now().UTC()},
		// oidc_logout_states: a row minted for this owner carries user_id and is
		// erased explicitly (sess-user). sess-expired and sess-live are seeded
		// through GORM with UserID unset — a plain uint, so they persist user_id=0,
		// NOT NULL (genuine pre-031 NULL rows no longer exist; migration 033
		// purged them). They stand in for rows belonging to no/another owner: the
		// expired one is dropped by the best-effort post-commit purge
		// (sess-expired), the unexpired one survives this owner's erasure and is
		// bounded by its own TTL (sess-live).
		&models.OIDCLogoutState{SessionID: "sess-user", UserID: user.ID, EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		&models.OIDCLogoutState{SessionID: "sess-expired", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(-time.Minute).UTC()},
		&models.OIDCLogoutState{SessionID: "sess-live", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(time.Hour).UTC()},
	}
	for _, row := range seed {
		requireNoErr(t, database.Create(row).Error, "seed row")
	}

	// The set under test comes from the schema, so it cannot drift behind a
	// migration the way a literal list does.
	scopedTables := userScopedTablesFromSchema(t, database)
	if len(scopedTables) == 0 {
		t.Fatal("derived no user-scoped tables from the live schema — the derivation is broken, not the schema")
	}
	t.Logf("user-scoped tables derived from the live schema: %v", scopedTables)

	// Positive anchor, and the fixture-coverage gate in one: every derived
	// table must actually hold a row for this account before the erasure runs.
	// Without it the post-erasure count of a table nothing seeded would be zero
	// for the wrong reason, and the sweep would pass while proving nothing
	// about it.
	for _, table := range scopedTables {
		if seeded := countUserRowsInTable(t, database, table, user.ID); seeded == 0 {
			t.Fatalf("%s carries a %s column but this test seeds no row for the account under erasure — add a seed row above, and thread the table through DeleteAccountAndRelatedData, or record it in erasureUserScopeExemptions with the reason", table, userScopeColumn)
		}
	}

	requireNoErr(t, repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID), "delete account")

	if usersLeft := countWhere(t, database, &models.User{}, "id = ?", user.ID); usersLeft != 0 {
		t.Fatalf("users still has %d row(s) for the deleted account", usersLeft)
	}

	for _, table := range scopedTables {
		if remaining := countUserRowsInTable(t, database, table, user.ID); remaining != 0 {
			t.Fatalf("%s still has %d row(s) for the deleted user — account erasure incomplete", table, remaining)
		}
	}

	// The post-commit housekeeping purge drops expired logout states and keeps
	// unexpired ones (they age out via their own TTL).
	if expiredLeft := countWhere(t, database, &models.OIDCLogoutState{}, "session_id = ?", "sess-expired"); expiredLeft != 0 {
		t.Fatalf("expected expired oidc_logout_states row to be purged after erasure, found %d", expiredLeft)
	}
	if liveLeft := countWhere(t, database, &models.OIDCLogoutState{}, "session_id = ?", "sess-live"); liveLeft != 1 {
		t.Fatalf("expected unexpired oidc_logout_states row to survive erasure, found %d", liveLeft)
	}
}

// TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError exercises the
// error-return branches for the explicit oidc_identities, register_pickup_tokens,
// and oidc_logout_states deletes: when one child delete fails mid-transaction,
// the whole erasure must roll back so the account is not left half-deleted.
func TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dropTable string
	}{
		{name: "register_pickup_tokens delete fails", dropTable: "register_pickup_tokens"},
		{name: "oidc_identities delete fails", dropTable: "oidc_identities"},
		{name: "oidc_logout_states delete fails", dropTable: "oidc_logout_states"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(dir, "delerr.db")})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})
			repos := NewRepositories(database)
			user := &models.User{
				Email:            "delerr@example.com",
				PasswordHash:     "hash",
				RecoveryCodeHash: "recovery",
				Role:             models.RoleOwner,
				CycleLength:      models.DefaultCycleLength,
				PeriodLength:     models.DefaultPeriodLength,
				AutoPeriodFill:   true,
				CreatedAt:        time.Now().UTC(),
			}
			if err := repos.Users.Create(context.Background(), user); err != nil {
				t.Fatalf("create user: %v", err)
			}

			// Drop a child table so its delete inside the transaction errors,
			// hitting the error-return branch and forcing a rollback.
			if err := database.Exec("DROP TABLE " + tc.dropTable).Error; err != nil {
				t.Fatalf("drop %s: %v", tc.dropTable, err)
			}

			if err := repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID); err == nil {
				t.Fatal("expected an error when a child-table delete fails, got nil")
			}

			var usersLeft int64
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Count(&usersLeft).Error; err != nil {
				t.Fatalf("count users: %v", err)
			}
			if usersLeft != 1 {
				t.Fatalf("transaction must roll back on delete error; user rows = %d, want 1", usersLeft)
			}
		})
	}
}
