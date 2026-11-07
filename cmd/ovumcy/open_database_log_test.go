package main

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

func TestMustOpenDatabaseLogsTheMigrationReport(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	database := mustOpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "boot.db")})
	t.Cleanup(func() { closeDatabase(database) })

	if out := buf.String(); !strings.Contains(out, "migrations: applied ") || !strings.Contains(out, ", schema version ") {
		t.Fatalf("boot log = %q, want the migration report line", out)
	}
}
