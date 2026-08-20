package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The destructive-replay guard decides whether a migration may run by READING
// the schema: it lists the tables, reads the columns of each one before the
// first statement, and reads them again after the last. Every one of those
// reads can fail — a connection lost mid-migration, a catalog read the driver
// refuses — and what the runner does then is the subject of this file.
//
// The answer has to be "refuse", never "continue with what was read": a
// snapshot that came back empty because the read failed would compare equal to
// any schema at all, and the guard would wave through exactly the rebuild it
// exists to stop. The cases below therefore assert two things together — the
// error names the migration and the read that failed, and NOTHING was written.
//
// The failure is injected at the connection pool rather than by breaking the
// database file, because the branch has to be entered on a schema that is
// otherwise healthy: a corrupt file would fail the statements too, and the case
// would no longer be about the inspection at all. The pool is a thin wrapper
// around a real SQLite connection, so every read that is not the injected one
// is the real thing.

// sqliteTableListQuery and sqliteTableColumnsQuery are the catalog reads the
// gorm SQLite migrator issues for GetTables and ColumnTypes respectively. They
// are matched as substrings of the SQL that reaches the pool, so a case names
// the read it takes out instead of failing the connection wholesale.
const (
	sqliteTableListQuery    = "SELECT name FROM sqlite_master"
	sqliteTableColumnsQuery = "SELECT sql FROM sqlite_master"
)

// errInjectedCatalogReadFailure is what the injected read returns. Each case
// asserts it is still in the chain the runner returns, so a refusal cannot be
// mistaken for one of the guard's own verdicts.
var errInjectedCatalogReadFailure = errors.New("injected catalog read failure")

// catalogReadFault is one injected failure: which read fails, and — for the
// post-condition case — from which moment on.
//
// armOnExec is what makes the "after the statements ran" case expressible. The
// snapshot and the post-condition issue the SAME query, so a fault armed from
// the start would fail the snapshot and the migration would never reach its
// statements. Arming on a statement of the migration's own SQL puts the failure
// exactly between the two reads.
type catalogReadFault struct {
	mu        sync.Mutex
	failQuery string
	armOnExec string
	armed     bool
}

func newCatalogReadFault(failQuery string) *catalogReadFault {
	return &catalogReadFault{failQuery: failQuery, armed: true}
}

func newCatalogReadFaultArmedByStatement(failQuery string, armOnExec string) *catalogReadFault {
	return &catalogReadFault{failQuery: failQuery, armOnExec: armOnExec}
}

func (fault *catalogReadFault) observeExec(query string) {
	if fault.armOnExec == "" {
		return
	}
	fault.mu.Lock()
	defer fault.mu.Unlock()
	if strings.Contains(query, fault.armOnExec) {
		fault.armed = true
	}
}

func (fault *catalogReadFault) shouldFail(query string) bool {
	fault.mu.Lock()
	defer fault.mu.Unlock()
	return fault.armed && strings.Contains(query, fault.failQuery)
}

// catalogFaultPool is a gorm.ConnPool that delegates everything to a real
// *sql.DB and fails only the injected read. It implements gorm.ConnPoolBeginner
// rather than gorm.TxBeginner on purpose: the latter would hand gorm the raw
// *sql.Tx and every read inside applyMigration's transaction would bypass the
// injection, which is the only place the guard runs.
type catalogFaultPool struct {
	inner *sql.DB
	fault *catalogReadFault
}

func (pool *catalogFaultPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return pool.inner.PrepareContext(ctx, query)
}

func (pool *catalogFaultPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	pool.fault.observeExec(query)
	return pool.inner.ExecContext(ctx, query, args...)
}

func (pool *catalogFaultPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if pool.fault.shouldFail(query) {
		return nil, errInjectedCatalogReadFailure
	}
	return pool.inner.QueryContext(ctx, query, args...)
}

func (pool *catalogFaultPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return pool.inner.QueryRowContext(ctx, query, args...)
}

func (pool *catalogFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	transaction, err := pool.inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &catalogFaultTx{inner: transaction, fault: pool.fault}, nil
}

// catalogFaultTx carries the same injection into the migration's transaction
// and commits or rolls it back for real, so a case can assert that a refused
// migration left nothing behind.
type catalogFaultTx struct {
	inner *sql.Tx
	fault *catalogReadFault
}

func (transaction *catalogFaultTx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return transaction.inner.PrepareContext(ctx, query)
}

func (transaction *catalogFaultTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	transaction.fault.observeExec(query)
	return transaction.inner.ExecContext(ctx, query, args...)
}

func (transaction *catalogFaultTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if transaction.fault.shouldFail(query) {
		return nil, errInjectedCatalogReadFailure
	}
	return transaction.inner.QueryContext(ctx, query, args...)
}

func (transaction *catalogFaultTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return transaction.inner.QueryRowContext(ctx, query, args...)
}

