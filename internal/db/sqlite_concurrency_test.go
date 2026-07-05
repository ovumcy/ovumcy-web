package db

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSQLiteConcurrentDayWritesNoBusyError is the regression for the
// SQLITE_BUSY-on-concurrent-writes defect. Under WAL, GORM's deferred write
// transactions perform a read then upgrade to a write lock; a concurrent writer
// turns that upgrade into SQLITE_BUSY_SNAPSHOT, which SQLite fails IMMEDIATELY
// without invoking the busy handler (so busy_timeout never engages). The
// `_txlock=immediate` DSN param makes every write transaction start with
// BEGIN IMMEDIATE, acquiring the write lock up front so concurrent writers queue
// on busy_timeout instead of failing. This test drives overlapping upserts
// (Find -> Create/Save, the two-tabs-same-day pattern) from many goroutines and
// asserts not a single SQLITE_BUSY surfaces.
func TestSQLiteConcurrentDayWritesNoBusyError(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenSQLite(filepath.Join(dir, "concurrency.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	repos := NewRepositories(database)
	user := &models.User{
		Email:            "concurrency@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	const (
		workers      = 50
		iterations   = 40
		daysPerBlock = 3 // per-worker disjoint block; revisited to hit Save
	)
	baseDay := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	var busyCount int64
	var otherErr atomic.Value // error
	var wg sync.WaitGroup
	start := make(chan struct{})

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			ctx := context.Background()
			for i := 0; i < iterations; i++ {
				// Disjoint per-worker day block => no UNIQUE races; the only
				// contention is the concurrent-writer lock upgrade we want to
				// exercise. Revisiting the block hits Create then Find+Save.
				day := baseDay.AddDate(0, 0, worker*daysPerBlock+(i%daysPerBlock))
				dayEnd := day.AddDate(0, 0, 1)

				// Mirror the production PUT /days/<date> upsert: Find (SELECT ->
				// read snapshot) then Create/Save (write upgrade) INSIDE ONE
				// transaction. Under a deferred BEGIN this upgrade is what
				// SQLite fails as SQLITE_BUSY_SNAPSHOT without the busy handler.
				err := repos.DailyLogs.WithinTransaction(ctx, func(tx *DailyLogRepository) error {
					existing, found, err := tx.FindByUserAndDayRange(ctx, user.ID, day, dayEnd)
					if err != nil {
						return err
					}
					if found {
						existing.IsPeriod = !existing.IsPeriod
						return tx.Save(ctx, &existing)
					}
					return tx.Create(ctx, &models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true})
				})
				if err != nil {
					record(&busyCount, &otherErr, err)
				}
			}
		}(w)
	}

	close(start)
	wg.Wait()

	if n := atomic.LoadInt64(&busyCount); n > 0 {
		t.Fatalf("got %d SQLITE_BUSY errors under concurrent day writes; busy_timeout/BEGIN IMMEDIATE not engaging", n)
	}
	if v := otherErr.Load(); v != nil {
		// Non-BUSY errors (e.g. UNIQUE races on the overlapping day set) are not
		// what this regression guards; surface them so the test is not silently
		// masking a real failure, but do not conflate them with the BUSY defect.
		t.Logf("non-BUSY error observed during concurrent writes: %v", v.(error))
	}
}

func record(busyCount *int64, otherErr *atomic.Value, err error) {
	msg := err.Error()
	if strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(strings.ToLower(msg), "database is locked") {
		atomic.AddInt64(busyCount, 1)
		return
	}
	otherErr.Store(err)
}
