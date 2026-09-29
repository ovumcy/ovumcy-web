package bootstrap

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestVerifySchemaInvariantsRefusesAMissingNormalizedEmailIndex drives the boot
// check against a real migrated database, before and after the index is dropped
// out of band. The index shapes it refuses are covered per engine in internal/db;
// this pins that the boot entry point reaches that check and returns its refusal.
func TestVerifySchemaInvariantsRefusesAMissingNormalizedEmailIndex(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "schema-invariants.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	repositories, _ := BuildRepositories(database, "")

	if err := VerifySchemaInvariants(context.Background(), repositories); err != nil {
		t.Fatalf("a freshly migrated database must pass, got %v", err)
	}

	if err := database.Exec("DROP INDEX " + db.NormalizedEmailIndexName).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
	err = VerifySchemaInvariants(context.Background(), repositories)
	if err == nil || !strings.Contains(err.Error(), db.NormalizedEmailIndexName) {
		t.Fatalf("a database without %s must be refused by name, got %v", db.NormalizedEmailIndexName, err)
	}
}
