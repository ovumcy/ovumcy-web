package db

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ovumcy/ovumcy-web/internal/models"
	embeddedmigrations "github.com/ovumcy/ovumcy-web/migrations"
	"gorm.io/gorm"
)

func TestOpenSQLiteAppliesEmbeddedMigrationsOnCleanDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-clean.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertDailyLogsSchemaReconciled(t, database)
	assertOIDCLogoutStateSchemaReconciled(t, database)
	assertNormalizedEmailIndexExists(t, database)
	assertAllEmbeddedMigrationsApplied(t, database)
}

func TestOpenSQLiteUpgradesLegacyInitSchema(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-legacy.db")
	seedLegacyInitSchema(t, databasePath)

	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertDailyLogsSchemaReconciled(t, database)
	assertOIDCLogoutStateSchemaReconciled(t, database)
	assertNormalizedEmailIndexExists(t, database)
	assertAllEmbeddedMigrationsApplied(t, database)

	assertMigratedLegacyUserDefaults(t, database)
	assertMigratedLegacyDailyLogDefaults(t, database)
	assertMigratedLegacyDailyLogDateCanonicalized(t, database)
}

// TestMigration019CanonicalizesNonUTCDateFields locks the on-disk
// rewrite contract of migration 019: legacy rows whose stored date or
// last_period_start carries a non-UTC offset (because they were written
// before the DailyLog BeforeSave hook landed) must be rewritten to
// canonical UTC-midnight TEXT form. The migration is idempotent — a row
// already in canonical form is left at the same value.
func TestMigration019CanonicalizesNonUTCDateFields(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-019.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, last_period_start, created_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"non-canonical@example.com",
		"test-hash",
		"owner",
		"2026-02-15 00:00:00-05:00",
	).Error; err != nil {
		t.Fatalf("insert non-canonical user: %v", err)
	}

	var nonCanonicalUser struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(
		`SELECT id FROM users WHERE email = ?`, "non-canonical@example.com",
	).Scan(&nonCanonicalUser).Error; err != nil {
		t.Fatalf("load non-canonical user id: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		nonCanonicalUser.ID,
		"2026-02-20 00:00:00+09:00",
		true,
		"medium",
		"[]",
		"non-canonical-log",
	).Error; err != nil {
		t.Fatalf("insert non-canonical daily log: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		nonCanonicalUser.ID,
		"2026-02-21 00:00:00+00:00",
		false,
		"none",
		"[]",
		"already-canonical-log",
	).Error; err != nil {
		t.Fatalf("insert already-canonical daily log: %v", err)
	}

	if err := database.Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, "019",
	).Error; err != nil {
		t.Fatalf("delete migration 019 record: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT last_period_start FROM users WHERE email = ?`,
		"non-canonical@example.com",
		time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC),
	)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT date FROM daily_logs WHERE notes = ?`,
		"non-canonical-log",
		time.Date(2026, time.February, 20, 0, 0, 0, 0, time.UTC),
	)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT date FROM daily_logs WHERE notes = ?`,
		"already-canonical-log",
		time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC),
	)
}

// assertStoredDateEqualsUTCMidnight reads the column the query selects
// and asserts the value, parsed by the glebarez driver into time.Time,
// equals the expected UTC-midnight instant. This is format-agnostic
// (the driver reformats DATE columns on read, so byte-equal comparisons
// are unreliable) but instant-strict — a non-UTC offset row resolves to
// a different instant than the canonical UTC-midnight target.
func assertStoredDateEqualsUTCMidnight(t *testing.T, database *gorm.DB, query string, arg any, expected time.Time) {
	t.Helper()

	var stored time.Time
	if err := database.Raw(query, arg).Row().Scan(&stored); err != nil {
		t.Fatalf("scan stored date for %v: %v", arg, err)
	}
	if !stored.Equal(expected) {
		t.Fatalf("expected stored date %s for %v, got %s", expected.Format(time.RFC3339), arg, stored.Format(time.RFC3339))
	}
	if stored.Hour() != 0 || stored.Minute() != 0 || stored.Second() != 0 {
		t.Fatalf("expected midnight time-of-day for %v, got %s", arg, stored.Format(time.RFC3339Nano))
	}
}

