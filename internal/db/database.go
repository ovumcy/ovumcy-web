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

	if err := applyEmbeddedMigrations(database, normalized.Driver); err != nil {
		return nil, fmt.Errorf("apply embedded migrations: %w", err)
	}

	return database, nil
}

func OpenSQLite(dbPath string) (*gorm.DB, error) {
	return OpenDatabase(Config{
		Driver:     DriverSQLite,
		SQLitePath: dbPath,
	})
}
