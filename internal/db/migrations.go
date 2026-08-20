package db

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	embeddedmigrations "github.com/ovumcy/ovumcy-web/migrations"
	"gorm.io/gorm"
)

// sqlLineCommentPrefix is the only comment form the migration set uses. Block
// comments (/* ... */) appear in no migration file and are deliberately not
// recognized: an unrecognized prefix simply leaves the statement unmatched, and
// an unmatched statement is executed exactly as before.
const sqlLineCommentPrefix = "--"

var migrationFilePattern = regexp.MustCompile(`^(\d+)_.*\.sql$`)
var addColumnStatementPattern = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+([^\s]+)\s+ADD\s+COLUMN\s+([^\s]+)\b`)
var dropTableStatementPattern = regexp.MustCompile(`(?i)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^\s;]+)`)

type embeddedMigration struct {
	Version string
	Order   int
	Name    string
	SQL     string
}

func applyEmbeddedMigrations(database *gorm.DB, driver Driver) error {
	if err := ensureSchemaMigrationsTable(database); err != nil {
		return err
	}

	migrations, err := loadEmbeddedMigrations(driver)
	if err != nil {
		return err
	}

	appliedVersions, err := loadAppliedMigrationVersions(database)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		if _, alreadyApplied := appliedVersions[migration.Version]; alreadyApplied {
			continue
		}

		if err := refuseTableDropReplayOnANewerSchema(migration, appliedVersions); err != nil {
			return err
		}

		if err := applyMigration(database, migration); err != nil {
			return err
		}
	}

	return nil
}

func ensureSchemaMigrationsTable(database *gorm.DB) error {
	const createTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if err := database.Exec(createTableSQL).Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func loadEmbeddedMigrations(driver Driver) ([]embeddedMigration, error) {
	migrationDir := migrationDirForDriver(driver)

	entries, err := fs.ReadDir(embeddedmigrations.Files, migrationDir)
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]embeddedMigration, 0, len(entries))
	seenVersions := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := strings.TrimSpace(entry.Name())
		matches := migrationFilePattern.FindStringSubmatch(fileName)
		if len(matches) != 2 {
			continue
		}

		version := matches[1]
		order, err := strconv.Atoi(version)
		if err != nil {
			return nil, fmt.Errorf("parse migration version from %s: %w", fileName, err)
		}

		if existing, exists := seenVersions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %s in %s and %s", version, existing, fileName)
		}
		seenVersions[version] = fileName

		rawSQL, err := fs.ReadFile(embeddedmigrations.Files, path.Join(migrationDir, fileName))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", fileName, err)
		}

		migrations = append(migrations, embeddedMigration{
			Version: version,
			Order:   order,
			Name:    fileName,
			SQL:     string(rawSQL),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].Order == migrations[j].Order {
			return migrations[i].Name < migrations[j].Name
		}
		return migrations[i].Order < migrations[j].Order
	})

	return migrations, nil
}

func migrationDirForDriver(driver Driver) string {
	switch driver {
	case DriverPostgres:
		return "postgres"
	default:
		return "."
	}
}

type appliedMigrationVersion struct {
	Version string `gorm:"column:version"`
}

func loadAppliedMigrationVersions(database *gorm.DB) (map[string]struct{}, error) {
	rows := make([]appliedMigrationVersion, 0)
	if err := database.Raw(`SELECT version FROM schema_migrations`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load applied migration versions: %w", err)
	}

	versions := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		versions[row.Version] = struct{}{}
	}
	return versions, nil
}

func applyMigration(database *gorm.DB, migration embeddedMigration) error {
	return database.Transaction(func(tx *gorm.DB) error {
		statements := splitSQLStatements(migration.SQL)
		if len(statements) == 0 {
			return errors.New("migration has no SQL statements")
		}

		columnsBeforeTheDrop, err := snapshotColumnsOfDroppedTables(tx, statements)
		if err != nil {
			return fmt.Errorf("inspect migration %s: %w", migration.Name, err)
		}

		for _, statement := range statements {
			skip, err := shouldSkipStatement(tx, statement)
			if err != nil {
				return fmt.Errorf("inspect migration %s: %w", migration.Name, err)
			}
			if skip {
				continue
			}

			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("execute migration %s statement %q: %w", migration.Name, statement, err)
			}
		}

		if err := requireDroppedTablesKeptTheirColumns(tx, migration, columnsBeforeTheDrop); err != nil {
			return err
		}

		if err := tx.Exec(
			`INSERT INTO schema_migrations(version, name) VALUES (?, ?)`,
			migration.Version,
			migration.Name,
		).Error; err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}

		return nil
	})
}

// droppedTableColumns is one table a migration is about to DROP, together with
// the columns it had before the migration ran.
type droppedTableColumns struct {
	Table   string
	Columns []string
}

// refuseTableDropReplayOnANewerSchema stops a migration that DROPs a table from
// running against a database that already records a LATER migration as applied.
//
// The runner re-applies any migration whose schema_migrations row is missing —
// a restore from a backup taken before the row was written, or a pruned ledger
// — and for `ALTER TABLE ... ADD COLUMN` that is safe, because
// shouldSkipStatement drops the statement when the column is already there. A
// migration that reconciles a table by rebuilding it (create a replacement,
// copy, DROP the original, rename) has no such skip: its replacement table is
// the table as it looked AT THAT VERSION, so a replay on a schema that later
// migrations have widened copies the columns that version knew about and drops
// every column added after it, with the rows' values in them. On this schema
// migration 003 rebuilds daily_logs from the nine columns of migration 001 and
// would discard the eight health columns migrations 005-023 added.
//
// A recorded later version is proof the database moved past this migration:
// there is nothing left to apply, and the only thing a replay can do is
// destroy. Refusing runs no statement at all, so the database is left exactly
// as it was and the operator can restore the missing ledger row or the backup
// and boot again.
//
// It is dialect-neutral by construction — it reads the SQL of whichever tree
// the driver loaded — and therefore dormant on Postgres, whose 003 is a
// `SELECT 1;` because its bootstrap schema never needed the rebuild.
func refuseTableDropReplayOnANewerSchema(migration embeddedMigration, appliedVersions map[string]struct{}) error {
	droppedTables := tablesDroppedByMigration(migration)
	if len(droppedTables) == 0 {
		return nil
	}

	laterVersion, hasLaterVersion := highestAppliedVersionAfter(appliedVersions, migration.Order)
	if !hasLaterVersion {
		return nil
	}

	return fmt.Errorf(
		"refusing to re-apply migration %s: it drops table %s, and migration %s is already recorded as applied, so the database schema is newer than the one this migration rebuilds — re-applying it would discard the columns and values added after it. Nothing was executed and the database is unchanged; if migration %s is in fact applied, restore its schema_migrations row, otherwise restore from a backup",
		migration.Name,
		strings.Join(droppedTables, ", "),
		laterVersion,
		migration.Version,
	)
}

// highestAppliedVersionAfter returns the highest recorded migration version
// numbered above order. A ledger row whose version is not a number is ignored
// rather than treated as newer: it cannot be ordered against the file set.
func highestAppliedVersionAfter(appliedVersions map[string]struct{}, order int) (string, bool) {
	highestVersion := ""
	highestOrder := order
	for version := range appliedVersions {
		versionOrder, err := strconv.Atoi(strings.TrimSpace(version))
		if err != nil {
			continue
		}
		if versionOrder > highestOrder {
			highestOrder = versionOrder
			highestVersion = version
		}
	}
	return highestVersion, highestVersion != ""
}

// snapshotColumnsOfDroppedTables records the columns of every table the
// migration's statements DROP, read before any of them runs.
//
// It is the second half of the destructive-replay guard and covers the case the
// version comparison cannot see: a database that lost its schema_migrations
// content entirely records no later version, so the whole set replays from 001
// with nothing to compare against. The post-condition below then measures the
// effect itself.
func snapshotColumnsOfDroppedTables(database *gorm.DB, statements []string) ([]droppedTableColumns, error) {
	snapshot := make([]droppedTableColumns, 0)
	seenTables := make(map[string]struct{})

	for _, statement := range statements {
		tableName, isDropTable := parseDropTableStatement(statement)
		if !isDropTable {
			continue
		}
		if _, alreadySeen := seenTables[tableName]; alreadySeen {
			continue
		}
		seenTables[tableName] = struct{}{}

		if !database.Migrator().HasTable(tableName) {
			continue
		}
		columns, err := tableColumnNames(database, tableName)
		if err != nil {
			return nil, err
		}
		snapshot = append(snapshot, droppedTableColumns{Table: tableName, Columns: columns})
	}

	return snapshot, nil
}

// requireDroppedTablesKeptTheirColumns fails the migration — and, being inside
// its transaction, rolls back every statement it ran — when a table it dropped
// came back without a column it had before.
//
// A migration that means to REMOVE a column says so with an explicit
// `ALTER TABLE ... DROP COLUMN`, which this check does not look at; what it
// refuses is a column removed as a side effect of rebuilding a table from a
// narrower replacement, which is never what the migration author wrote and is
// always the shape of a replay against a newer schema.
func requireDroppedTablesKeptTheirColumns(database *gorm.DB, migration embeddedMigration, columnsBeforeTheDrop []droppedTableColumns) error {
	for _, before := range columnsBeforeTheDrop {
		if !database.Migrator().HasTable(before.Table) {
			return fmt.Errorf(
				"refusing migration %s: it dropped table %s and did not put it back. Nothing was written — the migration was rolled back and the database is unchanged",
				migration.Name,
				before.Table,
			)
		}

		columnsNow, err := tableColumnNames(database, before.Table)
		if err != nil {
			return fmt.Errorf("inspect migration %s: %w", migration.Name, err)
		}

		missing := missingColumnNames(before.Columns, columnsNow)
		if len(missing) == 0 {
			continue
		}

		return fmt.Errorf(
			"refusing migration %s: rebuilding table %s would have dropped column(s) %s, which held data before it ran — the migration rebuilds the table from the columns its own version knew about, so re-applying it on a newer schema discards everything added after it. Nothing was written — the migration was rolled back and the database is unchanged; if migration %s is in fact applied, restore its schema_migrations row, otherwise restore from a backup",
			migration.Name,
			before.Table,
			strings.Join(missing, ", "),
			migration.Version,
		)
	}

	return nil
}

// tablesDroppedByMigration returns, in file order and without repeats, the
// tables a migration removes with DROP TABLE.
func tablesDroppedByMigration(migration embeddedMigration) []string {
	dropped := make([]string, 0)
	seenTables := make(map[string]struct{})
	for _, statement := range splitSQLStatements(migration.SQL) {
		tableName, isDropTable := parseDropTableStatement(statement)
		if !isDropTable {
			continue
		}
		if _, alreadySeen := seenTables[tableName]; alreadySeen {
			continue
		}
		seenTables[tableName] = struct{}{}
		dropped = append(dropped, tableName)
	}
	return dropped
}

// parseDropTableStatement reports whether a statement chunk is a
// `DROP TABLE ...`, returning the table it removes. Like the ADD COLUMN
// detection it reads past the prose header splitSQLStatements leaves attached
// to the first statement of a chunk, and for the same reason: the regex is
// anchored at ^, so a comment line above the statement would hide it.
func parseDropTableStatement(statement string) (tableName string, isDropTable bool) {
	matches := dropTableStatementPattern.FindStringSubmatch(stripLeadingSQLComments(statement))
	if len(matches) != 2 {
		return "", false
	}
	return normalizeSQLIdentifier(matches[1]), true
}

// tableColumnNames reads one table's column names, lowercased, through the same
// migrator the runner already uses for the ADD COLUMN skip, so one reader
// serves both engines.
func tableColumnNames(database *gorm.DB, tableName string) ([]string, error) {
	columnTypes, err := database.Migrator().ColumnTypes(tableName)
	if err != nil {
		return nil, fmt.Errorf("read columns of table %s: %w", tableName, err)
	}

	names := make([]string, 0, len(columnTypes))
	for _, columnType := range columnTypes {
		names = append(names, strings.ToLower(strings.TrimSpace(columnType.Name())))
	}
	return names, nil
}

// missingColumnNames returns the entries of before that are absent from after,
// in their original order.
func missingColumnNames(before []string, after []string) []string {
	present := make(map[string]struct{}, len(after))
	for _, name := range after {
		present[name] = struct{}{}
	}

	missing := make([]string, 0)
	for _, name := range before {
		if _, exists := present[name]; !exists {
			missing = append(missing, name)
		}
	}
	return missing
}

func splitSQLStatements(sqlText string) []string {
	rawParts := strings.Split(sqlText, ";")
	statements := make([]string, 0, len(rawParts))
	for _, rawPart := range rawParts {
		statement := strings.TrimSpace(rawPart)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	return statements
}

func shouldSkipStatement(database *gorm.DB, statement string) (bool, error) {
	tableName, columnName, isAddColumn := parseAddColumnStatement(statement)
	if !isAddColumn {
		return false, nil
	}

	exists, err := tableColumnExists(database, tableName, columnName)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// parseAddColumnStatement reports whether a statement chunk is an
// `ALTER TABLE ... ADD COLUMN ...`, returning the table and column it targets.
// It is the single detection used by the already-exists skip that gives SQLite
// (which has no ADD COLUMN IF NOT EXISTS) its migration idempotency, so it has
// to see past the prose header that splitSQLStatements leaves attached to the
// first statement of a file.
func parseAddColumnStatement(statement string) (tableName string, columnName string, isAddColumn bool) {
	matches := addColumnStatementPattern.FindStringSubmatch(stripLeadingSQLComments(statement))
	if len(matches) != 3 {
		return "", "", false
	}
	return normalizeSQLIdentifier(matches[1]), normalizeSQLIdentifier(matches[2]), true
}

// stripLeadingSQLComments drops the leading blank and `--` comment lines of a
// statement chunk and returns the rest verbatim.
//
// splitSQLStatements splits on `;` without stripping comments, so the first
// chunk of a migration that opens with a prose header is the header plus the
// statement. The detection regex is anchored at ^, so that chunk never matched
// and the skip silently did not apply to the file's first ADD COLUMN.
//
// Only whole leading lines are removed. Comment text can therefore never be
// parsed as part of a statement: a header line that mentions ALTER or ADD
// COLUMN is discarded with its line rather than matched, and a trailing comment
// on a code line is left where it is for the executor. Nothing after the first
// line of SQL is touched.
func stripLeadingSQLComments(statement string) string {
	remainder := statement
	for {
		remainder = strings.TrimLeftFunc(remainder, unicode.IsSpace)
		if !strings.HasPrefix(remainder, sqlLineCommentPrefix) {
			return remainder
		}
		lineEnd := strings.IndexByte(remainder, '\n')
		if lineEnd < 0 {
			return ""
		}
		remainder = remainder[lineEnd+1:]
	}
}

func tableColumnExists(database *gorm.DB, tableName string, columnName string) (bool, error) {
	return database.Migrator().HasColumn(tableName, columnName), nil
}

func normalizeSQLIdentifier(identifier string) string {
	normalized := strings.TrimSpace(identifier)
	normalized = strings.Trim(normalized, "\"`[]")
	return strings.TrimSpace(normalized)
}
