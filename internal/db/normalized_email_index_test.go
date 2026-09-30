package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestVerifyNormalizedEmailIndexOnSQLite(t *testing.T) {
	config := migratedSQLiteConfig(t, filepath.Join(t.TempDir(), "normalized-email-index.db"))
	runNormalizedEmailIndexChecks(t, config)
}

func TestVerifyNormalizedEmailIndexOnPostgres(t *testing.T) {
	runNormalizedEmailIndexChecks(t, startPostgresTestConfig(t))
}

// runNormalizedEmailIndexChecks drives the check against one migrated database
// of either engine. Every index it builds is derived from the definition the
// engine reports for migration 002's own index, never typed by hand, so the
// accepted case pins each engine's real rendering and each refused case differs
// from it in exactly one respect.
func runNormalizedEmailIndexChecks(t *testing.T, config Config) {
	database := openPostgresForMigrationBootstrapTest(t, config)
	repo := NewHealthRepository(database)
	ctx := context.Background()

	entry, found, err := repo.loadIndexCatalogEntry(ctx, NormalizedEmailIndexName)
	if err != nil || !found || !entry.Usable {
		t.Fatalf("migration 002's index must be readable from the catalog and usable (found=%v, entry=%+v, err=%v)", found, entry, err)
	}
	migrated, schemaPrefix := entry.Definition, entry.SchemaPrefix
	if got := canonicalIndexDefinition(migrated, schemaPrefix); !isNormalizedEmailIndexDefinition(got) {
		t.Fatalf("the migrated index %q must canonicalize to the expected definition, got %q", migrated, got)
	}
	if err := repo.VerifyNormalizedEmailIndex(ctx); err != nil {
		t.Fatalf("a freshly migrated database must pass, got %v", err)
	}

	refused := map[string]string{
		"a different key expression": strings.Replace(migrated, "lower(", "upper(", 1),
		"a non-unique index":         strings.Replace(migrated, "UNIQUE ", "", 1),
		"a partial index":            migrated + " WHERE email <> ''",
	}
	for name, definition := range refused {
		t.Run(name, func(t *testing.T) {
			if definition == migrated {
				t.Fatalf("the derived definition did not change %q", migrated)
			}
			recreateNormalizedEmailIndex(t, database, definition)
			err := repo.VerifyNormalizedEmailIndex(ctx)
			if err == nil {
				t.Fatalf("index %q must be refused", definition)
			}
			for _, want := range []string{NormalizedEmailIndexName + " is not the unique index", "DROP INDEX " + NormalizedEmailIndexName, "version = '002'"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal must contain %q, got %v", want, err)
				}
			}
		})
	}

	t.Run("redundant parentheses around the key", func(t *testing.T) {
		recreateNormalizedEmailIndex(t, database, strings.Replace(migrated, "(lower(", "((lower(", 1)+")")
		if err := repo.VerifyNormalizedEmailIndex(ctx); err != nil {
			t.Fatalf("the same key in extra parentheses is the same index, got %v", err)
		}
	})

	if config.Driver == DriverPostgres {
		var listed string
		if err := database.Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?", NormalizedEmailIndexName).Scan(&listed).Error; err != nil || listed != migrated {
			t.Fatalf("the catalog read must report the definition pg_indexes lists, got %q against %q (err=%v)", migrated, listed, err)
		}
		t.Run("an index a failed CREATE INDEX CONCURRENTLY left behind", func(t *testing.T) {
			leaveAFailedConcurrentBuild(t, database, migrated)
			requireUnusableIndexRefused(t, repo, migrated)
		})
		t.Run("an index built but never validated", func(t *testing.T) {
			// A CONCURRENTLY build that fails in its validation pass leaves the
			// index ready (maintained on writes) but not valid. The test
			// container's role is a superuser, so the flag is set directly.
			t.Cleanup(func() { recreateNormalizedEmailIndex(t, database, migrated) })
			if err := database.Exec("UPDATE pg_index SET indisvalid = false WHERE indexrelid = ?::regclass", NormalizedEmailIndexName).Error; err != nil {
				t.Fatalf("clear indisvalid: %v", err)
			}
			requireUnusableIndexRefused(t, repo, migrated)
		})
	}

	t.Run("a missing index, restored by the remedy the refusal names", func(t *testing.T) {
		if err := database.Exec("DROP INDEX " + NormalizedEmailIndexName).Error; err != nil {
			t.Fatalf("drop index: %v", err)
		}
		err := repo.VerifyNormalizedEmailIndex(ctx)
		if err == nil {
			t.Fatal("a database without the index must be refused")
		}
		const remedy = "DELETE FROM schema_migrations WHERE version = '002'"
		if !strings.Contains(err.Error(), NormalizedEmailIndexName+" on users (lower(trim(email))) is missing") || !strings.Contains(err.Error(), remedy) {
			t.Fatalf("refusal must name the index and the remedy, got %v", err)
		}

		if err := database.Exec(remedy).Error; err != nil {
			t.Fatalf("apply the remedy: %v", err)
		}
		reopened := openPostgresForMigrationBootstrapTest(t, config)
		if err := NewHealthRepository(reopened).VerifyNormalizedEmailIndex(ctx); err != nil {
			t.Fatalf("re-applying migration 002 must restore the index, got %v", err)
		}
	})
}

