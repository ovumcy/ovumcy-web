package db

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// migrationReplayStore is one engine a data-migration fixture is replayed on.
// open returns a fresh handle on the SAME storage on every call, so a test can
// seed through one handle, close it, and let the next open re-run the migration.
type migrationReplayStore struct {
	driver Driver
	open   func(t *testing.T) *gorm.DB
}

// assertMigrationsApplied checks that every embedded migration of the store's
// own driver is recorded on database.
func (store migrationReplayStore) assertMigrationsApplied(t *testing.T, database *gorm.DB) {
	t.Helper()
	assertAllEmbeddedMigrationsAppliedForDriver(t, database, store.driver)
}

// forEachMigrationReplayStore runs fn once per supported engine. A data
// migration ships as two files, one per dialect; the parity checks compare the
// file names and the resulting schema but not what each file's predicate does to
// rows, so a fixture replayed on SQLite alone leaves the Postgres file running
// only against empty tables. The Postgres subtest skips where no Postgres is
// available (it is required in CI), so the SQLite arm keeps the fixture live.
func forEachMigrationReplayStore(t *testing.T, fn func(t *testing.T, store migrationReplayStore)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "replay.db")
		fn(t, migrationReplayStore{
			driver: DriverSQLite,
			open: func(t *testing.T) *gorm.DB {
				return openSQLiteForMigrationBootstrapTest(t, databasePath)
			},
		})
	})

	t.Run("postgres", func(t *testing.T) {
		config := startPostgresTestConfig(t)
		fn(t, migrationReplayStore{
			driver: DriverPostgres,
			open: func(t *testing.T) *gorm.DB {
				return openPostgresForMigrationBootstrapTest(t, config)
			},
		})
	})
}

// closeForMigrationReplay releases database so the next store.open re-runs the
// migrations against the same storage.
func closeForMigrationReplay(t *testing.T, database *gorm.DB) {
	t.Helper()
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}