func assertMigratedLegacyUserDefaults(t *testing.T, database *gorm.DB) {
	t.Helper()

	var migratedUser struct {
		Email               string `gorm:"column:email"`
		DisplayName         string `gorm:"column:display_name"`
		LocalAuthEnabled    bool   `gorm:"column:local_auth_enabled"`
		AuthSessionVersion  int    `gorm:"column:auth_session_version"`
		OnboardingCompleted bool   `gorm:"column:onboarding_completed"`
		CycleLength         int    `gorm:"column:cycle_length"`
		PeriodLength        int    `gorm:"column:period_length"`
		LutealPhase         int    `gorm:"column:luteal_phase"`
		AutoPeriodFill      bool   `gorm:"column:auto_period_fill"`
		IrregularCycle      bool   `gorm:"column:irregular_cycle"`
		TrackBBT            bool   `gorm:"column:track_bbt"`
		TemperatureUnit     string `gorm:"column:temperature_unit"`
		TrackCervicalMucus  bool   `gorm:"column:track_cervical_mucus"`
		HideSexChip         bool   `gorm:"column:hide_sex_chip"`
		HideCycleFactors    bool   `gorm:"column:hide_cycle_factors"`
		HideNotesField      bool   `gorm:"column:hide_notes_field"`
	}
	if err := database.
		Table("users").
		Select(
			"email",
			"display_name",
			"local_auth_enabled",
			"auth_session_version",
			"onboarding_completed",
			"cycle_length",
			"period_length",
			"luteal_phase",
			"auto_period_fill",
			"irregular_cycle",
			"track_bbt",
			"temperature_unit",
			"track_cervical_mucus",
			"hide_sex_chip",
			"hide_cycle_factors",
			"hide_notes_field",
		).
		Where("email = ?", "legacy@example.com").
		First(&migratedUser).Error; err != nil {
		t.Fatalf("load migrated legacy user: %v", err)
	}

	assertStringDefault(t, "display_name", migratedUser.DisplayName, "")
	assertBoolDefault(t, "local_auth_enabled", migratedUser.LocalAuthEnabled, true)
	assertIntDefault(t, "auth_session_version", migratedUser.AuthSessionVersion, 1)
	assertBoolDefault(t, "onboarding_completed", migratedUser.OnboardingCompleted, false)
	assertIntDefault(t, "cycle_length", migratedUser.CycleLength, 28)
	assertIntDefault(t, "period_length", migratedUser.PeriodLength, 5)
	assertIntDefault(t, "luteal_phase", migratedUser.LutealPhase, 14)
	assertBoolDefault(t, "auto_period_fill", migratedUser.AutoPeriodFill, true)
	assertBoolDefault(t, "irregular_cycle", migratedUser.IrregularCycle, false)
	assertBoolDefault(t, "track_bbt", migratedUser.TrackBBT, false)
	assertStringDefault(t, "temperature_unit", migratedUser.TemperatureUnit, "c")
	assertBoolDefault(t, "track_cervical_mucus", migratedUser.TrackCervicalMucus, false)
	assertBoolDefault(t, "hide_sex_chip", migratedUser.HideSexChip, false)
	assertBoolDefault(t, "hide_cycle_factors", migratedUser.HideCycleFactors, false)
	assertBoolDefault(t, "hide_notes_field", migratedUser.HideNotesField, false)
}

func assertStringDefault(t *testing.T, field string, got string, want string) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %q, got %q", field, want, got)
	}
}

func assertIntDefault(t *testing.T, field string, got int, want int) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %d, got %d", field, want, got)
	}
}

func assertBoolDefault(t *testing.T, field string, got bool, want bool) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %t, got %t", field, want, got)
	}
}

