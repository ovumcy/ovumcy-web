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

// removedTableMarker is how a migration declares that a table it drops is meant
// to stay gone, rather than being the middle of a table rebuild. It is quoted
// into the refusal below, so an operator who hits the check reads the exact
// line the migration is missing.
const removedTableMarker = "ovumcy:removes-table"

var migrationFilePattern = regexp.MustCompile(`^(\d+)_.*\.sql$`)
var addColumnStatementPattern = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+([^\s]+)\s+ADD\s+COLUMN\s+([^\s]+)\b`)
var dropTableStatementPattern = regexp.MustCompile(`(?i)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^\s;]+)`)
var dropColumnStatementPattern = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+([^\s]+)\s+DROP\s+(?:COLUMN\s+)?(?:IF\s+EXISTS\s+)?([^\s;]+)`)
var renameColumnStatementPattern = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+([^\s]+)\s+RENAME\s+(?:COLUMN\s+)?([^\s;]+)\s+TO\s+([^\s;]+)`)
var removedTableMarkerPattern = regexp.MustCompile(`(?im)^[ \t]*--[ \t]*` + regexp.QuoteMeta(removedTableMarker) + `[ \t]+([^\s;]+)[ \t]*$`)
var createUniqueIndexStatementPattern = regexp.MustCompile(`(?i)^CREATE\s+UNIQUE\s+INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)\s+ON\s+([^\s(]+)\s*\(`)

// duplicateGroupReportLimit caps how many conflicting groups a refusal spells
// out. An operator needs enough to see the shape of the problem and to start
// fixing it, not a dump of the table.
const duplicateGroupReportLimit = 5

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

		columnsBefore, err := snapshotEveryTableColumns(tx)
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

			if err := refuseUniqueIndexOverExistingDuplicates(tx, migration, statement); err != nil {
				return err
			}

			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("execute migration %s statement %q: %w", migration.Name, statement, err)
			}
		}

		if err := requireNoTableSilentlyNarrowed(tx, migration, columnsBefore, statements); err != nil {
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

// tableColumnsBefore is one table as it stood before a migration ran.
type tableColumnsBefore struct {
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

// uniqueIndexIntent is a `CREATE UNIQUE INDEX` statement read back as what it
// asks the database for: an index name, a table, and the key expressions that
// have to be unique together.
type uniqueIndexIntent struct {
	IndexName   string
	Table       string
	KeyExprs    []string
	Recognized  bool
	KeyExprList string
}

// duplicateGroup is one conflicting key, rendered as text by the engine, with
// the number of rows that share it.
type duplicateGroup struct {
	ConflictKey string `gorm:"column:conflict_key"`
	RowCount    int64  `gorm:"column:row_count"`
}

// refuseUniqueIndexOverExistingDuplicates stops a migration that adds a UNIQUE
// index to a table that already holds rows the index cannot cover, and names
// the conflicting groups.
//
// Left to the engine, the same situation is a bare `UNIQUE constraint failed`
// (or `could not create unique index`) with nothing an operator can act on:
// neither engine says which rows collided, and the application that wrote them
// is the only thing that knows what the key means. The alternative — having the
// migration delete or merge the extra rows itself — is not available here. This
// instance stores one person's health history, and a schema change is not
// consent to lose part of it. So the migration refuses, prints the groups, and
// leaves every row exactly where it was for the owner to resolve.
//
// The check derives its query from the statement rather than from a list of
// migrations, so it covers the next unique index as well as this one, and it
// runs only for a statement that actually asks for one. NULL keys are excluded
// because both engines treat NULLs as distinct in a unique index: grouping them
// would report a conflict the index would have accepted.
//
// A statement it cannot read — a partial index, or anything trailing the key
// list — is left alone rather than guessed at: the engine still refuses the
// migration on a genuine conflict, only with its own opaque message.
func refuseUniqueIndexOverExistingDuplicates(database *gorm.DB, migration embeddedMigration, statement string) error {
	intent := parseCreateUniqueIndexStatement(statement)
	if !intent.Recognized {
		return nil
	}
	if !database.Migrator().HasTable(intent.Table) {
		return nil
	}

	groups, err := loadDuplicateKeyGroups(database, intent)
	if err != nil {
		return fmt.Errorf("inspect migration %s: %w", migration.Name, err)
	}
	if len(groups) == 0 {
		return nil
	}

	rendered := make([]string, 0, duplicateGroupReportLimit)
	for _, group := range groups {
		if len(rendered) == duplicateGroupReportLimit {
			rendered = append(rendered, "and more")
			break
		}
		rendered = append(rendered, fmt.Sprintf("%s -> %d rows", group.ConflictKey, group.RowCount))
	}

	return fmt.Errorf(
		"refusing migration %s: table %s already holds rows that unique index %s on (%s) cannot cover, and this migration never deletes, merges or rewrites a row to make room for it. Conflicting group(s), keyed as (%s): %s. Nothing was written: the migration was rolled back and the database is unchanged. Resolve each group by renaming or removing the extra rows through the application, then start it again",
		migration.Name,
		intent.Table,
		intent.IndexName,
		intent.KeyExprList,
		intent.KeyExprList,
		strings.Join(rendered, "; "),
	)
}

// loadDuplicateKeyGroups asks the database which key values already repeat,
// building the query out of the index's own key expressions so the grouping and
// the index agree by construction. The key is rendered as text by the engine —
// `CAST(... AS TEXT)` and `||` are the two spellings both engines share — so
// one reader serves both.
func loadDuplicateKeyGroups(database *gorm.DB, intent uniqueIndexIntent) ([]duplicateGroup, error) {
	renderedKeys := make([]string, 0, len(intent.KeyExprs))
	notNullTerms := make([]string, 0, len(intent.KeyExprs))
	for _, expression := range intent.KeyExprs {
		renderedKeys = append(renderedKeys, "CAST("+expression+" AS TEXT)")
		notNullTerms = append(notNullTerms, "("+expression+") IS NOT NULL")
	}

	query := fmt.Sprintf(
		"SELECT %s AS conflict_key, COUNT(*) AS row_count FROM %s WHERE %s GROUP BY %s HAVING COUNT(*) > 1 ORDER BY conflict_key LIMIT %d",
		strings.Join(renderedKeys, " || ' | ' || "),
		intent.Table,
		strings.Join(notNullTerms, " AND "),
		intent.KeyExprList,
		duplicateGroupReportLimit+1,
	)

	groups := make([]duplicateGroup, 0)
	if err := database.Raw(query).Scan(&groups).Error; err != nil {
		return nil, fmt.Errorf("check table %s for rows that would collide under unique index %s: %w", intent.Table, intent.IndexName, err)
	}
	return groups, nil
}

// parseCreateUniqueIndexStatement reads a statement chunk as a
// `CREATE UNIQUE INDEX`. Like the ADD COLUMN and DROP TABLE detections it looks
// past the prose header splitSQLStatements leaves attached to the first chunk
// of a file. Recognized is false for anything else, and for a unique index
// whose key list is followed by more SQL (a partial index's WHERE, an engine
// option): the key list alone would not describe what the index covers, and a
// guess there would refuse a migration the engine would have accepted.
func parseCreateUniqueIndexStatement(statement string) uniqueIndexIntent {
	body := stripLeadingSQLComments(statement)
	match := createUniqueIndexStatementPattern.FindStringSubmatchIndex(body)
	if match == nil {
		return uniqueIndexIntent{}
	}

	openParen := match[1] - 1
	closeParen := matchingCloseParenIndex(body, openParen)
	if closeParen < 0 {
		return uniqueIndexIntent{}
	}
	if strings.TrimSpace(body[closeParen+1:]) != "" {
		return uniqueIndexIntent{}
	}

	keyExprs := splitTopLevelCommas(body[openParen+1 : closeParen])
	if len(keyExprs) == 0 {
		return uniqueIndexIntent{}
	}

	return uniqueIndexIntent{
		IndexName:   normalizeSQLIdentifier(body[match[2]:match[3]]),
		Table:       normalizeSQLIdentifier(body[match[4]:match[5]]),
		KeyExprs:    keyExprs,
		Recognized:  true,
		KeyExprList: strings.Join(keyExprs, ", "),
	}
}

// matchingCloseParenIndex returns the index of the parenthesis closing the one
// at openParen, or -1 when it is never closed.
func matchingCloseParenIndex(text string, openParen int) int {
	depth := 0
	for index := openParen; index < len(text); index++ {
		switch text[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// splitTopLevelCommas splits an index key list on the commas that separate its
// entries, leaving the commas inside a function call where they are. An empty
// entry makes the whole list unreadable and returns nothing, so the caller
// leaves the statement to the engine rather than building a query around a hole.
func splitTopLevelCommas(list string) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0

	appendPart := func(end int) bool {
		part := strings.TrimSpace(list[start:end])
		if part == "" {
			return false
		}
		parts = append(parts, part)
		return true
	}

	for index := range len(list) {
		switch list[index] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth != 0 {
				continue
			}
			if !appendPart(index) {
				return nil
			}
			start = index + 1
		}
	}
	if !appendPart(len(list)) {
		return nil
	}
	return parts
}

// snapshotEveryTableColumns records the columns of EVERY table there is, read
// before the migration's first statement runs.
//
// It is the second half of the destructive-replay guard and covers the case the
// version comparison cannot see: a database that lost its schema_migrations
// content entirely records no later version, so the whole set replays from 001
// with nothing to compare against. The post-condition below then measures the
// effect itself.
//
// It measures every table rather than the tables the migration textually DROPs,
// because the DROP is an idiom and not the invariant. SQLite has two ways to
// rebuild a table, and the invariant has to hold for both:
//
//	CREATE TABLE t_new (…); INSERT INTO t_new SELECT … FROM t; DROP TABLE t;
//	ALTER TABLE t_new RENAME TO t;                       -- migrations 003, 024
//
//	ALTER TABLE t RENAME TO t_old; CREATE TABLE t (…);
//	INSERT INTO t SELECT … FROM t_old; DROP TABLE t_old; -- the rename-first form
//
// The second narrows t exactly as the first does, and a check keyed on the
// dropped name sees nothing: t_old does not exist yet when the snapshot is
// taken, and t is never dropped at all. Reading the whole schema is what makes
// the guard about the effect instead of about the spelling.
//
// The cost is one column read per table per APPLIED migration, and it lands
// only where a migration actually applies: a boot with nothing to do never
// reaches applyMigration, because applyEmbeddedMigrations skips on the ledger
// first, so a running instance pays nothing. Priced on this host 2026-08-20,
// 20 clean SQLite bootstraps of the whole set each: mean 300 ms with the
// snapshot against 113 ms without, best 218 ms against 88 ms — roughly 150 ms
// added once, at install or at an upgrade that applies the whole set. On
// Postgres the difference did not separate from container startup (9.3 s
// against 14.3 s, in the wrong direction). Keyed on the schema rather than on
// the statement text on purpose: a textual precondition for taking the
// snapshot would have to enumerate the ways a table can lose a column, which
// is the assumption that let the rename-first form through.
func snapshotEveryTableColumns(database *gorm.DB) ([]tableColumnsBefore, error) {
	tables, err := database.Migrator().GetTables()
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	snapshot := make([]tableColumnsBefore, 0, len(tables))
	for _, table := range tables {
		columns, err := tableColumnNames(database, table)
		if err != nil {
			return nil, err
		}
		snapshot = append(snapshot, tableColumnsBefore{Table: normalizeSQLIdentifier(table), Columns: columns})
	}
	return snapshot, nil
}

// requireNoTableSilentlyNarrowed fails the migration — and, being inside its
// transaction, rolls back every statement it ran — when a table lost a column,
// or the table itself, without the migration having said so.
//
// Both losses are expressible, and each escape hatch is a thing the author
// writes out by hand and names:
//
//   - a column removed by an explicit `ALTER TABLE t DROP COLUMN c` in this
//     migration's own SQL is the visible form of that intent and is allowed;
//   - a table removed on purpose carries the marker line
//     `-- ovumcy:removes-table <name>` (see removedTableMarkerPattern), which
//     names the one table it authorizes.
//
// A bare `DROP TABLE t` is deliberately NOT an authorization: it is the middle
// of the rebuild idiom above, where the author's intent is to replace the
// table, not to remove it — reading it as consent is exactly how migration 003
// came to discard eight health columns while reporting success.
func requireNoTableSilentlyNarrowed(database *gorm.DB, migration embeddedMigration, columnsBefore []tableColumnsBefore, statements []string) error {
	removedOnPurpose := tablesRemovedOnPurpose(migration)
	accountedFor := columnsAccountedForByName(statements)

	for _, before := range columnsBefore {
		if !database.Migrator().HasTable(before.Table) {
			if _, authorized := removedOnPurpose[before.Table]; authorized {
				continue
			}
			return fmt.Errorf(
				"refusing migration %s: table %s is gone after it ran and the migration does not say it meant to remove it — a rebuild that drops a table must put it back. Nothing was written: the migration was rolled back and the database is unchanged. If the removal is intended, the migration must carry the line `-- %s %s`; if it is not, and migration %s is in fact already applied, restore its schema_migrations row, otherwise restore from a backup",
				migration.Name,
				before.Table,
				removedTableMarker,
				before.Table,
				migration.Version,
			)
		}

		columnsNow, err := tableColumnNames(database, before.Table)
		if err != nil {
			return fmt.Errorf("inspect migration %s: %w", migration.Name, err)
		}

		missing := missingColumnNames(before.Columns, columnsNow)
		missing = withoutAccountedForColumns(missing, accountedFor[before.Table])
		if len(missing) == 0 {
			continue
		}

		return fmt.Errorf(
			"refusing migration %s: table %s lost column(s) %s, which held data before it ran, and no statement in the migration drops or renames them — a rebuild recreates the table from the columns its own version knew about, so re-applying it on a newer schema discards everything added after it. Nothing was written: the migration was rolled back and the database is unchanged; if migration %s is in fact applied, restore its schema_migrations row, otherwise restore from a backup",
			migration.Name,
			before.Table,
			strings.Join(missing, ", "),
			migration.Version,
		)
	}

	return nil
}

// tablesRemovedOnPurpose returns the tables a migration declares it means to
// remove for good.
//
// The declaration is a comment line of its own naming ONE table:
//
//	-- ovumcy:removes-table register_pickup_tokens
//
// It lives in the migration because that is where the intent is, reviewed as
// one file with the statement that carries it out, and it cannot be written by
// accident: the marker is namespaced, it is not English, and it authorizes only
// the table it names — a migration that retires one table and rebuilds another
// still has to put the rebuilt one back. It is read from the raw SQL rather
// than from a statement chunk because the runner splits on `;` and a comment
// can land anywhere.
func tablesRemovedOnPurpose(migration embeddedMigration) map[string]struct{} {
	removed := make(map[string]struct{})
	for _, match := range removedTableMarkerPattern.FindAllStringSubmatch(migration.SQL, -1) {
		removed[normalizeSQLIdentifier(match[1])] = struct{}{}
	}
	return removed
}

// columnsAccountedForByName returns table -> the column names whose
// disappearance this migration explains in its own SQL. There are two visible
// forms of that intent and both engines support both:
//
//	ALTER TABLE t DROP COLUMN c            -- the column is retired
//	ALTER TABLE t RENAME COLUMN c TO other -- the column is still there, renamed
//
// A rename is not a loss: the values are under the new name, and refusing it
// would have the guard reporting a data loss that did not happen, which is the
// mirror of the defect it exists to catch. Only the OLD name of the statement
// is accounted for — renaming one column says nothing about the rest of the
// table, so a rebuild that quietly drops a second column is still refused.
//
// The `TO` clause is what separates a column rename from a TABLE rename:
// `ALTER TABLE t RENAME TO t_old` has no name before the TO and therefore
// matches nothing here, which matters because that statement is the first half
// of the rename-first rebuild idiom and must never authorize anything.
func columnsAccountedForByName(statements []string) map[string]map[string]struct{} {
	accountedFor := make(map[string]map[string]struct{})

	record := func(tableName string, columnName string) {
		table := normalizeSQLIdentifier(tableName)
		if accountedFor[table] == nil {
			accountedFor[table] = make(map[string]struct{})
		}
		accountedFor[table][normalizeSQLIdentifier(columnName)] = struct{}{}
	}

	for _, statement := range statements {
		body := stripLeadingSQLComments(statement)
		if matches := dropColumnStatementPattern.FindStringSubmatch(body); len(matches) == 3 {
			record(matches[1], matches[2])
			continue
		}
		if matches := renameColumnStatementPattern.FindStringSubmatch(body); len(matches) == 4 {
			record(matches[1], matches[2])
		}
	}
	return accountedFor
}

// withoutAccountedForColumns removes from missing the column names whose
// disappearance the migration itself explained.
func withoutAccountedForColumns(missing []string, accountedFor map[string]struct{}) []string {
	if len(accountedFor) == 0 {
		return missing
	}

	kept := make([]string, 0, len(missing))
	for _, name := range missing {
		if _, explained := accountedFor[name]; explained {
			continue
		}
		kept = append(kept, name)
	}
	return kept
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
