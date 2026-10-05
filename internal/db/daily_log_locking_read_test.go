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

// lockingReadUsers is the settings source for the dry-run day writes: a
// dry-run database returns no user row, and the writes below need settings
// that let them run to their day saves.
type lockingReadUsers struct{}

func (lockingReadUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{PeriodLength: 5, AutoPeriodFill: true}, nil
}

func (lockingReadUsers) UpdateByID(context.Context, uint, map[string]any) error { return nil }

// captureDayWriteReads runs write against a PostgreSQL dry-run database whose
// single-day reads each answer with a stored period day, so every write runs
// on to its save, and returns the SQL of each single-day read it made.
func captureDayWriteReads(t *testing.T, write func(*services.DayService) error) []string {
	t.Helper()
	database, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=ovumcy dbname=ovumcy sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run postgres: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	var reads []string
	if err := database.Callback().Query().After("gorm:query").Register("test:capture_day_write_reads", func(tx *gorm.DB) {
		row, single := tx.Statement.Dest.(*models.DailyLog)
		if !single {
			return
		}
		reads = append(reads, tx.Statement.SQL.String())
		*row = models.DailyLog{ID: 1, UserID: 7, Date: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC), IsPeriod: true}
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatalf("register capture callback: %v", err)
	}
	service := services.NewDayServiceWithTx(NewDailyLogRepository(database), lockingReadUsers{}, nil)
	if err := write(service); err != nil {
		t.Fatalf("dry-run day write: %v", err)
	}
	return reads
}

// TestEveryMergedDayReadLocksTheRowOnPostgres: each write that saves a day row
// it read, or builds its values from one, reads that row with SELECT … FOR
// UPDATE — the full write (PUT) and its hidden-field preservation, the manual
// cycle-start mark, and the period autofill and its clearing. A plain read
// there lets the write revert a concurrent write of the same day committed
// after the read.
func TestEveryMergedDayReadLocksTheRowOnPostgres(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	writes := map[string]func(*services.DayService) error{
		"full day write": func(service *services.DayService) error {
			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 7, day,
				services.DayEntryInput{IsPeriod: true, Flow: models.FlowLight, Mood: 3, PreserveNotes: true}, day, time.UTC)
			return err
		},
		"manual cycle-start mark": func(service *services.DayService) error {
			return service.MarkCycleStartManually(context.Background(), 7, day, day, time.UTC, services.ManualCycleStartOptions{})
		},
		"period autofill": func(service *services.DayService) error {
			return service.AutoFillFollowingPeriodDays(context.Background(), 7, day, 3, models.FlowLight, day.AddDate(0, 0, 5), time.UTC)
		},
		"autofill clearing": func(service *services.DayService) error {
			return service.ClearAutoFilledPeriodNeighbors(context.Background(), 7, day, 3, models.FlowLight, time.UTC)
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			reads := captureDayWriteReads(t, write)
			if len(reads) == 0 {
				t.Fatal("expected the write to read the day it saves")
			}
			for _, read := range reads {
				if !strings.HasSuffix(strings.TrimSpace(read), "FOR UPDATE") {
					t.Fatalf("expected every day read this write merges onto to lock the row, got %q", read)
				}
			}
		})
	}
}

// TestPutAfterAConcurrentPatchKeepsTheHiddenFieldOnPostgres: a full write
// (PUT) from an account that hides the notes field preserves the stored notes.
// When a partial write (PATCH) changing those notes is still open as the PUT
// reads, the PUT must wait and preserve the notes that PATCH leaves — a plain
// read hands it the old notes, and its write reverts the PATCH.
func TestPutAfterAConcurrentPatchKeepsTheHiddenFieldOnPostgres(t *testing.T) {
	database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repos := NewRepositories(database)
	userID := createDailyLogTestUser(t, database, "put-after-patch@example.com")
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	if err := repos.DailyLogs.Create(ctx, &models.DailyLog{UserID: userID, Date: day, Mood: 1, Notes: "before"}); err != nil {
		t.Fatalf("seed day: %v", err)
	}

	patchHeld := make(chan struct{})
	releasePatch := make(chan struct{})
	var completed atomic.Int32
	runner := func(ctx context.Context, fn func(services.DayLogRepository) error) error {
		return repos.DailyLogs.WithinTransaction(ctx, func(tx *DailyLogRepository) error {
			if err := fn(tx); err != nil {
				return err
			}
			if completed.Add(1) == 1 {
				close(patchHeld)
				<-releasePatch
			}
			return nil
		})
	}
	service := services.NewDayServiceWithTx(repos.DailyLogs, repos.Users, runner)

	var wait sync.WaitGroup
	var patchErr, putErr error
	wait.Go(func() {
		_, patchErr = service.PatchDayEntryWithAutoFillAt(ctx, userID, day,
			services.DayEntryInput{Notes: "after"}, services.DayEntryFields{Notes: true}, time.Now(), time.UTC)
	})
	select {
	case <-patchHeld:
	case <-ctx.Done():
		t.Fatalf("partial write never reached its commit: %v", patchErr)
	}
	wait.Go(func() {
		_, putErr = service.UpsertDayEntryWithAutoFillAt(ctx, userID, day,
			services.DayEntryInput{Flow: models.FlowNone, Mood: 3, PreserveNotes: true}, time.Now(), time.UTC)
	})
	// Long enough for the full write to reach its read; with the lock it waits
	// there for the partial write's commit.
	time.Sleep(300 * time.Millisecond)
	close(releasePatch)
	wait.Wait()

	if patchErr != nil || putErr != nil {
		t.Fatalf("expected both writes to succeed, got patch: %v, put: %v", patchErr, putErr)
	}
	stored, found, err := repos.DailyLogs.FindByUserAndDayRange(ctx, userID, day, day.AddDate(0, 0, 1))
	if err != nil || !found {
		t.Fatalf("load day: found=%v err=%v", found, err)
	}
	if stored.Mood != 3 || stored.Notes != "after" {
		t.Fatalf("expected the PUT's mood and the PATCH's hidden notes (mood=3, notes=%q), got mood=%d notes=%q: the full write reverted the partial one", "after", stored.Mood, stored.Notes)
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
