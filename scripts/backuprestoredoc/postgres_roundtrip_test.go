package backuprestoredoc

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
	"gorm.io/gorm"
)

// TestDocumentedPostgresRestoreReplacesDriftedDataWithTheBackedUpGeneration
// runs the runbook's Postgres backup and restore, as the runbook writes them,
// against an ephemeral database carrying the application's own schema.
//
// The shape is the one the claim needs and no smaller: generation 1 is seeded
// and dumped, generation 2 deliberately drifts it — a row written, a row
// changed, a row deleted — and only then is the documented restore run. Without
// the drift, a restore that moved nothing would pass, which is precisely the
// failure the runbook's own table warns about.
//
// It closes with that failure as a counterfactual: the same replay with the two
// additions the runbook calls load-bearing taken away, asserted to exit 0 while
// changing nothing at all. That is what keeps this from being a test of
// Postgres rather than a test of the procedure.
func TestDocumentedPostgresRestoreReplacesDriftedDataWithTheBackedUpGeneration(t *testing.T) {
	commands := documentedPostgresCommands(t)
	dsn, container := testdb.StartPostgres(t, "ovumcy_runbook_restore")
	config := db.Config{Driver: db.DriverPostgres, PostgresURL: dsn}

	seeded := seedGenerationOne(t, config)

	// The operator's working directory: the runbook's `mkdir -p backups` and
	// its `backups/…sql` paths are relative to wherever the commands are run,
	// and the restore has to find the dump the backup wrote.
	backupDir := t.TempDir()
	backupExport := runDocumentedBackup(t, container, backupDir, commands)

	driftToGenerationTwo(t, config, seeded)
	driftedExport := runDocumentedBackup(t, container, t.TempDir(), commands)
	if exportsMatch(backupExport, driftedExport) {
		t.Fatal("generation 2 left the dump unchanged: the drift is not observable, so a restore that moved nothing would pass this guard")
	}

	restoreOutput := runDocumentedRestore(t, container, backupDir, commands)
	if strings.Contains(restoreOutput, "ERROR:") {
		t.Fatalf("the documented restore printed errors, which the runbook says it does not:\n%s", restoreOutput)
	}

	restoredExport := runDocumentedBackup(t, container, t.TempDir(), commands)
	assertExportMatchesApartFromRestoreResidue(t, backupExport, restoredExport)

	assertGenerationOneReadsBack(t, config, seeded)

	assertRestoreWithoutTheDocumentedAdditionsChangesNothing(t, config, container, backupDir, commands, backupExport, seeded)
}