// leaveAFailedConcurrentBuild builds the catalog state the definition alone
// cannot tell apart from a good index: two rows on one normalized address, then
// the migrated definition re-issued CONCURRENTLY, which fails on them and leaves
// the index behind. Its cleanup restores the migrated index.
func leaveAFailedConcurrentBuild(t *testing.T, database *gorm.DB, migrated string) {
	t.Helper()
	if err := database.Exec("DROP INDEX " + NormalizedEmailIndexName).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
	for _, email := range []string{"shared@example.com", " Shared@Example.com"} {
		if err := database.Exec(
			`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
			email, "hash", "owner",
		).Error; err != nil {
			t.Fatalf("insert %q: %v", email, err)
		}
	}
	t.Cleanup(func() {
		_ = database.Exec("DROP INDEX IF EXISTS " + NormalizedEmailIndexName).Error
		_ = database.Exec("DELETE FROM users WHERE lower(trim(email)) = 'shared@example.com'").Error
		_ = database.Exec(migrated).Error
	})

	concurrently := strings.Replace(migrated, "CREATE UNIQUE INDEX ", "CREATE UNIQUE INDEX CONCURRENTLY ", 1)
	if concurrently == migrated {
		t.Fatalf("the derived definition did not change %q", migrated)
	}
	if err := database.Exec(concurrently).Error; err == nil {
		t.Fatal("building the unique index over two rows on one address must fail")
	}
}

// requireUnusableIndexRefused holds the check to refusing an index whose
// definition is exactly the migrated one, so the refusal can only come from the
// catalog marking it unusable.
func requireUnusableIndexRefused(t *testing.T, repo *HealthRepository, migrated string) {
	t.Helper()
	ctx := context.Background()
	entry, found, err := repo.loadIndexCatalogEntry(ctx, NormalizedEmailIndexName)
	if err != nil || !found || entry.Usable || entry.Definition != migrated {
		t.Fatalf("the catalog must hold the migrated definition, unusable (found=%v, entry=%+v, err=%v)", found, entry, err)
	}

	err = repo.VerifyNormalizedEmailIndex(ctx)
	if err == nil {
		t.Fatal("an index the database marks invalid must be refused")
	}
	for _, want := range []string{NormalizedEmailIndexName + " has the definition migration 002 builds but the database marks it invalid", "DROP INDEX " + NormalizedEmailIndexName, "version = '002'"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal must contain %q, got %v", want, err)
		}
	}
}

func recreateNormalizedEmailIndex(t *testing.T, database *gorm.DB, definition string) {
	t.Helper()
	if err := database.Exec("DROP INDEX " + NormalizedEmailIndexName).Error; err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if err := database.Exec(definition).Error; err != nil {
		t.Fatalf("create index %q: %v", definition, err)
	}
}

func TestVerifyNormalizedEmailIndexReportsAnUnreadableCatalog(t *testing.T) {
	database := openHealthProbeDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	err = NewHealthRepository(database).VerifyNormalizedEmailIndex(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read the definition of index "+NormalizedEmailIndexName) {
		t.Fatalf("a catalog that cannot be read must be reported, got %v", err)
	}
}