func assertMigratedLegacyDailyLogDefaults(t *testing.T, database *gorm.DB) {
	t.Helper()

	var migratedLog struct {
		CycleStart      bool    `gorm:"column:cycle_start"`
		IsUncertain     bool    `gorm:"column:is_uncertain"`
		Flow            string  `gorm:"column:flow"`
		Mood            int     `gorm:"column:mood"`
		SexActivity     string  `gorm:"column:sex_activity"`
		BBT             float64 `gorm:"column:bbt"`
		CervicalMucus   string  `gorm:"column:cervical_mucus"`
		CycleFactorKeys string  `gorm:"column:cycle_factor_keys"`
		SymptomIDs      *string `gorm:"column:symptom_ids"`
		Notes           string  `gorm:"column:notes"`
	}
	if err := database.
		Table("daily_logs").
		Select("cycle_start", "is_uncertain", "flow", "mood", "sex_activity", "bbt", "cervical_mucus", "cycle_factor_keys", "symptom_ids", "notes").
		Where("notes = ?", "legacy-log").
		First(&migratedLog).Error; err != nil {
		t.Fatalf("load migrated legacy daily log: %v", err)
	}

	if migratedLog.CycleStart {
		t.Fatal("expected migrated cycle_start default to be false")
	}
	if migratedLog.IsUncertain {
		t.Fatal("expected migrated is_uncertain default to be false")
	}
	if migratedLog.Flow != "light" {
		t.Fatalf("expected migrated flow=light, got %q", migratedLog.Flow)
	}
	if migratedLog.Mood != 0 {
		t.Fatalf("expected migrated mood default to be 0, got %d", migratedLog.Mood)
	}
	if migratedLog.SexActivity != models.SexActivityNone {
		t.Fatalf("expected migrated sex_activity default to be %q, got %q", models.SexActivityNone, migratedLog.SexActivity)
	}
	if migratedLog.BBT != 0 {
		t.Fatalf("expected migrated bbt default to be 0, got %v", migratedLog.BBT)
	}
	if migratedLog.CervicalMucus != models.CervicalMucusNone {
		t.Fatalf("expected migrated cervical_mucus default to be %q, got %q", models.CervicalMucusNone, migratedLog.CervicalMucus)
	}
	if migratedLog.CycleFactorKeys != "[]" {
		t.Fatalf("expected migrated cycle_factor_keys default to be [], got %q", migratedLog.CycleFactorKeys)
	}
	if migratedLog.SymptomIDs == nil || strings.TrimSpace(*migratedLog.SymptomIDs) != "[1,2]" {
		t.Fatalf("expected migrated symptom_ids to remain [1,2], got %v", migratedLog.SymptomIDs)
	}
}

func assertMigratedLegacyDailyLogDateCanonicalized(t *testing.T, database *gorm.DB) {
	t.Helper()

	expected := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	assertStoredDateEqualsUTCMidnight(t, database, `SELECT date FROM daily_logs WHERE notes = ?`, "legacy-log", expected)
}

func assertOIDCLogoutStateSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasTable("oidc_logout_states") {
		t.Fatal("expected oidc_logout_states table to exist after migrations")
	}
	if !database.Migrator().HasColumn("oidc_logout_states", "session_id") {
		t.Fatal("expected oidc_logout_states.session_id column to exist after migrations")
	}
	if !database.Migrator().HasColumn("oidc_logout_states", "expires_at") {
		t.Fatal("expected oidc_logout_states.expires_at column to exist after migrations")
	}
}

func TestOpenSQLiteMigrationBootstrapIsIdempotent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-idempotent.db")

	firstOpen, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("first open sqlite: %v", err)
	}
	firstRecords := loadMigrationRecords(t, firstOpen)

	firstSQLDB, err := firstOpen.DB()
	if err != nil {
		t.Fatalf("first open sql db: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first sql db: %v", err)
	}

	secondOpen := openSQLiteForMigrationBootstrapTest(t, databasePath)
	secondRecords := loadMigrationRecords(t, secondOpen)

	if !reflect.DeepEqual(firstRecords, secondRecords) {
		t.Fatalf("expected migration records to remain unchanged between boots, before=%v after=%v", firstRecords, secondRecords)
	}
}

