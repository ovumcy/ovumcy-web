package main

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

func TestLogMigrationStatusNamesTheSchemaVersion(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "migration-status.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	logMigrationStatus(database)
	out := buf.String()
	if !strings.Contains(out, "migrations applied:") || strings.Contains(out, "schema version \n") {
		t.Fatalf("unexpected log line %q", out)
	}

	buf.Reset()
	if err := database.Exec("DROP TABLE schema_migrations").Error; err != nil {
		t.Fatal(err)
	}
	logMigrationStatus(database)
	if !strings.Contains(buf.String(), "migration status unavailable") {
		t.Fatalf("missing failure line: %q", buf.String())
	}
}