// assertRestoreWithoutTheDocumentedAdditionsChangesNothing replays the dump the
// way the runbook says NOT to — into a database that still holds its schema,
// with `-v ON_ERROR_STOP=1` removed — and asserts the outcome the runbook's
// table records for that row: a clean exit that moved nothing. Both halves of
// the broken command are derived from the documented one, never spelled here,
// so the counterfactual cannot drift away from the procedure it is the negative
// of.
func assertRestoreWithoutTheDocumentedAdditionsChangesNothing(
	t *testing.T,
	config db.Config,
	container string,
	backupDir string,
	commands runbookCommands,
	backupExport []byte,
	seeded seededGeneration,
) {
	t.Helper()

	driftToGenerationTwo(t, config, seeded)
	driftedExport := runDocumentedBackup(t, container, t.TempDir(), commands)

	replay := strings.Replace(commands.replay, onErrorStopFlag+" ", "", 1)
	if replay == commands.replay {
		t.Fatalf("removing %q from the documented replay changed nothing, so the counterfactual would run the documented command instead:\n%s", onErrorStopFlag, commands.replay)
	}

	output, err := runRunbookScript(t, container, backupDir, replay)
	if err != nil {
		t.Fatalf("the runbook records this case as exiting 0; it failed instead: %v\n%s", err, output)
	}
	if !strings.Contains(output, "ERROR:") {
		t.Fatalf("replaying into a database that still holds its schema printed no errors at all — the runbook's account of why the drop step is needed no longer holds:\n%s", output)
	}

	afterExport := runDocumentedBackup(t, container, t.TempDir(), commands)
	if exportsMatch(afterExport, backupExport) {
		t.Fatal("the dump replayed in full without dropping the schema first: the runbook's stated reason for the drop step is wrong")
	}

	// Not a row moved: `CREATE TABLE` and `COPY` both fail against a schema
	// that still holds its tables, and `COPY` is all-or-nothing per table.
	if !sameLines(copyData(driftedExport), copyData(afterExport)) {
		t.Fatalf("the failed replay moved rows, which the runbook's table says it does not:\n%s", differenceReport(driftedExport, afterExport))
	}

	// The sequences, though, are not rows. `setval` is the one statement in a
	// plain dump that succeeds against a populated schema, so a replay that
	// restored nothing still rewinds every sequence to the backup's value and
	// leaves the next insert colliding with an id already in use. The runbook
	// records this row as "changed nothing"; this is the part that is not
	// nothing, and it is asserted in both directions so that a Postgres release
	// which stops doing it makes the runbook's wording stale here rather than
	// silently.
	if !sameLines(sequenceSettings(afterExport), sequenceSettings(backupExport)) {
		t.Fatalf("the failed replay did not rewind the sequences to the backup's values, which the runbook now warns it does:\n  after the failed replay: %v\n  in the backup:           %v", sequenceSettings(afterExport), sequenceSettings(backupExport))
	}
	if sameLines(sequenceSettings(afterExport), sequenceSettings(driftedExport)) {
		t.Fatalf("the sequences did not move at all, so this case cannot demonstrate the collision the runbook warns about: %v", sequenceSettings(afterExport))
	}

	// And the consequence itself, rather than only its cause: the next write
	// lands on an id that is already taken. One attempt, because Postgres
	// advances the sequence past the collision as it fails.
	withRepositories(t, config, func(repos *db.Repositories) {
		entry := models.DailyLog{UserID: seeded.ownerID, Date: runbookDay(t, "2026-04-01"), Notes: "written after the failed replay"}
		if err := repos.DailyLogs.Create(context.Background(), &entry); err == nil {
			t.Error("the first write after the failed replay succeeded: the rewound sequence no longer collides, and the runbook's warning about it is stale")
		}
	})
}

// runDocumentedBackup runs the documented backup command in dir and returns the
// dump it wrote — the "export" the runbook's claim is about. Every dump this
// guard compares is produced by this one command, so the comparison is like
// with like and no pg_dump invocation is ever spelled by the test itself.
func runDocumentedBackup(t *testing.T, container string, dir string, commands runbookCommands) []byte {
	t.Helper()

	output, err := runRunbookScript(t, container, dir, commands.backup)
	if err != nil {
		t.Fatalf("the documented backup command failed: %v\n%s", err, output)
	}

	export, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(commands.dumpFile)))
	if err != nil {
		t.Fatalf("read the dump the documented backup command wrote to %s: %v", commands.dumpFile, err)
	}
	if !bytes.Contains(export, []byte("COPY public.users")) {
		t.Fatalf("the dump written to %s carries no users table, so it is not a dump of this application's database (%d bytes)", commands.dumpFile, len(export))
	}
	return export
}

// runDocumentedRestore runs the two commands of the documented restore, in the
// documented order, and returns their combined output.
func runDocumentedRestore(t *testing.T, container string, dir string, commands runbookCommands) string {
	t.Helper()

	output, err := runRunbookScript(t, container, dir, commands.drop, commands.replay)
	if err != nil {
		t.Fatalf("the documented restore failed: %v\n%s", err, output)
	}
	return output
}

// runRunbookScript runs Postgres commands taken verbatim from the runbook,
// with the compose transport swapped for a direct `docker exec -i` against
// container. That substitution is the only one this half makes; runScript
// refuses to execute whatever a substitution missed.
func runRunbookScript(t *testing.T, container string, dir string, commands ...string) (string, error) {
	t.Helper()

	script := strings.Join(commands, "\n")
	script = strings.ReplaceAll(script, composeExecPrefix, "docker exec -i "+container)
	return runScript(t, dir, script)
}