func (transaction *catalogFaultTx) Commit() error {
	return transaction.inner.Commit()
}

func (transaction *catalogFaultTx) Rollback() error {
	return transaction.inner.Rollback()
}

// openSQLiteWithACatalogReadFault opens an existing database file through the
// fault-injecting pool. It bypasses OpenDatabase, so opening can never itself
// run a migration.
func openSQLiteWithACatalogReadFault(t *testing.T, databasePath string, fault *catalogReadFault) *gorm.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", databasePath)
	requireNoErr(t, err, "open sqlite for the fault-injecting pool")
	t.Cleanup(func() { requireNoErr(t, sqlDB.Close(), "close the fault-injecting pool") })

	database, err := gorm.Open(
		&sqlite.Dialector{Conn: &catalogFaultPool{inner: sqlDB, fault: fault}},
		&gorm.Config{Logger: logger.Discard},
	)
	requireNoErr(t, err, "open gorm over the fault-injecting pool")
	return database
}

// widenDailyLogsFixture is the migration every case here runs: one plain ADD
// COLUMN, which is the safest statement in the tree. If the runner applied it
// despite the failed inspection, the column would be there — so its absence is
// what proves the refusal came before any write.
const (
	widenDailyLogsFixtureName   = "900_widen_daily_logs.sql"
	widenDailyLogsFixtureColumn = "inspection_probe"
	widenDailyLogsFixtureSQL    = "ALTER TABLE daily_logs ADD COLUMN inspection_probe TEXT;"
)

// TestAMigrationIsRefusedWhenTheTableListCannotBeRead covers the first read the
// guard makes. GetTables is what decides which tables the post-condition will
// compare; a failure there leaves the guard with an empty snapshot, which would
// compare equal to every possible outcome.
func TestAMigrationIsRefusedWhenTheTableListCannotBeRead(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "table-list-read.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

	fault := newCatalogReadFault(sqliteTableListQuery)
	database := openSQLiteWithACatalogReadFault(t, databasePath, fault)

	err := applyMigration(database, embeddedMigration{
		Version: "900",
		Order:   900,
		Name:    widenDailyLogsFixtureName,
		SQL:     widenDailyLogsFixtureSQL,
	})

	requireInspectionRefusal(t, err, "list tables")
	assertFixtureColumnAbsent(t, databasePath)
	assertSentinelDayIntact(t, databasePath)
}

// TestAMigrationIsRefusedWhenATableColumnListCannotBeRead covers the read
// underneath it: the table list came back, and reading one table's columns
// failed. The refusal must name the table, because that is the only thing that
// tells an operator which read to look at.
func TestAMigrationIsRefusedWhenATableColumnListCannotBeRead(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "column-list-read.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

	fault := newCatalogReadFault(sqliteTableColumnsQuery)
	database := openSQLiteWithACatalogReadFault(t, databasePath, fault)

	err := applyMigration(database, embeddedMigration{
		Version: "900",
		Order:   900,
		Name:    widenDailyLogsFixtureName,
		SQL:     widenDailyLogsFixtureSQL,
	})

	requireInspectionRefusal(t, err, "read columns of table ")
	assertFixtureColumnAbsent(t, databasePath)
	assertSentinelDayIntact(t, databasePath)
}

