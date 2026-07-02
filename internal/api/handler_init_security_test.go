package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

func TestNewHandlerRejectsEmptySecret(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-handler-init-test.db")

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	_, err = NewHandler("   ", time.UTC, i18nManager, false, newTestHandlerDependencies(database, i18nManager))
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
	if err.Error() != "secret key is required" {
		t.Fatalf("expected error %q, got %q", "secret key is required", err.Error())
	}
}