// seededGeneration is what generation 1 put into the database, kept so the
// restored generation can be read back through the application's own
// repositories and not only compared as dump lines.
type seededGeneration struct {
	ownerID     uint
	ownerEmail  string
	displayName string
	logDates    []time.Time
}

// seedGenerationOne fills a freshly migrated database with an owner and a few
// representative day logs — the same fixture shape the SQLite backup guard
// uses (internal/db/sqlite_backup_restore_test.go), so the two documented
// procedures are judged on comparable data.
func seedGenerationOne(t *testing.T, config db.Config) seededGeneration {
	t.Helper()

	var seeded seededGeneration
	withRepositories(t, config, func(repos *db.Repositories) {
		seeded = seedGenerationInto(t, repos)
	})
	return seeded
}

// seedGenerationInto is the same fixture against an already open database, for
// the SQLite half, which has to hold the connection open while it snapshots
// the volume: closing checkpoints the WAL away, and the WAL is part of what
// that half is there to prove.
func seedGenerationInto(t *testing.T, repos *db.Repositories) seededGeneration {
	t.Helper()

	seeded := seededGeneration{
		ownerEmail:  "owner@example.com",
		displayName: "generation one",
		logDates: []time.Time{
			runbookDay(t, "2026-02-01"),
			runbookDay(t, "2026-02-02"),
			runbookDay(t, "2026-02-15"),
		},
	}
	{
		user := &models.User{
			DisplayName:      seeded.displayName,
			Email:            seeded.ownerEmail,
			PasswordHash:     "hash",
			RecoveryCodeHash: "recovery",
			Role:             models.RoleOwner,
			CycleLength:      models.DefaultCycleLength,
			PeriodLength:     models.DefaultPeriodLength,
			AutoPeriodFill:   true,
			CreatedAt:        time.Now().UTC(),
		}
		if err := repos.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("seed the owner: %v", err)
		}
		seeded.ownerID = user.ID

		bbt := 36.5
		logs := []models.DailyLog{
			{
				UserID: user.ID, Date: seeded.logDates[0],
				IsPeriod: true, CycleStart: true, Flow: "heavy", Mood: 3,
				Notes:           "first day, cramps",
				SymptomIDs:      []uint{1, 4, 7},
				CycleFactorKeys: []string{"stress", "travel"},
			},
			{
				UserID: user.ID, Date: seeded.logDates[1],
				IsPeriod: true, Flow: "light", BBT: &bbt,
			},
			{
				UserID: user.ID, Date: seeded.logDates[2],
				SexActivity: "protected", CervicalMucus: "eggwhite",
			},
		}
		for i := range logs {
			if err := repos.DailyLogs.Create(context.Background(), &logs[i]); err != nil {
				t.Fatalf("seed day log %d: %v", i, err)
			}
		}
	}

	return seeded
}

// driftToGenerationTwo moves the database away from the backed-up generation in
// all three directions a restore has to undo: a row written, a row changed, a
// row deleted. Anything less and a restore that did nothing would still leave
// the database looking right.
func driftToGenerationTwo(t *testing.T, config db.Config, seeded seededGeneration) {
	t.Helper()

	withRepositories(t, config, func(repos *db.Repositories) {
		driftToGenerationTwoInto(t, repos, seeded)
	})
}

