package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/testdb"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// symptomNameUniqueIndexName is migration 037's index, named here so the
// fixture that removes it and the migration that creates it can only drift
// apart loudly.
const symptomNameUniqueIndexName = "idx_symptom_types_user_name_unique"

// refusedUpgradeFixture is the database an operator actually meets: one that
// held duplicate symptom names before migration 037 existed, so the boot that
// would add the index refuses instead.
type refusedUpgradeFixture struct {
	Config     db.Config
	UserID     uint
	SurvivorID uint
	DuplicateD uint
	DayLogID   uint
}

// TestARefusedSymptomNameUpgradeBootsAfterTheDocumentedRepairOnSQLite is the
// finding's own case, end to end and in the order the runbook gives.
//
// Before the repair existed this could not be written at all: the refusal ends
// the boot, and the message sent the operator to the application, which is the
// thing the refusal has stopped. Every step below therefore runs with the
// instance down.
func TestARefusedSymptomNameUpgradeBootsAfterTheDocumentedRepairOnSQLite(t *testing.T) {
	t.Parallel()

	fixture := seedRefusedSymptomNameUpgrade(t, db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: filepath.Join(t.TempDir(), "refused-upgrade.db"),
	})
	assertTheDocumentedRepairUnblocksTheBoot(t, fixture)
}

// TestARefusedSymptomNameUpgradeBootsAfterTheDocumentedRepairOnPostgres is the
// same case on the other engine.
//
// It is not redundant with the SQLite one. The repair reads its groups through
// the engine's own lower(), which is the expression the two engines disagree
// about — Postgres folds by locale, SQLite folds ASCII only — so "the repair
// finds exactly what the index would refuse" is a claim about each engine
// separately, and a green run on one says nothing about the other.
func TestARefusedSymptomNameUpgradeBootsAfterTheDocumentedRepairOnPostgres(t *testing.T) {
	t.Parallel()

	fixture := seedRefusedSymptomNameUpgrade(t, db.Config{
		Driver:      db.DriverPostgres,
		PostgresURL: testdb.StartPostgresDSN(t, "ovumcy_repair_test"),
	})
	assertTheDocumentedRepairUnblocksTheBoot(t, fixture)
}

func assertTheDocumentedRepairUnblocksTheBoot(t *testing.T, fixture refusedUpgradeFixture) {
	t.Helper()

	// 1. The instance does not start, and says so in terms an operator with a
	//    stopped instance can act on.
	bootErr := bootAndClose(fixture.Config)
	if bootErr == nil {
		t.Fatal("the fixture must reproduce the refused migration: the boot succeeded")
	}
	message := bootErr.Error()
	if !strings.Contains(message, "ovumcy repair") {
		t.Fatalf("the refusal must name the offline repair, got: %s", message)
	}
	if strings.Contains(strings.ToLower(message), "through the application") {
		t.Fatalf("the refusal must not send the operator to an application it has stopped, got: %s", message)
	}

	// 2. The inspection reports the group and changes nothing, exiting non-zero
	//    so an upgrade script can gate on it.
	var inspected bytes.Buffer
	inspectErr := runRepairCommand(fixture.Config, []string{"symptom-names"}, &inspected)
	if inspectErr == nil {
		t.Fatal("an inspection that found duplicates must report a non-zero outcome")
	}
	report := inspected.String()
	for _, fragment := range []string{"keep", "merge into kept", fmt.Sprintf("%d", fixture.SurvivorID), fmt.Sprintf("%d", fixture.DuplicateD)} {
		if !strings.Contains(report, fragment) {
			t.Fatalf("the inspection must name %q, got:\n%s", fragment, report)
		}
	}
	if bootAndClose(fixture.Config) == nil {
		t.Fatal("an inspection must change nothing: the boot succeeded after it")
	}

	// 3. The repair merges the group.
	var applied bytes.Buffer
	if err := runRepairCommand(fixture.Config, []string{"symptom-names", "--apply"}, &applied); err != nil {
		t.Fatalf("the documented repair must succeed, got: %v", err)
	}

	// 4. The full boot now runs every migration, this one included.
	if err := bootAndClose(fixture.Config); err != nil {
		t.Fatalf("the instance must start after the documented repair, got: %v", err)
	}

	// 5. Nothing the owner recorded was dropped on the way: the day that named
	//    both rows names the surviving one, exactly once, and it resolves.
	assertDayLogKeepsItsSymptom(t, fixture)

	// 6. Running it again is a clean no-op, and the instance still starts.
	var repeated bytes.Buffer
	if err := runRepairCommand(fixture.Config, []string{"symptom-names", "--apply"}, &repeated); err != nil {
		t.Fatalf("the repair must be repeatable, got: %v", err)
	}
	if !strings.Contains(repeated.String(), "Nothing to merge") {
		t.Fatalf("a second run must report that it found nothing, got:\n%s", repeated.String())
	}
	if err := bootAndClose(fixture.Config); err != nil {
		t.Fatalf("the instance must still start after a repeated repair, got: %v", err)
	}
}

