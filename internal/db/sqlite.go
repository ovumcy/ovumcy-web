package db

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func OpenSQLite(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_foreign_keys=on&_busy_timeout=5000", dbPath)
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newSQLiteLogger(os.Stdout),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := applyEmbeddedMigrations(database); err != nil {
		return nil, fmt.Errorf("apply embedded migrations: %w", err)
	}

	return database, nil
}

func newSQLiteLogger(output io.Writer) gormlogger.Interface {
	if output == nil {
		output = os.Stdout
	}

	return gormlogger.New(
		log.New(output, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
			ParameterizedQueries:      true,
		},
	)
}