// TestAMigrationIsRolledBackWhenTheColumnsCannotBeReReadAfterItRan covers the
// second half of the same read pair, and it is the one with something to lose:
// here the statements have already executed inside the transaction, so
// "refuse" has to mean "roll back" and not merely "report".
//
// The fixture creates a table whose name arms the injection, which is what puts
// the failure between the snapshot and the post-condition. Both the created
// table and the ADD COLUMN must be gone afterwards.
func TestAMigrationIsRolledBackWhenTheColumnsCannotBeReReadAfterItRan(t *testing.T) {
	const armingTable = "inspection_probe_arming_table"

	databasePath := filepath.Join(t.TempDir(), "post-condition-read.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

	fault := newCatalogReadFaultArmedByStatement(sqliteTableColumnsQuery, armingTable)
	database := openSQLiteWithACatalogReadFault(t, databasePath, fault)

	err := applyMigration(database, embeddedMigration{
		Version: "900",
		Order:   900,
		Name:    widenDailyLogsFixtureName,
		SQL:     "CREATE TABLE " + armingTable + " (id INTEGER);\n" + widenDailyLogsFixtureSQL,
	})

	requireInspectionRefusal(t, err, "read columns of table ")
	assertFixtureColumnAbsent(t, databasePath)
	assertTableAbsentAfterRollback(t, databasePath, armingTable)
	assertSentinelDayIntact(t, databasePath)
}

// requireInspectionRefusal holds every case above to the same contract: the
// runner returned an error, it names the migration and the read that failed,
// and the injected cause is still in the chain.
func requireInspectionRefusal(t *testing.T, err error, readDescription string) {
	t.Helper()

	if err == nil {
		t.Fatalf("a migration whose schema inspection failed must be refused, got no error")
	}
	if !errors.Is(err, errInjectedCatalogReadFailure) {
		t.Fatalf("the refusal must carry the underlying read failure; got %v", err)
	}
	if !strings.Contains(err.Error(), "inspect migration "+widenDailyLogsFixtureName) {
		t.Errorf("the refusal must name the migration it stopped; got %v", err)
	}
	if !strings.Contains(err.Error(), readDescription) {
		t.Errorf("the refusal must name the read that failed (%q); got %v", readDescription, err)
	}
}

// assertFixtureColumnAbsent reads the schema back through a connection that
// applies no migration, so the reader can never repair what it measures.
func assertFixtureColumnAbsent(t *testing.T, databasePath string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	columns, err := tableColumnNames(reader, "daily_logs")
	requireNoErr(t, err, "read daily_logs columns back")
	for _, column := range columns {
		if column == widenDailyLogsFixtureColumn {
			t.Fatalf("the refused migration must not have written anything, but daily_logs.%s is there", widenDailyLogsFixtureColumn)
		}
	}
}

func assertTableAbsentAfterRollback(t *testing.T, databasePath string, tableName string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	if reader.Migrator().HasTable(tableName) {
		t.Fatalf("the refused migration must have been rolled back, but table %s is still there", tableName)
	}
}

// TestALedgerVersionThatCannotBeOrderedIsNotReadAsANewerSchema pins how the
// version half reads a schema_migrations row it cannot place.
//
// The refusal fires when the ledger records a migration numbered ABOVE the one
// about to replay, so the comparison has to be numeric. A row whose version is
// not a number — an operator's hand-written marker, a row from a tool that
// stamped a date — cannot be ordered against the file set at all, and the two
// wrong answers are symmetric: read as newer (a string comparison puts
// "baseline" above "003") it refuses a migration that must run and blocks a
// legitimate boot; read as a reason to stop looking it would hide the numeric
// row beside it. It is ignored, and only ignored.
func TestALedgerVersionThatCannotBeOrderedIsNotReadAsANewerSchema(t *testing.T) {
	migration := embeddedMigration{
		Version: "003",
		Order:   3,
		Name:    "003_rebuild_daily_logs.sql",
		SQL:     "DROP TABLE daily_logs_old;",
	}

	t.Run("a version that is not a number is not newer than anything", func(t *testing.T) {
		err := refuseTableDropReplayOnANewerSchema(migration, ledgerVersions("001", "002", "baseline", "2024-05-01-init"))
		if err != nil {
			t.Fatalf("a ledger holding no numerically later version must let the migration run; got %v", err)
		}
	})

	t.Run("and it does not hide the numeric version beside it", func(t *testing.T) {
		err := refuseTableDropReplayOnANewerSchema(migration, ledgerVersions("baseline", "024", "zzz"))
		if err == nil {
			t.Fatalf("a ledger recording migration 024 must refuse the replay of 003")
		}
		if !strings.Contains(err.Error(), "migration 024 is already recorded") {
			t.Errorf("the refusal must name the numeric version it found; got %v", err)
		}
		for _, unorderable := range []string{"baseline", "zzz"} {
			if strings.Contains(err.Error(), unorderable) {
				t.Errorf("the refusal must not name the unorderable row %q; got %v", unorderable, err)
			}
		}
	})
}

// TestTheReplayRefusalNamesEachDroppedTableOnceInFileOrder pins the list the
// operator is handed. The refusal is the whole user interface of this guard —
// it is read once, under a failed boot — so the tables it names are in the
// order the migration drops them and each appears once, however many DROP
// statements mention it.
func TestTheReplayRefusalNamesEachDroppedTableOnceInFileOrder(t *testing.T) {
	migration := embeddedMigration{
		Version: "003",
		Order:   3,
		Name:    "003_rebuild_daily_logs.sql",
		SQL: `DROP TABLE IF EXISTS daily_logs_old;
CREATE TABLE daily_logs_old (id INTEGER);
DROP TABLE IF EXISTS symptom_types_old;
DROP TABLE IF EXISTS daily_logs_old;`,
	}

	err := refuseTableDropReplayOnANewerSchema(migration, ledgerVersions("024"))
	if err == nil {
		t.Fatalf("a ledger recording migration 024 must refuse the replay of 003")
	}
	if !strings.Contains(err.Error(), "it drops table daily_logs_old, symptom_types_old, and migration 024") {
		t.Errorf("the refusal must list each dropped table once, in file order; got %v", err)
	}
}

func ledgerVersions(versions ...string) map[string]struct{} {
	applied := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		applied[version] = struct{}{}
	}
	return applied
}