// TestRepairInspectionIsSilentAndCleanOnADatabaseWithNoDuplicates is the
// anti-vacuity half: the same command on a healthy database must report clean
// and exit zero, or the case above would pass on a command that always fails.
func TestRepairInspectionIsSilentAndCleanOnADatabaseWithNoDuplicates(t *testing.T) {
	t.Parallel()

	config := db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "clean.db")}
	closeDatabase(t, openMigratedDatabase(t, config))

	var output bytes.Buffer
	if err := runRepairCommand(config, []string{"symptom-names"}, &output); err != nil {
		t.Fatalf("a clean database must inspect clean, got: %v", err)
	}
	if !strings.Contains(output.String(), "nothing to refuse") {
		t.Fatalf("expected a clean report, got: %s", output.String())
	}
}

// TestRepairUsageNamesOnlyCommandsAStoppedInstanceCanRun pins the usage text
// itself, because it is what the migration refusal points at: a repair listed
// here that needed a running server would close the loop the finding opened.
func TestRepairUsageNamesOnlyCommandsAStoppedInstanceCanRun(t *testing.T) {
	t.Parallel()

	config := db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "usage.db")}
	for _, args := range [][]string{nil, {"symptom-nmaes"}, {"symptom-names", "--force"}} {
		var output bytes.Buffer
		err := runRepairCommand(config, args, &output)
		if err == nil {
			t.Fatalf("expected usage for args %v", args)
		}
		if !strings.Contains(err.Error(), "symptom-names") || !strings.Contains(err.Error(), "--apply") {
			t.Fatalf("usage must name the repair and its apply flag, got: %v", err)
		}
		if !strings.Contains(err.Error(), "do not need the server") {
			t.Fatalf("usage must say a repair runs without the server, got: %v", err)
		}
	}
}

// TestRepairInspectionNamesWhatEachRowIsBeforeTheOperatorConsents covers the
// custom half of the catalogue, which the built-in fixture above never reaches.
//
// The kind and the state are the two facts that decide the plan — an active row
// is kept over an archived one, a built-in over a custom one — so an operator
// reading the report has to be able to see both for every row, not infer them
// from which line says "keep".
func TestRepairInspectionNamesWhatEachRowIsBeforeTheOperatorConsents(t *testing.T) {
	t.Parallel()

	config := db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "custom-pair.db")}
	database := openMigratedDatabase(t, config)

	user := models.User{
		Email:        "custom-owner@example.com",
		PasswordHash: "hash",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := database.Exec(`DROP INDEX IF EXISTS ` + symptomNameUniqueIndexName).Error; err != nil {
		t.Fatalf("drop the per-owner name index: %v", err)
	}
	archived := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	for _, symptom := range []models.SymptomType{
		{UserID: user.ID, Name: "Jaw ache", Icon: "x", Color: "#FF0000", ArchivedAt: &archived},
		{UserID: user.ID, Name: "jaw ache", Icon: "x", Color: "#FF0000"},
	} {
		if err := database.Create(&symptom).Error; err != nil {
			t.Fatalf("seed %q: %v", symptom.Name, err)
		}
	}
	closeDatabase(t, database)

	var output bytes.Buffer
	if err := runRepairCommand(config, []string{"symptom-names"}, &output); err == nil {
		t.Fatal("expected the inspection to report duplicates")
	}
	report := output.String()
	for _, fragment := range []string{"custom", "archived", "active", "KIND", "STATE"} {
		if !strings.Contains(report, fragment) {
			t.Fatalf("the report must name %q, got:\n%s", fragment, report)
		}
	}
}

// TestRepairReportsAnUnusableDatabaseConfigRatherThanPanicking keeps the one
// prerequisite this subcommand does have diagnosable: it is run from a shell an
// operator assembled by hand, not from the compose environment the server gets.
func TestRepairReportsAnUnusableDatabaseConfigRatherThanPanicking(t *testing.T) {
	t.Parallel()

	err := runRepairCommand(db.Config{Driver: db.DriverSQLite, SQLitePath: ""}, []string{"symptom-names"}, nil)
	if err == nil || !strings.Contains(err.Error(), "database init failed") {
		t.Fatalf("expected a database-init failure, got %v", err)
	}
}

