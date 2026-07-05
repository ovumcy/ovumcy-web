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
	//
	// `_txlock=immediate` makes every write transaction open with BEGIN
	// IMMEDIATE. Without it, GORM's deferred write transactions take a read
	// snapshot (the day upsert's SELECT) then upgrade to a write lock; under WAL
	// a concurrent writer turns that upgrade into SQLITE_BUSY_SNAPSHOT (extended
	// code 517), which SQLite fails IMMEDIATELY without invoking the busy handler
	// — so busy_timeout never engages and concurrent day writes surface as 500s.
	// BEGIN IMMEDIATE acquires the write lock up front, so contending writers
	// queue on busy_timeout instead. Regression: sqlite_concurrency_test.go.
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate", dbPath)
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
