package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// Guard for the GDPR Art. 25 row of SECURITY.md's cross-reference table:
// "auto-period-fill off by default". Auto-fill writes period days the owner
// never logged, so the claim is about how much inferred health data an account
// holds before anyone configures anything.
//
// The claim used to be false at every producer at once — the gorm column
// default, both account constructors, both dialect migrations and the
// clear-data reset all said ON — and the tests the row cited were the
// owner-only middleware and audit-flag tests, which cannot observe it. These
// two tests are what the row cites now, and between them they cover the legs
// that decide what a fresh account carries:
//
//   - what a new account gets, read back off the MIGRATED schema of each
//     supported engine rather than off the struct;
//   - that the engine's column DEFAULT is never what decides it, which is the
//     one leg that cannot be read off either dialect's schema (see
//     migrations/035_users_auto_period_fill_default_off.sql: SQLite cannot
//     restate a DEFAULT without rebuilding the account table, so its 002-era
//     literal still says 1 and is inert only because every INSERT names the
//     column);
//   - and that clear-data resets it OFF instead of re-arming it, which is the
//     containment-across-transitions half of the same claim.
//
// The account constructors are the third producer; they are pinned one layer up
// in internal/services (TestOwnerAccountConstructorsLeaveAutoPeriodFillOff),
// where they live.

// TestNewAccountsCarryAutoPeriodFillOff creates an account the way registration
// does — the caller sets no value for the setting — and reads back what the
// migrated schema stored, on every supported engine.
func TestNewAccountsCarryAutoPeriodFillOff(t *testing.T) {
	if models.DefaultAutoPeriodFill {
		t.Fatal("models.DefaultAutoPeriodFill is on: SECURITY.md's Art. 25 row states auto-period-fill as off by default, so either the constant or the claim has to move")
	}

	t.Run("sqlite", func(t *testing.T) {
		database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "auto-period-fill.db"))
		assertNewAccountCarriesAutoPeriodFillOff(t, database, "sqlite-default@example.com")
	})

	t.Run("postgres", func(t *testing.T) {
		database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		assertNewAccountCarriesAutoPeriodFillOff(t, database, "postgres-default@example.com")

		// Only Postgres can restate the column DEFAULT in place, so only there
		// is the schema literal itself worth asserting — migration 035 is the
		// statement that moves it.
		var stored *string
		if err := database.Raw(
			`SELECT column_default FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'auto_period_fill'`,
		).Row().Scan(&stored); err != nil {
			t.Fatalf("read the postgres column default: %v", err)
		}
		if stored == nil || strings.TrimSpace(*stored) != "false" {
			got := "NULL"
			if stored != nil {
				got = *stored
			}
			t.Fatalf("expected users.auto_period_fill to default to false on postgres, got %s", got)
		}
	})
}

func assertNewAccountCarriesAutoPeriodFillOff(t *testing.T, database *gorm.DB, email string) {
	t.Helper()

	repo := NewUserRepository(database)
	user := models.User{
		Email:            email,
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		LocalAuthEnabled: true,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), &user); err != nil {
		t.Fatalf("create account: %v", err)
	}

	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if stored.AutoPeriodFill {
		t.Fatal("a new account was stored with auto-period-fill ON: it would start writing inferred period days nobody asked for, against SECURITY.md's Art. 25 row")
	}

	// The value above must come from the caller, never from the engine. A gorm
	// tag whose default is a truthy literal REPLACES a false field on create,
	// so this is not a theoretical distinction: `default:true` silently turned
	// an explicit false back into true, and it is why one engine's stale
	// DEFAULT literal can be left inert instead of rebuilt.
	statement := database.Session(&gorm.Session{DryRun: true, NewDB: true}).Create(&models.User{
		Email:        "dry-run-" + email,
		PasswordHash: "hash",
		Role:         models.RoleOwner,
		CreatedAt:    time.Now().UTC(),
	}).Statement
	if !strings.Contains(statement.SQL.String(), "auto_period_fill") {
		t.Fatalf("the INSERT gorm builds omits auto_period_fill, so the engine's column DEFAULT decides what a new account carries: %s", statement.SQL.String())
	}
}

