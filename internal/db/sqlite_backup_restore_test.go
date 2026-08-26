package db

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestSQLiteBackupRestorePreservesHealthData proves that the standard self-host
// backup procedure — close the app, copy the SQLite file, reopen it elsewhere —
// preserves all logged health data, including the JSON-serialized symptom and
// cycle-factor fields. For a tracker that owns the user's records, silent data
// loss or corruption on restore would be the worst-case failure, so it is worth
// an explicit regression.
func TestSQLiteBackupRestorePreservesHealthData(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "ovumcy.db")

	user, seedLogs := seedBackupSourceDatabase(t, originalPath)

	backupPath := filepath.Join(dir, "ovumcy-backup.db")
	copyFileForBackupTest(t, originalPath, backupPath)

	restoredDB, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: backupPath})
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := restoredDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	restoredRepos := NewRepositories(restoredDB)

	restoredUser, err := restoredRepos.Users.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find restored user: %v", err)
	}
	if restoredUser.Email != user.Email || restoredUser.Role != user.Role {
		t.Fatalf("restored user mismatch: email=%q role=%q", restoredUser.Email, restoredUser.Role)
	}

	restoredLogs, err := restoredRepos.DailyLogs.ListByUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("list restored logs: %v", err)
	}
	assertRestoredLogsMatch(t, seedLogs, restoredLogs)
}

// seedBackupSourceDatabase opens a fresh SQLite database at path, seeds it with a
// user and a few representative day logs, then closes the connection so SQLite
// flushes (and checkpoints any WAL) into the main file before it is copied.
func seedBackupSourceDatabase(t *testing.T, path string) (*models.User, []models.DailyLog) {
	t.Helper()

	originalDB, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: path})
	if err != nil {
		t.Fatalf("open original database: %v", err)
	}
	originalRepos := NewRepositories(originalDB)

	user := &models.User{
		Email:            "owner@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := originalRepos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	seedLogs := []models.DailyLog{
		{
			UserID: user.ID, Date: backupRestoreDay(t, "2026-02-01"),
			IsPeriod: true, CycleStart: true, Flow: "heavy", Mood: 3,
			Notes:           "first day, cramps",
			SymptomIDs:      []uint{1, 4, 7},
			CycleFactorKeys: []string{"stress", "travel"},
		},
		{
			UserID: user.ID, Date: backupRestoreDay(t, "2026-02-02"),
			IsPeriod: true, Flow: "light", BBT: new(36.5),
		},
		{
			UserID: user.ID, Date: backupRestoreDay(t, "2026-02-15"),
			SexActivity: "protected", CervicalMucus: "eggwhite",
		},
	}
	for i := range seedLogs {
		if err := originalRepos.DailyLogs.Create(context.Background(), &seedLogs[i]); err != nil {
			t.Fatalf("create day log %d: %v", i, err)
		}
	}

	if sqlDB, err := originalDB.DB(); err == nil {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close original database: %v", err)
		}
	}

	return user, seedLogs
}

// assertRestoredLogsMatch verifies the restored copy holds exactly the seeded
// logs, comparing both scalar fields and the JSON-serialized slice fields.
func assertRestoredLogsMatch(t *testing.T, seedLogs, restoredLogs []models.DailyLog) {
	t.Helper()

	if len(restoredLogs) != len(seedLogs) {
		t.Fatalf("expected %d restored logs, got %d", len(seedLogs), len(restoredLogs))
	}

	restoredByDate := make(map[string]models.DailyLog, len(restoredLogs))
	for _, log := range restoredLogs {
		restoredByDate[log.Date.Format("2006-01-02")] = log
	}

	for _, want := range seedLogs {
		key := want.Date.Format("2006-01-02")
		got, ok := restoredByDate[key]
		if !ok {
			t.Fatalf("missing restored log for %s", key)
		}
		if !dailyLogScalarsEqual(got, want) {
			t.Fatalf("scalar field mismatch for %s:\n got %+v\nwant %+v", key, got, want)
		}
		if !slices.Equal(got.SymptomIDs, want.SymptomIDs) {
			t.Fatalf("SymptomIDs mismatch for %s: got %v want %v", key, got.SymptomIDs, want.SymptomIDs)
		}
		if !slices.Equal(got.CycleFactorKeys, want.CycleFactorKeys) {
			t.Fatalf("CycleFactorKeys mismatch for %s: got %v want %v", key, got.CycleFactorKeys, want.CycleFactorKeys)
		}
	}
}

