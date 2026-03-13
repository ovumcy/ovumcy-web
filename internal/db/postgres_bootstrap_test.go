package db

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

func TestOpenPostgresAppliesEmbeddedMigrationsOnCleanDatabase(t *testing.T) {
	config := startPostgresTestConfig(t)
	database := openPostgresForMigrationBootstrapTest(t, config)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertAllEmbeddedMigrationsAppliedForDriver(t, database, DriverPostgres)
	assertPostgresNormalizedEmailUniqueness(t, database)
	assertPostgresDailyLogsSchemaReconciled(t, database)
}

func TestOpenPostgresMigrationBootstrapIsIdempotent(t *testing.T) {
	config := startPostgresTestConfig(t)

	firstOpen, err := OpenDatabase(config)
	if err != nil {
		t.Fatalf("first open postgres: %v", err)
	}
	firstRecords := loadMigrationRecords(t, firstOpen)

	firstSQLDB, err := firstOpen.DB()
	if err != nil {
		t.Fatalf("first open sql db: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first sql db: %v", err)
	}

	secondOpen := openPostgresForMigrationBootstrapTest(t, config)
	secondRecords := loadMigrationRecords(t, secondOpen)

	if len(firstRecords) != len(secondRecords) {
		t.Fatalf("expected identical postgres migration record count, before=%v after=%v", firstRecords, secondRecords)
	}
	for index := range firstRecords {
		if firstRecords[index].Version != secondRecords[index].Version || firstRecords[index].Name != secondRecords[index].Name {
			t.Fatalf("expected postgres migration records to remain unchanged, before=%v after=%v", firstRecords, secondRecords)
		}
	}
}

func TestUserRepositoryCreateTranslatesPostgresUniqueViolation(t *testing.T) {
	config := startPostgresTestConfig(t)
	database := openPostgresForMigrationBootstrapTest(t, config)
	repo := NewUserRepository(database)

	first := &models.User{
		Email:            "unique@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(first); err != nil {
		t.Fatalf("create first postgres user: %v", err)
	}

	second := &models.User{
		Email:            " UNIQUE@example.com ",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	err := repo.Create(second)
	if err == nil {
		t.Fatal("expected postgres unique violation error")
	}

	var uniqueErr *UniqueConstraintError
	if !errors.As(err, &uniqueErr) {
		t.Fatalf("expected UniqueConstraintError for postgres, got %T %v", err, err)
	}
}

func openPostgresForMigrationBootstrapTest(t *testing.T, config Config) *gorm.DB {
	t.Helper()

	database, err := OpenDatabase(config)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open postgres sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return database
}

func assertAllEmbeddedMigrationsAppliedForDriver(t *testing.T, database *gorm.DB, driver Driver) {
	t.Helper()

	expectedVersions := embeddedMigrationVersionsForDriverTest(t, driver)
	actualVersions := make([]string, 0)

	var rows []struct {
		Version string `gorm:"column:version"`
	}
	if err := database.Raw(`SELECT version FROM schema_migrations ORDER BY version ASC`).Scan(&rows).Error; err != nil {
		t.Fatalf("load applied migration versions: %v", err)
	}
	for _, row := range rows {
		actualVersions = append(actualVersions, row.Version)
	}

	if len(expectedVersions) != len(actualVersions) {
		t.Fatalf("unexpected applied migration versions for %s: expected=%v actual=%v", driver, expectedVersions, actualVersions)
	}
	for index := range expectedVersions {
		if expectedVersions[index] != actualVersions[index] {
			t.Fatalf("unexpected applied migration versions for %s: expected=%v actual=%v", driver, expectedVersions, actualVersions)
		}
	}
}

func assertPostgresNormalizedEmailUniqueness(t *testing.T, database *gorm.DB) {
	t.Helper()

	firstUser := models.User{
		Email:        "QA-Test2@Ovumcy.Local",
		PasswordHash: "hash-1",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := database.Create(&firstUser).Error; err != nil {
		t.Fatalf("create first postgres user: %v", err)
	}

	secondUser := models.User{
		Email:        " qa-test2@ovumcy.local ",
		PasswordHash: "hash-2",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := database.Create(&secondUser).Error; err == nil {
		t.Fatalf("expected duplicate normalized email insert to fail on postgres")
	}
}

func assertPostgresDailyLogsSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	user := models.User{
		Email:        "postgres-daily-log@example.com",
		PasswordHash: "hash",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create postgres daily-log user: %v", err)
	}

	if !database.Migrator().HasColumn("daily_logs", "mood") {
		t.Fatal("expected postgres daily_logs.mood column to exist after migrations")
	}
	if !database.Migrator().HasColumn("daily_logs", "sex_activity") {
		t.Fatal("expected postgres daily_logs.sex_activity column to exist after migrations")
	}
	if !database.Migrator().HasColumn("daily_logs", "bbt") {
		t.Fatal("expected postgres daily_logs.bbt column to exist after migrations")
	}
	if !database.Migrator().HasColumn("daily_logs", "cervical_mucus") {
		t.Fatal("expected postgres daily_logs.cervical_mucus column to exist after migrations")
	}
	if !database.Migrator().HasColumn("daily_logs", "cycle_start") {
		t.Fatal("expected postgres daily_logs.cycle_start column to exist after migrations")
	}
	if !database.Migrator().HasColumn("daily_logs", "is_uncertain") {
		t.Fatal("expected postgres daily_logs.is_uncertain column to exist after migrations")
	}
	if !database.Migrator().HasColumn("users", "luteal_phase") {
		t.Fatal("expected postgres users.luteal_phase column to exist after migrations")
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, mood, sex_activity, bbt, cervical_mucus, symptom_ids, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		"2026-01-11",
		true,
		"custom-flow",
		4,
		models.SexActivityProtected,
		36.54,
		models.CervicalMucusEggWhite,
		nil,
		"postgres-schema-check",
	).Error; err != nil {
		t.Fatalf("expected postgres daily_logs schema to allow new tracking fields with nullable symptom_ids and custom flow, got %v", err)
	}
}
