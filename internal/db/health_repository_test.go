package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

func openHealthProbeDB(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "health-probe.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}

func TestHealthRepositoryPingSucceedsAgainstAnOpenDatabase(t *testing.T) {
	repo := NewHealthRepository(openHealthProbeDB(t))

	if err := repo.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() unexpected error: %v", err)
	}
}

// TestHealthRepositoryPingFailsOnceTheHandleIsClosed is the failure this probe
// exists to detect: the process is still running and serving, but the storage
// layer underneath it is gone. A probe that cannot see that is a probe that
// reports ready forever.
func TestHealthRepositoryPingFailsOnceTheHandleIsClosed(t *testing.T) {
	database := openHealthProbeDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	if err := NewHealthRepository(database).Ping(context.Background()); err == nil {
		t.Fatal("expected Ping to fail against a closed database handle")
	}
}

// TestHealthRepositoryPingHonorsTheCallerContext pins the ctx threading the
// readiness service depends on: the deadline it sets has to reach the driver,
// or a stalled engine would outlive the request that probed it.
func TestHealthRepositoryPingHonorsTheCallerContext(t *testing.T) {
	repo := NewHealthRepository(openHealthProbeDB(t))

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	if err := repo.Ping(ctx); err == nil {
		t.Fatal("expected Ping to fail on an expired context")
	}
}
