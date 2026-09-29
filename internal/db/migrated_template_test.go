package db

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The migrated-schema template: tests that only need a migrated SQLite schema
// get a private COPY of one file, built once per test binary, instead of each
// replaying the whole migration chain. The template path is never handed to a
// test: migratedSQLiteConfig copies it to the caller's path and returns a
// Config for the copy. Tests that assert migration behaviour, schema_migrations
// rows, pragmas set by a first open, or open a pre-existing database keep
// calling OpenDatabase on a fresh path.
var (
	migratedTemplateOnce sync.Once
	migratedTemplateDir  string
	migratedTemplatePath string
	migratedTemplateErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if migratedTemplateDir != "" {
		_ = os.RemoveAll(migratedTemplateDir)
	}
	os.Exit(code)
}

func buildMigratedTemplate() {
	dir, err := os.MkdirTemp("", "ovumcy-db-template-")
	if err != nil {
		migratedTemplateErr = fmt.Errorf("create template dir: %w", err)
		return
	}
	migratedTemplateDir = dir
	path := filepath.Join(dir, "template.db")

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: path})
	if err != nil {
		migratedTemplateErr = fmt.Errorf("migrate template: %w", err)
		return
	}
	sqlDB, err := database.DB()
	if err != nil {
		migratedTemplateErr = fmt.Errorf("template sql.DB: %w", err)
		return
	}
	// Fold the WAL into the main file, then close: only a fully closed
	// database is copied, so no -wal/-shm sidecar is needed by the copy.
	if _, err := sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		migratedTemplateErr = fmt.Errorf("checkpoint template: %w", err)
		return
	}
	if err := sqlDB.Close(); err != nil {
		migratedTemplateErr = fmt.Errorf("close template: %w", err)
		return
	}
	migratedTemplatePath = path
}

// migratedSQLiteConfig copies the migrated template to path and returns a
// Config that opens the copy.
func migratedSQLiteConfig(t testing.TB, path string) Config {
	t.Helper()
	migratedTemplateOnce.Do(buildMigratedTemplate)
	if migratedTemplateErr != nil {
		t.Fatalf("migrated template: %v", migratedTemplateErr)
	}
	content, err := os.ReadFile(migratedTemplatePath)
	if err != nil {
		t.Fatalf("read migrated template: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create copy dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write migrated copy: %v", err)
	}
	return Config{Driver: DriverSQLite, SQLitePath: path}
}
