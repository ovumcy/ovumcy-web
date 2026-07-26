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
