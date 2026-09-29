package db

import (
	"context"
	"fmt"
	"strings"
)

// NormalizedEmailIndexName is the unique index migration 002 builds on
// users(lower(trim(email))). It is the only thing that stops two accounts from
// sharing one normalized address: the lookups that refuse an ambiguous address
// (resolveUniqueUserByEmail) lock such accounts out after the fact, they do not
// prevent the duplicate.
const NormalizedEmailIndexName = "idx_users_email_normalized"

// normalizedEmailIndexKey is the index's key as canonicalIndexDefinition reduces
// both engines' renderings of it: SQLite keeps migration 002's own text, and
// Postgres renders trim(x) as TRIM(BOTH FROM x).
const normalizedEmailIndexKey = "lower(trim(email))"

// normalizedEmailIndexRestore is the remedy both refusals end with. The index is
// never re-created at boot: re-applying migration 002 is what checks the table
// for addresses two accounts already share before building it, and names them.
const normalizedEmailIndexRestore = "delete migration 002's ledger row " +
	"(DELETE FROM schema_migrations WHERE version = '002') and start the server again: migration " +
	"002_users_schema_reconcile.sql is re-applied and re-creates the index, or refuses and names the " +
	"rows if two accounts already share an address"

// VerifyNormalizedEmailIndex returns nil only when NormalizedEmailIndexName
// exists as migration 002 defines it: UNIQUE, on users, keyed on
// lower(trim(email)), with no predicate, and on Postgres valid and ready. It
// reads the engine's catalog — sqlite_master, or pg_indexes and pg_index — and
// writes nothing, so a refusal leaves the database exactly as it found it.
func (repo *HealthRepository) VerifyNormalizedEmailIndex(ctx context.Context) error {
	index, found, err := repo.loadIndexCatalogEntry(ctx, NormalizedEmailIndexName)
	if err != nil {
		return fmt.Errorf("read the definition of index %s: %w", NormalizedEmailIndexName, err)
	}
	if !found {
		return fmt.Errorf(
			"unique index %s on users (%s) is missing, and the server does not start without it: it is what stops two accounts from sharing one email address. With the server stopped, %s",
			NormalizedEmailIndexName, normalizedEmailIndexKey, normalizedEmailIndexRestore,
		)
	}
	if !isNormalizedEmailIndexDefinition(canonicalIndexDefinition(index.Definition, index.SchemaPrefix)) {
		return fmt.Errorf(
			"index %s is not the unique index on users (%s) that migration 002 builds (found %q), and the server does not start without it: it is what stops two accounts from sharing one email address. With the server stopped, drop it (DROP INDEX %s), then %s",
			NormalizedEmailIndexName, normalizedEmailIndexKey, index.Definition, NormalizedEmailIndexName, normalizedEmailIndexRestore,
		)
	}
	// A CREATE INDEX CONCURRENTLY or REINDEX that fails — on two rows already
	// sharing an address, say — leaves the index in the catalog with the right
	// definition but invalid, and migration 002's IF NOT EXISTS would skip it on
	// re-apply, so this remedy drops it first.
	if !index.Usable {
		return fmt.Errorf(
			"index %s has the definition migration 002 builds but the database marks it invalid, as a failed CREATE INDEX CONCURRENTLY or REINDEX leaves it, so it does not stop two accounts from sharing one email address, and the server does not start without it. With the server stopped, drop it (DROP INDEX %s), then %s",
			NormalizedEmailIndexName, NormalizedEmailIndexName, normalizedEmailIndexRestore,
		)
	}
	return nil
}

// indexCatalogEntry is one index as the catalog reports it. Usable is always
// true on SQLite, which has no half-built index state.
type indexCatalogEntry struct {
	Definition   string `gorm:"column:definition"`
	SchemaPrefix string `gorm:"column:schema_prefix"`
	Usable       bool   `gorm:"column:usable"`
}

// loadIndexCatalogEntry reads one index's DDL from the catalog. On Postgres it
// looks only in current_schema(), where the migrations built the index, returns
// that schema as pg_get_indexdef quotes it, because the definition qualifies the
// table with it, and joins pg_index for indisvalid, which pg_indexes does not
// expose. indisvalid alone suffices: a concurrent build marks an index ready
// before valid, and a concurrent drop clears valid before ready and live.
func (repo *HealthRepository) loadIndexCatalogEntry(ctx context.Context, indexName string) (indexCatalogEntry, bool, error) {
	var query string
	switch dialect := repo.database.Name(); dialect {
	case string(DriverSQLite):
		query = `SELECT COALESCE(sql, '') AS definition, '' AS schema_prefix, 1 AS usable FROM sqlite_master WHERE type = 'index' AND name = ?`
	case string(DriverPostgres):
		query = `SELECT listed.indexdef AS definition, quote_ident(listed.schemaname) AS schema_prefix,
			state.indisvalid AS usable
			FROM pg_indexes AS listed
			JOIN pg_index AS state ON state.indexrelid = format('%I.%I', listed.schemaname, listed.indexname)::regclass
			WHERE listed.schemaname = current_schema() AND listed.indexname = ?`
	default:
		return indexCatalogEntry{}, false, fmt.Errorf("unsupported database dialect %q", dialect) // codecov:ignore -- OpenDatabase builds only sqlite and postgres handles
	}

	rows := make([]indexCatalogEntry, 0, 1)
	if err := repo.database.WithContext(ctx).Raw(query, indexName).Scan(&rows).Error; err != nil {
		return indexCatalogEntry{}, false, err
	}
	if len(rows) == 0 {
		return indexCatalogEntry{}, false, nil
	}
	return rows[0], true, nil
}

// canonicalIndexDefinition reduces an index's DDL, as either engine reports it,
// to one lowercase, space-free form: SQLite stores the CREATE statement as
// written, less IF NOT EXISTS, while Postgres reconstructs it with the table
// schema-qualified, the access method named and trim(x) in its SQL-standard
// spelling.
func canonicalIndexDefinition(definition string, schemaPrefix string) string {
	text := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	if schemaPrefix != "" {
		text = strings.Replace(text, " on "+strings.ToLower(schemaPrefix)+".", " on ", 1)
	}
	for _, rendering := range []struct{ from, to string }{
		{" using btree", ""},
		{"trim(both from ", "trim("},
		{`"`, ""},
		{" ", ""},
	} {
		text = strings.ReplaceAll(text, rendering.from, rendering.to)
	}
	return text
}

// isNormalizedEmailIndexDefinition reports whether a canonical definition is
// the unique index on users over normalizedEmailIndexKey and nothing else. The
// key list must close the statement — anything after it is a partial index's
// predicate — and redundant parentheses around the key, which the Postgres
// migration writes and SQLite would keep, are not a different index.
func isNormalizedEmailIndexDefinition(canonical string) bool {
	keyList, ok := strings.CutPrefix(canonical, "createuniqueindex"+NormalizedEmailIndexName+"onusers")
	if !ok {
		return false
	}
	for strings.HasPrefix(keyList, "(") && matchingCloseParenIndex(keyList, 0) == len(keyList)-1 {
		keyList = keyList[1 : len(keyList)-1]
	}
	return keyList == normalizedEmailIndexKey
}