// seedRefusedSymptomNameUpgrade builds the database as an upgrade meets it: the
// duplicate pair was written by the version before the index existed, and the
// day log points at both rows.
//
// It bootstraps at the current schema and then walks 037 back — drop the index,
// forget its ledger row — because that is the only way to reach the pre-037
// state with a binary that carries 037. The rows it then writes are exactly
// what the built-in seeding race left behind: two identical built-in rows for
// one account, differing only in case.
func seedRefusedSymptomNameUpgrade(t *testing.T, config db.Config) refusedUpgradeFixture {
	t.Helper()

	database := openMigratedDatabase(t, config)
	defer closeDatabase(t, database)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := models.User{
		Email:               "repair-owner@example.com",
		PasswordHash:        string(passwordHash),
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	survivor := models.SymptomType{UserID: user.ID, Name: "Cramps", Icon: "🩸", Color: "#FF4444", IsBuiltin: true}
	if err := database.Create(&survivor).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	dayLog := models.DailyLog{
		UserID:     user.ID,
		Date:       time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC),
		SymptomIDs: []uint{survivor.ID},
	}
	if err := database.Create(&dayLog).Error; err != nil {
		t.Fatalf("create day log: %v", err)
	}

	if err := database.Exec(`DROP INDEX IF EXISTS ` + symptomNameUniqueIndexName).Error; err != nil {
		t.Fatalf("drop index for the pre-037 fixture: %v", err)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = '037'`).Error; err != nil {
		t.Fatalf("forget migration 037: %v", err)
	}

	duplicate := models.SymptomType{UserID: user.ID, Name: "cramps", Icon: "🩸", Color: "#FF4444", IsBuiltin: true}
	if err := database.Create(&duplicate).Error; err != nil {
		t.Fatalf("seed the duplicate the index would refuse: %v", err)
	}

	// The owner's day names both rows, which is what the merge has to carry:
	// the picker showed two symptoms a normal request would never have created.
	references, err := json.Marshal([]uint{survivor.ID, duplicate.ID})
	if err != nil {
		t.Fatalf("encode symptom references: %v", err)
	}
	if err := database.Exec(
		`UPDATE daily_logs SET symptom_ids = ? WHERE id = ? AND user_id = ?`,
		string(references), dayLog.ID, user.ID,
	).Error; err != nil {
		t.Fatalf("point the day log at both rows: %v", err)
	}

	return refusedUpgradeFixture{
		Config:     config,
		UserID:     user.ID,
		SurvivorID: survivor.ID,
		DuplicateD: duplicate.ID,
		DayLogID:   dayLog.ID,
	}
}

// assertDayLogKeepsItsSymptom is the half a green boot cannot prove. Dropping
// both symptom rows would also let the migration through, so the boot alone is
// not evidence that the owner's day survived the repair.
func assertDayLogKeepsItsSymptom(t *testing.T, fixture refusedUpgradeFixture) {
	t.Helper()

	database := openMigratedDatabase(t, fixture.Config)
	defer closeDatabase(t, database)

	var stored struct {
		SymptomIDs *string `gorm:"column:symptom_ids"`
	}
	if err := database.Raw(
		`SELECT symptom_ids FROM daily_logs WHERE id = ? AND user_id = ?`,
		fixture.DayLogID, fixture.UserID,
	).Scan(&stored).Error; err != nil {
		t.Fatalf("read the day log back: %v", err)
	}
	if stored.SymptomIDs == nil {
		t.Fatal("the repair must not clear a day's symptom references")
	}

	ids := make([]uint, 0)
	if err := json.Unmarshal([]byte(*stored.SymptomIDs), &ids); err != nil {
		t.Fatalf("decode the day's symptom references %q: %v", *stored.SymptomIDs, err)
	}
	if len(ids) != 1 || ids[0] != fixture.SurvivorID {
		t.Fatalf("expected the day to name symptom %d exactly once, got %v", fixture.SurvivorID, ids)
	}

	var remaining int64
	if err := database.Raw(
		`SELECT COUNT(*) FROM symptom_types WHERE id = ? AND user_id = ?`,
		fixture.SurvivorID, fixture.UserID,
	).Scan(&remaining).Error; err != nil {
		t.Fatalf("count the surviving symptom: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("the day must still resolve to a symptom row, found %d", remaining)
	}
}

func openMigratedDatabase(t *testing.T, config db.Config) *gorm.DB {
	t.Helper()

	database, err := db.OpenDatabase(config)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return database
}

// bootAndClose is the boot this finding is about: OpenDatabase applies every
// pending migration, so its error is the one an operator reads in the startup
// log. The handle is released either way, which on Windows is what keeps a
// failed case from turning into a TempDir cleanup failure.
func bootAndClose(config db.Config) error {
	database, err := db.OpenDatabase(config)
	if err != nil {
		return err
	}
	if sqlDB, handleErr := database.DB(); handleErr == nil {
		_ = sqlDB.Close()
	}
	return nil
}

func closeDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql handle: %v", err)
	}
}
