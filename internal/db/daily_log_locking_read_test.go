package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// A partial day write (PATCH /api/v1/days/{date}) merges onto the stored row
// inside its transaction. On PostgreSQL READ COMMITTED a plain SELECT lets two
// such writes both merge onto the same old row, and the later commit erases the
// field the earlier one stated; the read they merge onto must lock the row.

// captureDailyLogReadSQL builds both day reads against dialector without a
// server (DryRun) and returns the SQL each would send.
func captureDailyLogReadSQL(t *testing.T, dialector gorm.Dialector) (plain string, locking string) {
	t.Helper()
	database, err := gorm.Open(dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run %s: %v", dialector.Name(), err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	var captured string
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_day_read_sql", func(tx *gorm.DB) {
		captured = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register capture callback: %v", err)
	}
	repo := NewDailyLogRepository(database)
	dayStart := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	if _, _, err := repo.FindByUserAndDayRange(context.Background(), 7, dayStart, dayEnd); err != nil {
		t.Fatalf("dry-run plain read: %v", err)
	}
	plain = captured
	if _, _, err := repo.FindByUserAndDayRangeForUpdate(context.Background(), 7, dayStart, dayEnd); err != nil {
		t.Fatalf("dry-run locking read: %v", err)
	}
	locking = captured
	if plain == "" || locking == "" {
		t.Fatalf("expected both reads to build SQL, got plain=%q locking=%q", plain, locking)
	}
	return plain, locking
}

func TestDailyLogLockingReadIsSelectForUpdateOnPostgres(t *testing.T) {
	plain, locking := captureDailyLogReadSQL(t, postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=ovumcy dbname=ovumcy sslmode=disable",
	}))
	if !strings.HasSuffix(strings.TrimSpace(locking), "FOR UPDATE") {
		t.Fatalf("expected the read a day merge depends on to lock the row (SELECT … FOR UPDATE), got %q", locking)
	}
	if strings.Contains(plain, "FOR UPDATE") {
		t.Fatalf("expected the plain day read to take no lock, got %q", plain)
	}
}

func TestDailyLogLockingReadEmitsNoLockClauseOnSQLite(t *testing.T) {
	plain, locking := captureDailyLogReadSQL(t, sqlite.Open(filepath.Join(t.TempDir(), "dry-run.db")))
	if strings.Contains(strings.ToUpper(locking), " FOR ") {
		t.Fatalf("expected SQLite, which has no row locks, to get no lock clause, got %q", locking)
	}
	if strings.TrimSpace(locking) != strings.TrimSpace(plain) {
		t.Fatalf("expected the SQLite locking read to be the plain read, got plain=%q locking=%q", plain, locking)
	}
}

// assertConcurrentDayPatchesBothLand runs two partial writes of one day, each
// naming a different field, through the production transaction runner. The
// first write's transaction is held open after its write until the second has
// had time to read; the second must then merge onto the first's row, not onto
// the row both started from. seedExisting chooses between a stored day and a
// day with no row yet, where both writes insert.
func assertConcurrentDayPatchesBothLand(t *testing.T, database *gorm.DB, seedExisting bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repos := NewRepositories(database)
	userID := createDailyLogTestUser(t, database, "concurrent-day-patch@example.com")
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	if seedExisting {
		if err := repos.DailyLogs.Create(ctx, &models.DailyLog{UserID: userID, Date: day, Mood: 1, Notes: "before"}); err != nil {
			t.Fatalf("seed day: %v", err)
		}
	}

	firstHeld := make(chan struct{})
	releaseFirst := make(chan struct{})
	var completed atomic.Int32
	runner := func(ctx context.Context, fn func(services.DayLogRepository) error) error {
		return repos.DailyLogs.WithinTransaction(ctx, func(tx *DailyLogRepository) error {
			if err := fn(tx); err != nil {
				return err
			}
			if completed.Add(1) == 1 {
				close(firstHeld)
				<-releaseFirst
			}
			return nil
		})
	}
	service := services.NewDayServiceWithTx(repos.DailyLogs, repos.Users, runner)

	var wait sync.WaitGroup
	var moodErr, notesErr error
	wait.Go(func() {
		_, moodErr = service.PatchDayEntryWithAutoFillAt(ctx, userID, day,
			services.DayEntryInput{Mood: 3}, services.DayEntryFields{Mood: true}, time.Now(), time.UTC)
	})
	select {
	case <-firstHeld:
	case <-ctx.Done():
		t.Fatalf("first partial write never reached its commit: %v", moodErr)
	}
	wait.Go(func() {
		_, notesErr = service.PatchDayEntryWithAutoFillAt(ctx, userID, day,
			services.DayEntryInput{Notes: "after"}, services.DayEntryFields{Notes: true}, time.Now(), time.UTC)
	})
	// Long enough for the second write to reach its read; with the lock it
	// waits there for the first commit.
	time.Sleep(300 * time.Millisecond)
	close(releaseFirst)
	wait.Wait()

	if moodErr != nil || notesErr != nil {
		t.Fatalf("expected both partial writes to succeed, got mood write: %v, notes write: %v", moodErr, notesErr)
	}
	stored, found, err := repos.DailyLogs.FindByUserAndDayRange(ctx, userID, day, day.AddDate(0, 0, 1))
	if err != nil || !found {
		t.Fatalf("load day: found=%v err=%v", found, err)
	}
	if stored.Mood != 3 || stored.Notes != "after" {
		t.Fatalf("expected both stated fields kept (mood=3, notes=%q), got mood=%d notes=%q: one partial write erased the other's field", "after", stored.Mood, stored.Notes)
	}
}

func TestConcurrentDayPatchesBothLandOnPostgres(t *testing.T) {
	database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	t.Run("stored day", func(t *testing.T) {
		assertConcurrentDayPatchesBothLand(t, database, true)
	})
	if err := database.Exec(`DELETE FROM daily_logs`).Error; err != nil {
		t.Fatalf("clear days: %v", err)
	}
	if err := database.Exec(`DELETE FROM users`).Error; err != nil {
		t.Fatalf("clear users: %v", err)
	}
	t.Run("day with no row", func(t *testing.T) {
		assertConcurrentDayPatchesBothLand(t, database, false)
	})
}

func TestConcurrentDayPatchesBothLandOnSQLite(t *testing.T) {
	for _, seedExisting := range []bool{true, false} {
		database, err := OpenDatabase(migratedSQLiteConfig(t, filepath.Join(t.TempDir(), "patch.db")))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		assertConcurrentDayPatchesBothLand(t, database, seedExisting)
	}
}

// TestDailyLogCreateReportsADuplicateDayAsAUniqueConstraintError pins the
// signal the partial write retries on: a second insert of the same owner's day
// is a UniqueConstraintError, not an anonymous failure.
func TestDailyLogCreateReportsADuplicateDayAsAUniqueConstraintError(t *testing.T) {
	database, err := OpenDatabase(migratedSQLiteConfig(t, filepath.Join(t.TempDir(), "dup.db")))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewDailyLogRepository(database)
	userID := createDailyLogTestUser(t, database, "duplicate-day@example.com")
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	if err := repo.Create(context.Background(), &models.DailyLog{UserID: userID, Date: day}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err = repo.Create(context.Background(), &models.DailyLog{UserID: userID, Date: day})
	var uniqueErr *UniqueConstraintError
	if !errors.As(err, &uniqueErr) {
		t.Fatalf("expected a UniqueConstraintError for a second insert of the same day, got %v", err)
	}
}
