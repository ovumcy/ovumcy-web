package db

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openPostgresConnection(databaseURL string) (*gorm.DB, error) {
	database, err := gorm.Open(postgres.Open(databaseURL), newGORMConfig(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Cap the connection pool. Without limits pgx/lib/pq will open a new
	// connection for every goroutine, exhausting Postgres's max_connections
	// under load. 10 open / 5 idle is a conservative bound suitable for a
	// single-operator deployment; adjust via DATABASE_POOL_* env vars if
	// this is ever extracted to config.
	// codecov:ignore:start -- no Postgres instance in unit-test coverage; the pool config runs only on the optional Postgres deployment path (exercised by the postgres e2e smoke)
	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get postgres sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	// codecov:ignore:end

	return database, nil
}
