package db

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openSQLiteConnection(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	// glebarez/modernc honors only the `_pragma=NAME(VALUE)` DSN form; the
	// mattn-style `_foreign_keys=on&_busy_timeout=5000` is silently ignored,
	// leaving foreign_keys OFF (ON DELETE CASCADE never fires for pooled CRUD
	// connections) and the journal in rollback mode. Use the `_pragma` form so
	// every connection in the pool enforces FKs, waits on a busy lock, and runs
	// in WAL. Regression: sqlite_pragma_test.go.
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	database, err := gorm.Open(sqlite.Open(dsn), newGORMConfig(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Cap the connection pool for SQLite. WAL mode supports concurrent reads
	// but has a single writer; an unbounded pool causes unnecessary writer
	// contention and goroutine churn. 4 conns cover the typical single-user
	// workload with headroom for background tasks.
	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get sqlite sql.DB: %w", err) // codecov:ignore -- database.DB() does not fail after a successful gorm.Open
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return database, nil
}