// TestAutoPeriodFillColumnDefaultDivergesAcrossEngines pins the trap migration
// 035 accepted, so that a change which starts depending on it fails here rather
// than on an owner's data.
//
// Postgres restates the column DEFAULT in one statement; SQLite has no ALTER
// COLUMN, and `users` is the parent of every foreign key in the schema while
// the runner applies each migration inside a transaction where PRAGMA
// foreign_keys cannot be toggled — so rebuilding the account table to move one
// literal was refused, and SQLite still carries the 002-era DEFAULT 1.
//
// Everything that makes that harmless rests on ONE premise: no insert path
// omits the column. This test states the consequence if that premise ever
// breaks — an account created by a raw INSERT, a second model, or a
// Select/Omit narrowing the column set gets auto-fill ON on SQLite and OFF on
// Postgres, from the same code. The cross-dialect parity test compares type
// families, not defaults, so it cannot see this; TestNewAccountsCarryAutoPeriodFillOff
// asserts the premise, and this test says what it is worth.
//
// A red run here means the divergence moved. If SQLite reports false, the
// account table was rebuilt after all and this test goes away with the comment
// above it; if Postgres reports true, migration 035 was undone.
func TestAutoPeriodFillColumnDefaultDivergesAcrossEngines(t *testing.T) {
	t.Run("sqlite stores the 002-era default", func(t *testing.T) {
		database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "auto-period-fill-omitted.db"))
		if stored := insertUserOmittingAutoPeriodFill(t, database, "sqlite-omitted@example.com"); !stored {
			t.Fatal("SQLite stored auto_period_fill=false for an INSERT that omitted the column: the divergence this test documents is gone, and the migration comment plus this test should go with it")
		}
	})

	t.Run("postgres stores the migrated default", func(t *testing.T) {
		database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
		if stored := insertUserOmittingAutoPeriodFill(t, database, "postgres-omitted@example.com"); stored {
			t.Fatal("Postgres stored auto_period_fill=true for an INSERT that omitted the column: migration 035 no longer moves the column DEFAULT")
		}
	})
}

// insertUserOmittingAutoPeriodFill creates an account the way nothing in this
// application does — naming only the columns without a default — and returns
// what the engine put in auto_period_fill.
func insertUserOmittingAutoPeriodFill(t *testing.T, database *gorm.DB, email string) bool {
	t.Helper()

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		email, "hash", models.RoleOwner, time.Now().UTC(),
	).Error; err != nil {
		t.Fatalf("insert an account without naming auto_period_fill: %v", err)
	}

	var stored bool
	if err := database.Raw(`SELECT auto_period_fill FROM users WHERE email = ?`, email).Row().Scan(&stored); err != nil {
		t.Fatalf("read back auto_period_fill: %v", err)
	}
	return stored
}

// TestClearAllDataTurnsAutoPeriodFillOff is the containment half. Erasure that
// re-arms the generator of inferred period days answers "wipe my records" by
// restarting part of what produced them, which the security constitution's
// "containment survives state transitions" rule calls a defect rather than a
// caveat. The account here has auto-fill ON, as an owner who chose it would.
//
// The cycle-length assertion is the positive anchor: "auto-fill is off" alone
// would pass just as well against a clear-data that reset nothing, since the
// account could have started that way.
func TestClearAllDataTurnsAutoPeriodFillOff(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-auto-period-fill@example.com")

	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"auto_period_fill": true,
		"cycle_length":     41,
	}).Error; err != nil {
		t.Fatalf("seed the owner's settings: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.AutoPeriodFill {
		t.Fatal("clear-data left auto-period-fill armed: the wiped account resumes writing inferred period days")
	}
	if got.CycleLength != models.DefaultCycleLength {
		t.Fatalf("expected the same call to reset cycle_length to %d, got %d", models.DefaultCycleLength, got.CycleLength)
	}
}