// driftToGenerationTwoInto is the same drift against an already open database.
func driftToGenerationTwoInto(t *testing.T, repos *db.Repositories, seeded seededGeneration) {
	t.Helper()
	{
		// Written: an account that must not survive the restore.
		stranger := &models.User{
			DisplayName:      "generation two",
			Email:            "drifted@example.com",
			PasswordHash:     "hash",
			RecoveryCodeHash: "recovery",
			Role:             models.RoleOwner,
			CycleLength:      models.DefaultCycleLength,
			PeriodLength:     models.DefaultPeriodLength,
			CreatedAt:        time.Now().UTC(),
		}
		if err := repos.Users.Create(context.Background(), stranger); err != nil {
			t.Fatalf("drift: create the second account: %v", err)
		}

		// Changed: a column of the account that does survive it.
		if err := repos.Users.UpdateDisplayName(context.Background(), seeded.ownerID, "drifted display name"); err != nil {
			t.Fatalf("drift: rename the owner: %v", err)
		}

		// Deleted: health data the restore has to bring back.
		firstDay := seeded.logDates[0]
		if err := repos.DailyLogs.DeleteByUserAndDayRange(context.Background(), seeded.ownerID, firstDay, firstDay.Add(24*time.Hour)); err != nil {
			t.Fatalf("drift: delete a day log: %v", err)
		}

		// Written again, on the health-data side.
		extra := models.DailyLog{UserID: seeded.ownerID, Date: runbookDay(t, "2026-03-09"), Notes: "logged after the dump"}
		if err := repos.DailyLogs.Create(context.Background(), &extra); err != nil {
			t.Fatalf("drift: create a day log: %v", err)
		}
	}
}

// assertGenerationOneReadsBack reads the restored database back through the
// application's own repositories. The dump comparison already proves the export
// matches; this proves the three drifted directions were each undone, and says
// which one was not in a form a human can act on — the runbook's own
// Post-Restore Verification step 5, run as code.
func assertGenerationOneReadsBack(t *testing.T, config db.Config, seeded seededGeneration) {
	t.Helper()

	withRepositories(t, config, func(repos *db.Repositories) {
		assertGenerationOneReadsBackFrom(t, repos, seeded)
	})
}

// assertGenerationOneReadsBackFrom is the same read-back against an already
// open database.
func assertGenerationOneReadsBackFrom(t *testing.T, repos *db.Repositories, seeded seededGeneration) {
	t.Helper()
	{
		users, err := repos.Users.CountUsers(context.Background())
		if err != nil {
			t.Fatalf("count the restored accounts: %v", err)
		}
		if users != 1 {
			t.Errorf("restored database holds %d accounts, want 1: the account written after the dump survived the restore", users)
		}

		owner, err := repos.Users.FindByID(context.Background(), seeded.ownerID)
		if err != nil {
			t.Fatalf("find the restored owner: %v", err)
		}
		if owner.Email != seeded.ownerEmail {
			t.Errorf("restored owner email is %q, want %q", owner.Email, seeded.ownerEmail)
		}
		if owner.DisplayName != seeded.displayName {
			t.Errorf("restored owner display name is %q, want %q: the change made after the dump survived the restore", owner.DisplayName, seeded.displayName)
		}

		logs, err := repos.DailyLogs.ListByUser(context.Background(), seeded.ownerID)
		if err != nil {
			t.Fatalf("list the restored day logs: %v", err)
		}
		restored := make(map[string]bool, len(logs))
		for _, log := range logs {
			restored[log.Date.Format("2006-01-02")] = true
		}
		for _, day := range seeded.logDates {
			if !restored[day.Format("2006-01-02")] {
				t.Errorf("restored database is missing the day log for %s", day.Format("2006-01-02"))
			}
		}
		if len(logs) != len(seeded.logDates) {
			t.Errorf("restored database holds %d day logs, want %d: a log written after the dump survived the restore", len(logs), len(seeded.logDates))
		}
	}
}

// withRepositories opens the application's own database layer against config,
// hands the repositories to fn, and closes the pool before returning. Closing
// is not tidiness: the runbook restores "with the app stopped", and an open
// pool is the app.
func withRepositories(t *testing.T, config db.Config, fn func(repos *db.Repositories)) {
	t.Helper()

	database, err := db.OpenDatabase(config)
	if err != nil {
		t.Fatalf("open the postgres database: %v", err)
	}
	defer closeDatabase(t, database)

	fn(db.NewRepositories(database))
}

func closeDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("reach the sql database handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close the sql database handle: %v", err)
	}
}

func runbookDay(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", value, err)
	}
	return parsed
}
