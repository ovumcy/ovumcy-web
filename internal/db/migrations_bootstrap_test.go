package db

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ovumcy/ovumcy-web/internal/models"
	embeddedmigrations "github.com/ovumcy/ovumcy-web/migrations"
	"gorm.io/gorm"
)

// addColumnTokenPattern finds the ADD COLUMN token pair anywhere in a chunk. It
// is deliberately independent of the runner's own anchored detection: the
// structural guard below decides for itself that a statement is an ADD COLUMN
// and then requires the runner to agree.
var addColumnTokenPattern = regexp.MustCompile(`(?i)\bADD\s+COLUMN\b`)

func TestOpenSQLiteAppliesEmbeddedMigrationsOnCleanDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-clean.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertDailyLogsSchemaReconciled(t, database)
	assertOIDCLogoutStateSchemaReconciled(t, database)
	assertAppStateSchema(t, database)
	assertNormalizedEmailIndexExists(t, database)
	assertCalendarFeedSelectorUniqueIndexExists(t, database)
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
	assertCalendarFeedSelectorUniqueIndexExists(t, database)
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

// TestMigration033PurgesUnattributedOIDCLogoutStates locks the data contract of
// migration 033. An oidc_logout_states row written before migration 031 carries
// a NULL user_id, so no "WHERE user_id = ?" delete can ever match it: its
// id_token_hint outlived the erasure of the account it was minted for, until its
// own TTL expired up to 7 days later. The migration removes exactly those rows
// and leaves every attributed row untouched.
//
// The surviving attributed row is the positive anchor — a migration that emptied
// the whole table would satisfy the "NULL rows are gone" half on its own. The
// re-run also proves runner idempotency: the DELETE lands on a database where it
// has already been applied once.
func TestMigration033PurgesUnattributedOIDCLogoutStates(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-033.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		"logout-state-owner@example.com",
		"test-hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert logout-state owner: %v", err)
	}

	var owner struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(
		`SELECT id FROM users WHERE email = ?`, "logout-state-owner@example.com",
	).Scan(&owner).Error; err != nil {
		t.Fatalf("load logout-state owner id: %v", err)
	}
	if owner.ID == 0 {
		t.Fatal("expected non-zero logout-state owner id")
	}

	// Raw SQL on purpose: models.OIDCLogoutState.UserID is a plain uint, so a
	// GORM insert writes 0 where a genuine pre-031 row carries NULL.
	expiresAt := time.Now().Add(time.Hour).UTC()
	for _, row := range []struct {
		sessionID string
		userID    any
	}{
		{sessionID: "pre-031-session", userID: nil},
		{sessionID: "attributed-session", userID: owner.ID},
	} {
		if err := database.Exec(
			`INSERT INTO oidc_logout_states
			   (session_id, user_id, end_session_endpoint, id_token_hint, post_logout_redirect_url, expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			row.sessionID,
			row.userID,
			"https://idp.example.com/logout",
			"id-token-hint",
			"https://app.example.com/",
			expiresAt,
		).Error; err != nil {
			t.Fatalf("insert %s logout state: %v", row.sessionID, err)
		}
	}

	if seeded := countOIDCLogoutStatesWithNullUser(t, database); seeded != 1 {
		t.Fatalf("expected exactly one seeded NULL-user_id logout state, got %d", seeded)
	}

	if err := database.Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, "033",
	).Error; err != nil {
		t.Fatalf("delete migration 033 record: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if remaining := countOIDCLogoutStatesWithNullUser(t, reopened); remaining != 0 {
		t.Fatalf("expected migration 033 to purge every NULL-user_id logout state, %d left", remaining)
	}
	if attributed := countOIDCLogoutStatesBySessionID(t, reopened, "attributed-session"); attributed != 1 {
		t.Fatalf("expected the attributed logout state to survive migration 033, found %d", attributed)
	}
}

// TestEmbeddedSQLiteMigrationAddColumnStatementsAreRecognizedByTheRunner is the
// structural guard behind SQLite migration idempotency. SQLite has no
// `ADD COLUMN IF NOT EXISTS`, so re-running a migration is safe only because the
// runner skips an ADD COLUMN whose column already exists. That skip is driven by
// one anchored pattern over the statement chunks splitSQLStatements produces,
// and those chunks carry the file's prose header, so a detection that cannot see
// past a leading comment silently stops protecting the first ADD COLUMN of every
// migration that opens with one.
//
// Walking the whole embedded set turns that into a structural regression rather
// than a per-file fix: a future migration whose ADD COLUMN sits behind prose is
// caught here instead of at an operator's boot.
func TestEmbeddedSQLiteMigrationAddColumnStatementsAreRecognizedByTheRunner(t *testing.T) {
	migrations, err := loadEmbeddedMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}

	inspected := 0
	for _, migration := range migrations {
		for index, statement := range splitSQLStatements(migration.SQL) {
			if !statementCarriesAddColumnCode(statement) {
				continue
			}
			inspected++

			tableName, columnName, recognized := parseAddColumnStatement(statement)
			if !recognized {
				t.Errorf(
					"%s statement %d carries an ADD COLUMN the runner does not recognize, so the already-exists skip never applies to it and a re-run fails on a duplicate column: %q",
					migration.Name, index, firstStatementLineForTest(statement),
				)
				continue
			}
			if tableName == "" || columnName == "" {
				t.Errorf(
					"%s statement %d resolved to an empty table/column pair (table=%q column=%q), so the skip would probe the wrong schema object: %q",
					migration.Name, index, tableName, columnName, firstStatementLineForTest(statement),
				)
			}
		}
	}

	// Positive anchor: a walk that matches nothing passes just as well when the
	// detection is dead, so require the set to actually contain ADD COLUMNs.
	if inspected == 0 {
		t.Fatal("expected the SQLite migration set to contain ADD COLUMN statements, found none — this guard would have passed vacuously")
	}
}

// statementCarriesAddColumnCode reports whether a statement chunk contains real
// ADD COLUMN code rather than prose about one. Whole `--` comment lines are
// dropped first, so a header sentence describing the runner's skip does not
// count as a statement.
func statementCarriesAddColumnCode(statement string) bool {
	codeLines := make([]string, 0)
	for _, line := range strings.Split(statement, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		codeLines = append(codeLines, line)
	}
	return addColumnTokenPattern.MatchString(strings.Join(codeLines, "\n"))
}

// firstStatementLineForTest returns the first line of a chunk that is neither
// blank nor a comment, so a failure names the offending SQL instead of dumping a
// forty-line prose header.
func firstStatementLineForTest(statement string) string {
	for _, line := range strings.Split(statement, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return trimmed
	}
	return strings.TrimSpace(statement)
}

// TestMigrationReappliesWhenItsSchemaMigrationsRecordIsMissing covers the boot
// consequence of the guard above. The trigger is narrow but real: a database
// that carries the column while its schema_migrations row is absent — a restore
// from a backup taken before the row was written, or an operator pruning the
// table — makes the runner replay the migration. It must complete instead of
// failing the whole boot on `duplicate column name`.
//
// 032 opens with a prose header, so its ADD COLUMN reaches the detection behind
// a comment. 031's ALTER is bare and is the positive anchor: it exercises the
// same delete-record-and-reopen harness through the path that always matched, so
// a green 032 cannot be credited to a harness that never re-applies anything.
func TestMigrationReappliesWhenItsSchemaMigrationsRecordIsMissing(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		version string
		table   string
		column  string
	}{
		{name: "comment-prefixed ALTER (032)", version: "032", table: "users", column: "calendar_feed_verifier_mac"},
		{name: "bare ALTER (031)", version: "031", table: "oidc_logout_states", column: "user_id"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "ovumcy-reapply-"+testCase.version+".db")
			database := openSQLiteForMigrationBootstrapTest(t, databasePath)

			if !database.Migrator().HasColumn(testCase.table, testCase.column) {
				t.Fatalf("expected %s.%s to exist after a clean boot", testCase.table, testCase.column)
			}

			if err := database.Exec(
				`DELETE FROM schema_migrations WHERE version = ?`, testCase.version,
			).Error; err != nil {
				t.Fatalf("delete migration %s record: %v", testCase.version, err)
			}

			sqlDB, err := database.DB()
			if err != nil {
				t.Fatalf("get sql db handle: %v", err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatalf("close sql db: %v", err)
			}

			reopened, err := OpenSQLite(databasePath)
			if err != nil {
				t.Fatalf(
					"expected migration %s to re-apply cleanly with %s.%s already present, but boot failed — the runner did not recognize its ADD COLUMN as skippable: %v",
					testCase.version, testCase.table, testCase.column, err,
				)
			}
			reopenedSQLDB, err := reopened.DB()
			if err != nil {
				t.Fatalf("get reopened sql db handle: %v", err)
			}
			t.Cleanup(func() { _ = reopenedSQLDB.Close() })

			if !reopened.Migrator().HasColumn(testCase.table, testCase.column) {
				t.Fatalf("expected %s.%s to survive the re-applied migration %s", testCase.table, testCase.column, testCase.version)
			}
			assertAllEmbeddedMigrationsApplied(t, reopened)
		})
	}
}

// TestParseAddColumnStatementSeesPastLeadingComments pins the detection
// semantics directly: leading prose is skipped, comment text is never parsed as
// SQL, and anything that is not an ADD COLUMN stays unrecognized (and therefore
// executes untouched).
func TestParseAddColumnStatementSeesPastLeadingComments(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statement  string
		wantTable  string
		wantColumn string
		wantOK     bool
	}{
		{
			name:       "bare ALTER",
			statement:  "ALTER TABLE users ADD COLUMN week_starts_on TEXT NOT NULL DEFAULT 'sunday'",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:       "ALTER behind a prose header",
			statement:  "-- Week-start display preference.\n--\n-- Mentions ADD COLUMN in prose.\n\nALTER TABLE users ADD COLUMN week_starts_on TEXT",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:       "quoted identifiers normalize",
			statement:  "-- header\n\nALTER TABLE \"users\" ADD COLUMN \"week_starts_on\" TEXT",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:      "comment naming a statement is not one",
			statement: "-- ALTER TABLE users ADD COLUMN never_created TEXT\n-- second line of prose\n",
			wantOK:    false,
		},
		{
			name:      "comment-only chunk without a trailing newline",
			statement: "-- a split inside prose leaves a comment fragment behind",
			wantOK:    false,
		},
		{
			name:      "non-ALTER statement stays unrecognized",
			statement: "-- header\n\nCREATE UNIQUE INDEX IF NOT EXISTS idx_users_calendar_feed_selector ON users (calendar_feed_selector)",
			wantOK:    false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tableName, columnName, ok := parseAddColumnStatement(testCase.statement)
			if ok != testCase.wantOK {
				t.Fatalf("parseAddColumnStatement() recognized=%t, want %t", ok, testCase.wantOK)
			}
			if tableName != testCase.wantTable || columnName != testCase.wantColumn {
				t.Fatalf("parseAddColumnStatement() = (%q, %q), want (%q, %q)", tableName, columnName, testCase.wantTable, testCase.wantColumn)
			}
		})
	}
}

// TestEmbeddedMigrationSetsMatchAcrossDialects keeps the two dialect trees
// aligned. A migration added to one engine and forgotten in the other is
// otherwise invisible on a machine without the Postgres container: the SQLite
// bootstrap stays green, and the divergence only surfaces where the missing
// version is needed.
func TestEmbeddedMigrationSetsMatchAcrossDialects(t *testing.T) {
	sqliteSet := embeddedMigrationIdentitiesForTest(t, DriverSQLite)
	postgresSet := embeddedMigrationIdentitiesForTest(t, DriverPostgres)

	if len(sqliteSet) == 0 {
		t.Fatal("expected the SQLite migration set to be non-empty")
	}
	if !reflect.DeepEqual(sqliteSet, postgresSet) {
		t.Fatalf("migration sets diverge across engines: sqlite=%v postgres=%v", sqliteSet, postgresSet)
	}
}

func embeddedMigrationIdentitiesForTest(t *testing.T, driver Driver) []string {
	t.Helper()

	migrations, err := loadEmbeddedMigrations(driver)
	if err != nil {
		t.Fatalf("load embedded %s migrations: %v", driver, err)
	}

	identities := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		identities = append(identities, migration.Version+"/"+migration.Name)
	}
	return identities
}

func countOIDCLogoutStatesWithNullUser(t *testing.T, database *gorm.DB) int64 {
	t.Helper()

	var count int64
	if err := database.Raw(
		`SELECT COUNT(*) FROM oidc_logout_states WHERE user_id IS NULL`,
	).Row().Scan(&count); err != nil {
		t.Fatalf("count NULL-user_id logout states: %v", err)
	}
	return count
}

func countOIDCLogoutStatesBySessionID(t *testing.T, database *gorm.DB, sessionID string) int64 {
	t.Helper()

	var count int64
	if err := database.Raw(
		`SELECT COUNT(*) FROM oidc_logout_states WHERE session_id = ?`, sessionID,
	).Row().Scan(&count); err != nil {
		t.Fatalf("count logout states for session %s: %v", sessionID, err)
	}
	return count
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
		Email                string `gorm:"column:email"`
		DisplayName          string `gorm:"column:display_name"`
		LocalAuthEnabled     bool   `gorm:"column:local_auth_enabled"`
		AuthSessionVersion   int    `gorm:"column:auth_session_version"`
		OnboardingCompleted  bool   `gorm:"column:onboarding_completed"`
		CycleLength          int    `gorm:"column:cycle_length"`
		PeriodLength         int    `gorm:"column:period_length"`
		LutealPhase          int    `gorm:"column:luteal_phase"`
		AutoPeriodFill       bool   `gorm:"column:auto_period_fill"`
		IrregularCycle       bool   `gorm:"column:irregular_cycle"`
		TrackBBT             bool   `gorm:"column:track_bbt"`
		TemperatureUnit      string `gorm:"column:temperature_unit"`
		TrackCervicalMucus   bool   `gorm:"column:track_cervical_mucus"`
		HideSexChip          bool   `gorm:"column:hide_sex_chip"`
		HideCycleFactors     bool   `gorm:"column:hide_cycle_factors"`
		HideNotesField       bool   `gorm:"column:hide_notes_field"`
		ShowHistoricalPhases bool   `gorm:"column:show_historical_phases"`
		WebhookEnabled       bool   `gorm:"column:webhook_enabled"`
		WebhookNotifyPeriod  bool   `gorm:"column:webhook_notify_period"`
		WebhookNotifyOvul    bool   `gorm:"column:webhook_notify_ovulation"`
		ReminderLeadDays     int    `gorm:"column:reminder_lead_days"`
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
			"show_historical_phases",
			"webhook_enabled",
			"webhook_notify_period",
			"webhook_notify_ovulation",
			"reminder_lead_days",
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
	assertBoolDefault(t, "show_historical_phases", migratedUser.ShowHistoricalPhases, false)
	// Webhook notification columns (migration 027) backfill NOT NULL defaults
	// onto the legacy row: delivery off, both per-kind opt-ins on, lead window 3.
	assertBoolDefault(t, "webhook_enabled", migratedUser.WebhookEnabled, false)
	assertBoolDefault(t, "webhook_notify_period", migratedUser.WebhookNotifyPeriod, true)
	assertBoolDefault(t, "webhook_notify_ovulation", migratedUser.WebhookNotifyOvul, true)
	assertIntDefault(t, "reminder_lead_days", migratedUser.ReminderLeadDays, 3)
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
		CycleStart      bool     `gorm:"column:cycle_start"`
		IsUncertain     bool     `gorm:"column:is_uncertain"`
		Flow            string   `gorm:"column:flow"`
		Mood            int      `gorm:"column:mood"`
		SexActivity     string   `gorm:"column:sex_activity"`
		BBT             *float64 `gorm:"column:bbt"`
		CervicalMucus   string   `gorm:"column:cervical_mucus"`
		PregnancyTest   string   `gorm:"column:pregnancy_test"`
		CycleFactorKeys string   `gorm:"column:cycle_factor_keys"`
		SymptomIDs      *string  `gorm:"column:symptom_ids"`
		Notes           string   `gorm:"column:notes"`
	}
	if err := database.
		Table("daily_logs").
		Select("cycle_start", "is_uncertain", "flow", "mood", "sex_activity", "bbt", "cervical_mucus", "pregnancy_test", "cycle_factor_keys", "symptom_ids", "notes").
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
	// Migration 024 rewrites the legacy `bbt = 0` sentinel to NULL, so a legacy
	// row that never recorded a temperature must migrate to NULL (not measured).
	if migratedLog.BBT != nil {
		t.Fatalf("expected migrated bbt sentinel 0 to become NULL, got %v", *migratedLog.BBT)
	}
	if migratedLog.CervicalMucus != models.CervicalMucusNone {
		t.Fatalf("expected migrated cervical_mucus default to be %q, got %q", models.CervicalMucusNone, migratedLog.CervicalMucus)
	}
	if migratedLog.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("expected migrated pregnancy_test default to be %q, got %q", models.PregnancyTestNone, migratedLog.PregnancyTest)
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
	if !database.Migrator().HasColumn("oidc_logout_states", "user_id") {
		t.Fatal("expected oidc_logout_states.user_id column to exist after migrations (migration 031)")
	}
}

// assertAppStateSchema pins the app_state key/value table created by migration
// 028 (issue #125): the table and its key/value/updated_at columns must exist
// after migrations on this engine.
func assertAppStateSchema(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasTable("app_state") {
		t.Fatal("expected app_state table to exist after migrations")
	}
	for _, column := range []string{"key", "value", "updated_at"} {
		if !database.Migrator().HasColumn("app_state", column) {
			t.Fatalf("expected app_state.%s column to exist after migrations", column)
		}
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
		"show_historical_phases",
		"last_period_start",
		"totp_secret",
		"totp_enabled",
		"totp_last_used_step",
		"timezone",
		// Webhook notification settings (migration 027, issue #124).
		"webhook_enabled",
		"webhook_url",
		"webhook_notify_period",
		"webhook_notify_ovulation",
		"webhook_period_last_sent_cycle_start",
		"webhook_ovulation_last_sent_cycle_start",
		"reminder_lead_days",
		// Calendar (.ics) feed subscription token (migration 029) plus the keyed
		// verifier authenticator that superseded bcrypt on the verify path
		// (migration 032; the bcrypt column stays for pre-032 rows and rollback).
		"calendar_feed_selector",
		"calendar_feed_verifier_hash",
		"calendar_feed_verifier_mac",
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
	if _, exists := columns["pregnancy_test"]; !exists {
		t.Fatal("expected daily_logs.pregnancy_test column to exist after migrations")
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

// assertCalendarFeedSelectorUniqueIndexExists locks migration 029's PARTIAL
// UNIQUE index on users.calendar_feed_selector: the by-selector feed lookup
// relies on it to resolve exactly one row, and cross-owner uniqueness of an armed
// selector is a correctness invariant of the token scheme. The index must be
// partial (predicate on non-empty selector) so every feed-off owner can share the
// empty-string zero value without colliding — a plain unique index would reject
// the second feed-off insert. SQLite-only (reads sqlite_master); the Postgres
// bootstrap proves column presence via the shared assertUsersSchemaReconciled,
// and its clean-boot insert path would fail if the partial predicate were missing.
func assertCalendarFeedSelectorUniqueIndexExists(t *testing.T, database *gorm.DB) {
	t.Helper()

	indexSQL := loadSQLiteObjectSQL(t, database, "index", "idx_users_calendar_feed_selector")
	definition := strings.ToLower(strings.Join(strings.Fields(indexSQL), ""))
	if definition == "" {
		t.Fatal("expected calendar feed selector index definition to exist")
	}
	if !strings.Contains(definition, "uniqueindex") {
		t.Fatalf("expected calendar feed selector index to be UNIQUE, got %q", indexSQL)
	}
	if !strings.Contains(definition, "calendar_feed_selector") {
		t.Fatalf("expected calendar feed selector index to key on calendar_feed_selector, got %q", indexSQL)
	}
	// The partial predicate is what lets multiple feed-off rows coexist. Assert
	// it is present so a future edit cannot silently drop it and reintroduce the
	// empty-string collision.
	if !strings.Contains(definition, "wherecalendar_feed_selector<>''") {
		t.Fatalf("expected calendar feed selector index to be PARTIAL on non-empty selector, got %q", indexSQL)
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