// dailyLogScalarsEqual compares every non-slice tracked field of two day logs.
func dailyLogScalarsEqual(a, b models.DailyLog) bool {
	return a.IsPeriod == b.IsPeriod && a.CycleStart == b.CycleStart &&
		a.Flow == b.Flow && a.Mood == b.Mood && a.Notes == b.Notes &&
		bbtPointersEqual(a.BBT, b.BBT) && a.SexActivity == b.SexActivity &&
		a.CervicalMucus == b.CervicalMucus
}

// bbtPointersEqual compares two nullable BBT values by content: both nil, or
// both set to the same reading. A pointer `==` would compare addresses.
func bbtPointersEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func backupRestoreDay(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", value, err)
	}
	return parsed
}

func copyFileForBackupTest(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open source for backup: %v", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create backup file: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("copy backup bytes: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close backup file: %v", err)
	}
}

// sqliteVolumeDatabaseFile is the database file inside the data volume, the one
// docs/self-hosted.md names beside its two WAL sidecars.
const sqliteVolumeDatabaseFile = "ovumcy.db"

// TestLiveWholeVolumeCaptureLosesACommitToACheckpointMidCapture takes the
// documented whole-volume archive apart at the one seam a sequential archiver
// has. `tar czf` reads `ovumcy.db`, then `ovumcy.db-shm`, then `ovumcy.db-wal`,
// one after another, and nothing holds the three still between those reads. A
// checkpoint landing in that window folds the WAL into the main database file
// AFTER the main file was read, and empties the WAL BEFORE the WAL is read — so
// the archive carries all three files, which is the property the runbook states,
// and still misses a commit that was in the database before the backup began.
//
// The test captures one database twice, and the only difference between the two
// captures is whether the app was stopped first:
//
//   - live, with a checkpoint in the window: the WAL-resident day is gone from
//     the restore, while the day written before the previous checkpoint is still
//     there — a partial archive, not an empty one;
//   - stopped, the way the runbook now tells an operator to take it: every day
//     reads back.
//
// The first half characterizes filesystem and WAL semantics, not a defect this
// repository can fix. If it ever goes green, a whole-volume archive no longer
// needs the stop step and docs/self-hosted.md should be relaxed back.
func TestLiveWholeVolumeCaptureLosesACommitToACheckpointMidCapture(t *testing.T) {
	sourceDir := t.TempDir()
	livePath := filepath.Join(sourceDir, sqliteVolumeDatabaseFile)

	liveDB, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: livePath})
	if err != nil {
		t.Fatalf("open live database: %v", err)
	}
	repos := NewRepositories(liveDB)
	user := createBackupRaceOwner(t, repos)

	createBackupRaceDay(t, repos, user.ID, "2026-03-01")
	checkpointWALTruncate(t, liveDB)

	// The commit the operator already has when the backup starts: written after
	// the previous checkpoint, so it lives in `ovumcy.db-wal` and nowhere else.
	createBackupRaceDay(t, repos, user.ID, "2026-03-02")
	assertWALCarriesBytes(t, livePath)

	liveCapture := filepath.Join(t.TempDir(), "live")
	captureWALSet(t, sourceDir, liveCapture, func() { checkpointWALTruncate(t, liveDB) })

	liveDays := restoredDayList(t, filepath.Join(liveCapture, sqliteVolumeDatabaseFile), user.ID)
	if !slices.Contains(liveDays, "2026-03-01") {
		t.Fatalf("the live capture restored %v — an archive that lost everything says nothing about the checkpoint window this test is about", liveDays)
	}
	if slices.Contains(liveDays, "2026-03-02") {
		t.Fatalf("the live capture restored %v, the WAL-resident commit included: a checkpoint between the main-file read and the sidecar reads no longer loses it here, so the whole-volume archive in docs/self-hosted.md can drop its stop step again", liveDays)
	}

	// The same database, in the same WAL-resident shape, captured after the app
	// was stopped — which is what the runbook requires.
	createBackupRaceDay(t, repos, user.ID, "2026-03-03")
	closeBackupTestDatabase(t, liveDB)

	stoppedCapture := filepath.Join(t.TempDir(), "stopped")
	captureWALSet(t, sourceDir, stoppedCapture, nil)

	stoppedDays := restoredDayList(t, filepath.Join(stoppedCapture, sqliteVolumeDatabaseFile), user.ID)
	for _, day := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		if !slices.Contains(stoppedDays, day) {
			t.Fatalf("the capture taken with the app stopped restored %v, missing %s: stopping first is the requirement docs/self-hosted.md states, so it has to be sufficient", stoppedDays, day)
		}
	}
}

