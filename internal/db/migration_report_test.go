package db

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestOpenDatabaseReportingNamesWhatTheRunnerApplied(t *testing.T) {
	embedded, err := loadEmbeddedMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations: %v", err)
	}
	newest := embedded[len(embedded)-1].Version
	config := Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "report.db")}

	database, report, err := OpenDatabaseReporting(config)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	closeOpenedPool(database)
	if report.Applied != len(embedded) || report.From != "" || report.To != newest {
		t.Fatalf("fresh database report = %+v, want Applied=%d From=\"\" To=%s", report, len(embedded), newest)
	}
	if got, want := report.String(), "migrations: applied "+strconv.Itoa(len(embedded))+" (empty -> "+newest+")"; got != want {
		t.Fatalf("fresh database line = %q, want %q", got, want)
	}

	database, report, err = OpenDatabaseReporting(config)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	closeOpenedPool(database)
	if report.Applied != 0 || report.To != newest {
		t.Fatalf("up-to-date report = %+v, want Applied=0 To=%s", report, newest)
	}
	if got, want := report.String(), "migrations: none pending, schema version "+newest; got != want {
		t.Fatalf("up-to-date line = %q, want %q", got, want)
	}
}

func TestMigrationReportNamesTheStartingVersionOfAnUpgrade(t *testing.T) {
	report := MigrationReport{Applied: 12, From: "031", To: "043"}
	if got, want := report.String(), "migrations: applied 12 (031 -> 043)"; got != want {
		t.Fatalf("upgrade line = %q, want %q", got, want)
	}
}
