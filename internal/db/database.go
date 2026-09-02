package db

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

type Config struct {
	Driver      Driver
	SQLitePath  string
	PostgresURL string
}

func (config Config) normalized() Config {
	normalized := config
	driver := strings.ToLower(strings.TrimSpace(string(config.Driver)))
	if driver == "" {
		driver = string(DriverSQLite)
	}
	normalized.Driver = Driver(driver)
	normalized.SQLitePath = strings.TrimSpace(config.SQLitePath)
	normalized.PostgresURL = strings.TrimSpace(config.PostgresURL)
	return normalized
}

func (config Config) Validate() error {
	switch config.normalized().Driver {
	case DriverSQLite:
		if strings.TrimSpace(config.SQLitePath) == "" {
			return errors.New("sqlite requires DB_PATH")
		}
		return nil
	case DriverPostgres:
		if strings.TrimSpace(config.PostgresURL) == "" {
			return errors.New("postgres requires DATABASE_URL")
		}
		return nil
	default:
		return fmt.Errorf("unsupported DB_DRIVER %q", config.Driver)
	}
}

func OpenDatabase(config Config) (*gorm.DB, error) {
	return openDatabase(config, true)
}

// OpenDatabaseWithoutMigrations opens the same pool and applies nothing.
//
// It exists for the offline repair path, and only for it. A migration that
// refuses — the per-owner symptom-name index meeting rows it cannot cover — ends
// every boot AND every other operator subcommand, because all of them reach the
// database through OpenDatabase and therefore through the same refusal. An
// operator told to fix the data "through the application" would have no
// application to fix it with, so the one subcommand whose job is to make the
// migration applicable has to be able to open a database the migration has not
// been applied to.
//
// What it returns is a database of UNKNOWN schema version, older than the binary
// by however many migrations are pending. A caller may touch only tables and
// columns that predate the migration that is stuck, and must never assume a
// column a pending migration adds. Everything else keeps using OpenDatabase.
func OpenDatabaseWithoutMigrations(config Config) (*gorm.DB, error) {
	return openDatabase(config, false)
}

func openDatabase(config Config, applyMigrations bool) (*gorm.DB, error) {
	normalized := config.normalized()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}

	var (
		database *gorm.DB
		err      error
	)

	switch normalized.Driver {
	case DriverSQLite:
		database, err = openSQLiteConnection(normalized.SQLitePath)
	case DriverPostgres:
		database, err = openPostgresConnection(normalized.PostgresURL)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q", normalized.Driver)
	}
	if err != nil {
		return nil, err
	}

	// The caller owns the handle only when this function returns one: every caller
	// (cmd/ovumcy, internal/cli) closes the pool it received and has nothing to
	// close when it gets an error back. So a failure AFTER the connection is open
	// has to release the pool here, or the sql.DB — and on SQLite the open
	// database file — leaks for the process lifetime, which matters most in the
	// CLI subcommands, whose process keeps running after a failed open. Deferring
	// the close and clearing it only on the success return keeps that true for
	// every post-open error path, including ones added later.
	handedToCaller := false
	defer func() {
		if !handedToCaller {
			closeOpenedPool(database)
		}
	}()

	if applyMigrations {
		if err := applyEmbeddedMigrations(database, normalized.Driver); err != nil {
			return nil, fmt.Errorf("apply embedded migrations: %w", err)
		}
	}

	handedToCaller = true
	return database, nil
}

// closeOpenedPool releases the connection pool behind a handle OpenDatabase
// opened but is not going to return. Both failures it can meet are ignored on
// purpose: the error the caller receives must stay the one that explains why the
// open failed, and neither "the pool was already unavailable" nor "closing a pool
// we are discarding failed" tells an operator anything actionable about that.
func closeOpenedPool(database *gorm.DB) {
	sqlDB, err := database.DB()
	if err != nil {
		return // codecov:ignore -- defensive: (*gorm.DB).DB() only errors when the connection pool is unavailable, which cannot happen on the handle gorm.Open just returned. Mirrors the same guard in internal/cli.
	}
	_ = sqlDB.Close()
}