// TestAHotCopyOfTheMainDatabaseFileAloneRestoresWithoutTheWALResidentDay pins
// the other hazard docs/self-hosted.md warns about, the one an operator reaches
// for when the whole-volume archive feels like too much: copying `ovumcy.db` on
// its own while the app is running.
//
// It is NOT the checkpoint race the test above takes apart, and it needs no
// race at all. Under WAL every commit lands in `ovumcy.db-wal` and reaches
// `ovumcy.db` only at a checkpoint, so the main file on its own is always a
// consistent database as of the LAST checkpoint — never the database as of the
// copy. Whatever was committed since is simply not in the bytes that were
// copied. Nothing reports it: the copy is not corrupt, it opens cleanly, it
// answers every query, and it is missing health records the owner entered.
// That is the loss docs/self-hosted.md closes with — "the same applies, for the
// same reason, if you copy individual files instead; stopping the app also
// checkpoints the WAL into the main database file" — and this is the arm that
// makes the sentence measurable.
//
// Like its neighbour, this characterizes SQLite and filesystem semantics rather
// than a defect this repository can fix, so it is written to be read as a known
// property. If the ABSENCE below ever fails, a single-file copy no longer loses
// the WAL and that sentence in the runbook can be relaxed.
func TestAHotCopyOfTheMainDatabaseFileAloneRestoresWithoutTheWALResidentDay(t *testing.T) {
	sourceDir := t.TempDir()
	livePath := filepath.Join(sourceDir, sqliteVolumeDatabaseFile)

	liveDB, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: livePath})
	if err != nil {
		t.Fatalf("open live database: %v", err)
	}
	defer closeBackupTestDatabase(t, liveDB)

	repos := NewRepositories(liveDB)
	user := createBackupRaceOwner(t, repos)

	// One day on each side of the last checkpoint: the first is inside
	// `ovumcy.db` itself, the second is only in the WAL beside it.
	createBackupRaceDay(t, repos, user.ID, "2026-04-01")
	checkpointWALTruncate(t, liveDB)
	createBackupRaceDay(t, repos, user.ID, "2026-04-02")
	assertWALCarriesBytes(t, livePath)

	// The copy an operator takes when they treat the database as one file: the
	// main file only, no sidecars, app still running.
	copyPath := filepath.Join(t.TempDir(), sqliteVolumeDatabaseFile)
	copyFileForBackupTest(t, livePath, copyPath)
	assertNoWALSidecarsBesideTheCopy(t, copyPath)

	// Nothing holds the WAL still across the residency check and the read of the
	// main file, and a checkpoint landing in that window would fold 2026-04-02
	// into the bytes just copied. Re-reading the WAL afterwards is what rules
	// that out: nothing writes to this database between the two checks, so a WAL
	// that still carries bytes was never folded away. Without it the absence
	// below could fail on a race while telling the reader to go and relax a
	// sentence in the runbook.
	assertWALCarriesBytes(t, livePath)

	restoredDays := restoredDayList(t, copyPath, user.ID)
	if !slices.Contains(restoredDays, "2026-04-01") {
		t.Fatalf("the hot single-file copy restored %v — a copy that lost everything says nothing about the WAL-resident commit this test is about", restoredDays)
	}
	if slices.Contains(restoredDays, "2026-04-02") {
		t.Fatalf("the hot single-file copy restored %v, the WAL-resident day included, with the write-ahead log still carrying bytes on both sides of the copy: copying ovumcy.db alone on a running instance no longer loses the commits still in ovumcy.db-wal, so the last bullet of the Backup and Restore Contract in docs/self-hosted.md can be relaxed", restoredDays)
	}
}

