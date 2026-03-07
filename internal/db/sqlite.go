package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openSQLiteConnection(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_foreign_keys=on&_busy_timeout=5000", dbPath)
	database, err := gorm.Open(sqlite.Open(dsn), newGORMConfig(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	return database, nil
}