func openSQLiteForMigrationBootstrapTest(t *testing.T, databasePath string) *gorm.DB {
	t.Helper()

	database, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return database
}

func seedLegacyInitSchema(t *testing.T, databasePath string) {
	t.Helper()

	dsn := fmt.Sprintf("%s?_foreign_keys=on&_busy_timeout=5000", databasePath)
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}

	initSQL, err := fs.ReadFile(embeddedmigrations.Files, "001_init.sql")
	if err != nil {
		t.Fatalf("read 001 migration: %v", err)
	}
	if err := database.Exec(string(initSQL)).Error; err != nil {
		t.Fatalf("apply 001 migration: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		"legacy@example.com",
		"legacy-hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	var legacyUser struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, "legacy@example.com").Scan(&legacyUser).Error; err != nil {
		t.Fatalf("load legacy user id: %v", err)
	}
	if legacyUser.ID == 0 {
		t.Fatal("expected non-zero legacy user id")
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		legacyUser.ID,
		"2026-01-10",
		true,
		"light",
		"[1,2]",
		"legacy-log",
	).Error; err != nil {
		t.Fatalf("insert legacy daily log: %v", err)
	}

	if database.Migrator().HasTable("schema_migrations") {
		t.Fatal("expected legacy schema to not have schema_migrations table")
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open legacy sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy sql db: %v", err)
	}
}

func assertUsersSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	expectedColumns := []string{
		"display_name",
		"onboarding_completed",
		"cycle_length",
		"period_length",
		"luteal_phase",
		"auto_period_fill",
		"auth_session_version",
		"local_auth_enabled",
		"irregular_cycle",
		"track_bbt",
		"temperature_unit",
		"track_cervical_mucus",
		"hide_sex_chip",
		"hide_cycle_factors",
		"hide_notes_field",
		"last_period_start",
	}

	for _, column := range expectedColumns {
		if !database.Migrator().HasColumn("users", column) {
			t.Fatalf("expected users.%s column to exist after migrations", column)
		}
	}
}

func assertSymptomTypesSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasColumn("symptom_types", "archived_at") {
		t.Fatal("expected symptom_types.archived_at column to exist after migrations")
	}
}

func assertDailyLogsSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	columns := loadTableColumns(t, database, "daily_logs")
	if _, exists := columns["mood"]; !exists {
		t.Fatal("expected daily_logs.mood column to exist after migrations")
	}
	if _, exists := columns["sex_activity"]; !exists {
		t.Fatal("expected daily_logs.sex_activity column to exist after migrations")
	}
	if _, exists := columns["bbt"]; !exists {
		t.Fatal("expected daily_logs.bbt column to exist after migrations")
	}
	if _, exists := columns["cervical_mucus"]; !exists {
		t.Fatal("expected daily_logs.cervical_mucus column to exist after migrations")
	}
	if _, exists := columns["symptom_ids"]; !exists {
		t.Fatal("expected daily_logs.symptom_ids column to exist after migrations")
	}
	if _, exists := columns["cycle_factor_keys"]; !exists {
		t.Fatal("expected daily_logs.cycle_factor_keys column to exist after migrations")
	}
	if _, exists := columns["cycle_start"]; !exists {
		t.Fatal("expected daily_logs.cycle_start column to exist after migrations")
	}
	if _, exists := columns["is_uncertain"]; !exists {
		t.Fatal("expected daily_logs.is_uncertain column to exist after migrations")
	}

	notNullFlags := loadTableColumnNotNullFlags(t, database, "daily_logs")
	if notNullFlags["symptom_ids"] {
		t.Fatal("expected daily_logs.symptom_ids to remain nullable")
	}
	if !notNullFlags["cycle_factor_keys"] {
		t.Fatal("expected daily_logs.cycle_factor_keys to be not null")
	}

	tableDefinition := loadSQLiteObjectSQL(t, database, "table", "daily_logs")
	normalized := strings.ToLower(strings.Join(strings.Fields(tableDefinition), ""))
	if strings.Contains(normalized, "check(flowin(") {
		t.Fatalf("expected daily_logs flow CHECK constraint to be removed, got %q", tableDefinition)
	}
}