// assertNoWALSidecarsBesideTheCopy is what keeps the test above about the copy
// it names. The hazard is copying the main file ALONE; a sidecar carried along
// beside it — by an edit here, or by a future helper — would restore the very
// commit the absence assertion expects to be gone, and the test would then be
// green about a procedure nobody performs.
func assertNoWALSidecarsBesideTheCopy(t *testing.T, copyPath string) {
	t.Helper()

	for _, sidecar := range []string{copyPath + "-wal", copyPath + "-shm"} {
		if _, err := os.Stat(sidecar); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is beside the copy (stat error: %v): this test is about copying the main database file alone", filepath.Base(sidecar), err)
		}
	}
}

// captureWALSet copies the WAL set the way a sequential archiver reads it: the
// main database file first, then the sidecars, with the database live and free
// to move in between. betweenReads, when set, runs in exactly that window.
func captureWALSet(t *testing.T, sourceDir, targetDir string, betweenReads func()) {
	t.Helper()

	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("create the capture directory: %v", err)
	}
	copyFileForBackupTest(t,
		filepath.Join(sourceDir, sqliteVolumeDatabaseFile),
		filepath.Join(targetDir, sqliteVolumeDatabaseFile))

	if betweenReads != nil {
		betweenReads()
	}

	for _, sidecar := range []string{sqliteVolumeDatabaseFile + "-shm", sqliteVolumeDatabaseFile + "-wal"} {
		source := filepath.Join(sourceDir, sidecar)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue // an archiver carries a sidecar only when it is there
		}
		copyFileForBackupTest(t, source, filepath.Join(targetDir, sidecar))
	}
}

// checkpointWALTruncate folds the WAL into the main database file and truncates
// it — the checkpoint SQLite runs on its own once the WAL passes its threshold,
// and on the last connection closing.
func checkpointWALTruncate(t *testing.T, database *gorm.DB) {
	t.Helper()

	var busy, walPages, checkpointed int
	if err := database.Raw("PRAGMA wal_checkpoint(TRUNCATE)").Row().Scan(&busy, &walPages, &checkpointed); err != nil {
		t.Fatalf("checkpoint the WAL: %v", err)
	}
	if busy != 0 {
		t.Fatalf("the checkpoint was blocked (busy=%d), so the window this test needs never opened", busy)
	}
}

// assertWALCarriesBytes proves the commit under test really is WAL-resident when
// the capture starts; without it the whole test could pass on a database that
// had already checkpointed itself.
func assertWALCarriesBytes(t *testing.T, databasePath string) {
	t.Helper()

	info, err := os.Stat(databasePath + "-wal")
	if err != nil {
		t.Fatalf("stat the write-ahead log: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("the write-ahead log is empty, so the commit this test is about already sits in the main database file")
	}
}

// restoredDayList opens a captured database and returns the days it holds for
// one owner, sorted.
func restoredDayList(t *testing.T, databasePath string, userID uint) []string {
	t.Helper()

	restored, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open the restored database: %v", err)
	}
	defer closeBackupTestDatabase(t, restored)

	logs, err := NewRepositories(restored).DailyLogs.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list restored logs: %v", err)
	}

	days := make([]string, 0, len(logs))
	for _, entry := range logs {
		days = append(days, entry.Date.Format("2006-01-02"))
	}
	slices.Sort(days)
	return days
}

func createBackupRaceOwner(t *testing.T, repos *Repositories) *models.User {
	t.Helper()

	user := &models.User{
		Email:            "race-owner@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func createBackupRaceDay(t *testing.T, repos *Repositories, userID uint, day string) {
	t.Helper()

	entry := &models.DailyLog{UserID: userID, Date: backupRestoreDay(t, day), IsPeriod: true, Flow: "medium"}
	if err := repos.DailyLogs.Create(context.Background(), entry); err != nil {
		t.Fatalf("create day log %s: %v", day, err)
	}
}

func closeBackupTestDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("reach the sql.DB handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}