func assertNormalizedEmailIndexExists(t *testing.T, database *gorm.DB) {
	t.Helper()

	indexSQL := loadSQLiteObjectSQL(t, database, "index", "idx_users_email_normalized")
	definition := strings.ToLower(strings.Join(strings.Fields(indexSQL), ""))
	if definition == "" {
		t.Fatal("expected normalized email index definition to exist")
	}
	if !strings.Contains(definition, "lower(trim(email))") {
		t.Fatalf("expected normalized email index to use lower(trim(email)), got %q", indexSQL)
	}
}

func assertAllEmbeddedMigrationsApplied(t *testing.T, database *gorm.DB) {
	t.Helper()

	expectedVersions := embeddedMigrationVersionsForTest(t)
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

	if !reflect.DeepEqual(expectedVersions, actualVersions) {
		t.Fatalf("unexpected applied migration versions: expected=%v actual=%v", expectedVersions, actualVersions)
	}
}

type migrationRecord struct {
	Version   string `gorm:"column:version"`
	Name      string `gorm:"column:name"`
	AppliedAt string `gorm:"column:applied_at"`
}

func loadMigrationRecords(t *testing.T, database *gorm.DB) []migrationRecord {
	t.Helper()

	records := make([]migrationRecord, 0)
	if err := database.Raw(
		`SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC`,
	).Scan(&records).Error; err != nil {
		t.Fatalf("load migration records: %v", err)
	}
	return records
}

func loadTableColumns(t *testing.T, database *gorm.DB, tableName string) map[string]struct{} {
	t.Helper()

	escapedTable := strings.ReplaceAll(tableName, `"`, `""`)
	query := fmt.Sprintf(`PRAGMA table_info("%s")`, escapedTable)

	var rows []struct {
		Name string `gorm:"column:name"`
	}
	if err := database.Raw(query).Scan(&rows).Error; err != nil {
		t.Fatalf("load table columns for %s: %v", tableName, err)
	}

	columns := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		columns[strings.ToLower(strings.TrimSpace(row.Name))] = struct{}{}
	}
	return columns
}

func loadTableColumnNotNullFlags(t *testing.T, database *gorm.DB, tableName string) map[string]bool {
	t.Helper()

	escapedTable := strings.ReplaceAll(tableName, `"`, `""`)
	query := fmt.Sprintf(`PRAGMA table_info("%s")`, escapedTable)

	var rows []struct {
		Name    string `gorm:"column:name"`
		NotNull int    `gorm:"column:notnull"`
	}
	if err := database.Raw(query).Scan(&rows).Error; err != nil {
		t.Fatalf("load table nullability for %s: %v", tableName, err)
	}

	flags := make(map[string]bool, len(rows))
	for _, row := range rows {
		flags[strings.ToLower(strings.TrimSpace(row.Name))] = row.NotNull == 1
	}
	return flags
}

func loadSQLiteObjectSQL(t *testing.T, database *gorm.DB, objectType string, objectName string) string {
	t.Helper()

	var row struct {
		SQL string `gorm:"column:sql"`
	}
	if err := database.Raw(
		`SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`,
		objectType,
		objectName,
	).Scan(&row).Error; err != nil {
		t.Fatalf("load sqlite master sql for %s %s: %v", objectType, objectName, err)
	}
	return row.SQL
}

func embeddedMigrationVersionsForTest(t *testing.T) []string {
	return embeddedMigrationVersionsForDriverTest(t, DriverSQLite)
}

func embeddedMigrationVersionsForDriverTest(t *testing.T, driver Driver) []string {
	t.Helper()

	migrations, err := loadEmbeddedMigrations(driver)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}

	versions := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		versions = append(versions, migration.Version)
	}
	return versions
}
