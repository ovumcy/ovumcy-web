package db

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestMergingDuplicateSymptomsRewritesOnlyTheOwnersOwnDayLogs is the privacy
// half of the repair. Symptom ids are global, so a stale id in ANOTHER
// account's day log looks exactly like one in the owner's, and a rewrite keyed
// on the id alone would edit a row belonging to somebody who was never part of
// this merge.
func TestMergingDuplicateSymptomsRewritesOnlyTheOwnersOwnDayLogs(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-scope.db")

	owner := createDailyLogTestUser(t, database, "merge-owner@example.com")
	stranger := createDailyLogTestUser(t, database, "merge-stranger@example.com")

	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	absorbed := insertRepairSymptom(t, database, owner, "cramps")

	ownerDay := insertRepairDayLog(t, database, owner, "2026-03-01", "["+uintText(absorbed)+"]")
	strangerDay := insertRepairDayLog(t, database, stranger, "2026-03-01", "["+uintText(absorbed)+"]")

	outcome := mergeRepairGroups(t, database, models.SymptomMerge{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
		Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
	})
	if outcome.DailyLogsRewritten != 1 {
		t.Fatalf("expected exactly the owner's day to be rewritten, got %d", outcome.DailyLogsRewritten)
	}

	if got := readRepairSymptomIDs(t, database, ownerDay); got != "["+uintText(survivor)+"]" {
		t.Fatalf("the owner's day must name the survivor, got %s", got)
	}
	if got := readRepairSymptomIDs(t, database, strangerDay); got != "["+uintText(absorbed)+"]" {
		t.Fatalf("another owner's day must be left exactly as it was, got %s", got)
	}
}

// TestMergingDuplicateSymptomsCollapsesADayThatNamedBothRows covers the day the
// duplicate is most visible on: the picker offered two entries for one name and
// the owner ticked both, so the merged day would otherwise name one symptom
// twice.
func TestMergingDuplicateSymptomsCollapsesADayThatNamedBothRows(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-collapse.db")

	owner := createDailyLogTestUser(t, database, "collapse-owner@example.com")
	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	absorbed := insertRepairSymptom(t, database, owner, "cramps")
	other := insertRepairSymptom(t, database, owner, "Headache")

	day := insertRepairDayLog(t, database, owner, "2026-03-02",
		"["+uintText(absorbed)+","+uintText(other)+","+uintText(survivor)+"]")
	// symptom_ids is nullable (migration 003) and a legacy row can still hold
	// NULL; it carries no reference, so the repair must pass over it rather than
	// fail on it or write [] into it.
	untouched := insertRepairDayLogWithoutSymptoms(t, database, owner, "2026-03-04")

	outcome := mergeRepairGroups(t, database, models.SymptomMerge{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
		Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
	})

	// The survivor takes the absorbed row's position, because that is where the
	// owner's own order put the pair, and the later mention of the same symptom
	// is what collapses.
	expected := "[" + uintText(survivor) + "," + uintText(other) + "]"
	if got := readRepairSymptomIDs(t, database, day); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
	if got := readRepairSymptomIDs(t, database, untouched); got != "<null>" {
		t.Fatalf("a day that names no symptom must stay untouched, got %s", got)
	}
	if outcome.DailyLogsRewritten != 1 {
		t.Fatalf("only the day that named the absorbed row is a rewrite, got %d", outcome.DailyLogsRewritten)
	}
}

// TestTheSchemaProbeTellsAnAbsentCatalogueFromAnUnreachableDatabase covers the
// answer that decides where the operator is sent.
//
// gorm's Migrator reports HasTable/HasColumn as a bare bool and drops the
// cause, so a database that stopped answering looks exactly like one with no
// catalogue — and the two verdicts point in opposite directions: check your
// connection setting, versus your connection setting was right and the database
// went away.
func TestTheSchemaProbeTellsAnAbsentCatalogueFromAnUnreachableDatabase(t *testing.T) {
	database := openRepairFixtureDatabase(t, "unreachable.db")

	// The catalogue IS there, so a verdict of "absent" here could only come from
	// the probe failing to notice that nothing can be read at all.
	repository := NewSymptomDuplicateRepository(database)
	if err := repository.RequireSymptomCatalogue(context.Background()); err != nil {
		t.Fatalf("a migrated database must satisfy the precondition, got: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	err = repository.RequireSymptomCatalogue(context.Background())
	if err == nil {
		t.Fatal("a database that cannot be read must not pass the precondition")
	}
	if errors.Is(err, ErrSymptomCatalogueAbsent) || errors.Is(err, ErrSymptomCatalogueTooOld) {
		t.Fatalf("an unreachable database must not be reported as a schema verdict, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot read the schema") {
		t.Fatalf("the refusal must say the database could not be read, got: %v", err)
	}
}

// TestMergingSeveralOfOneOwnersGroupsCountsDaysRatherThanPasses is the shape
// the built-in seeding race actually leaves behind: one account arrives with
// the whole built-in set duplicated, so the plan carries many groups for one
// owner and a single day names several of them.
//
// The count is the operator's evidence that nothing was lost, so it has to be
// the number of days touched. A rewrite pass per group walks the same day once
// per group it names and reports a multiple of the truth.
func TestMergingSeveralOfOneOwnersGroupsCountsDaysRatherThanPasses(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-many-groups.db")

	owner := createDailyLogTestUser(t, database, "many-groups-owner@example.com")
	crampsKept := insertRepairSymptom(t, database, owner, "Cramps")
	crampsGone := insertRepairSymptom(t, database, owner, "cramps")
	acheKept := insertRepairSymptom(t, database, owner, "Headache")
	acheGone := insertRepairSymptom(t, database, owner, "headache")

	day := insertRepairDayLog(t, database, owner, "2026-03-05",
		"["+uintText(crampsGone)+","+uintText(acheGone)+"]")

	outcome := mergeRepairGroups(t, database,
		models.SymptomMerge{
			UserID:   owner,
			Survivor: models.SymptomType{ID: crampsKept, UserID: owner},
			Absorbed: []models.SymptomType{{ID: crampsGone, UserID: owner}},
		},
		models.SymptomMerge{
			UserID:   owner,
			Survivor: models.SymptomType{ID: acheKept, UserID: owner},
			Absorbed: []models.SymptomType{{ID: acheGone, UserID: owner}},
		},
	)

	if outcome.DailyLogsRewritten != 1 {
		t.Fatalf("one day was touched, so the count must be 1, got %d", outcome.DailyLogsRewritten)
	}
	if outcome.GroupsMerged != 2 || outcome.SymptomsRemoved != 2 {
		t.Fatalf("expected 2 groups and 2 removed rows, got %d / %d", outcome.GroupsMerged, outcome.SymptomsRemoved)
	}

	expected := "[" + uintText(crampsKept) + "," + uintText(acheKept) + "]"
	if got := readRepairSymptomIDs(t, database, day); got != expected {
		t.Fatalf("both references must land on their survivors: expected %s, got %s", expected, got)
	}
}

// TestMergingDuplicateSymptomsLeavesADayTheMergeDoesNotName pins the scope of
// the write. The operator consents to a plan naming specific symptom rows, so a
// day outside that plan must come out byte for byte as they left it — including
// a repeated id this repair was never asked about.
func TestMergingDuplicateSymptomsLeavesADayTheMergeDoesNotName(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-out-of-scope.db")

	owner := createDailyLogTestUser(t, database, "out-of-scope-owner@example.com")
	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	absorbed := insertRepairSymptom(t, database, owner, "cramps")
	unrelated := insertRepairSymptom(t, database, owner, "Headache")

	named := insertRepairDayLog(t, database, owner, "2026-03-06", "["+uintText(absorbed)+"]")
	untouched := insertRepairDayLog(t, database, owner, "2026-03-07",
		"["+uintText(unrelated)+","+uintText(unrelated)+"]")

	outcome := mergeRepairGroups(t, database, models.SymptomMerge{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
		Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
	})

	if outcome.DailyLogsRewritten != 1 {
		t.Fatalf("only the day the merge names is a rewrite, got %d", outcome.DailyLogsRewritten)
	}
	if got := readRepairSymptomIDs(t, database, named); got != "["+uintText(survivor)+"]" {
		t.Fatalf("the named day must move onto the survivor, got %s", got)
	}
	stored := "[" + uintText(unrelated) + "," + uintText(unrelated) + "]"
	if got := readRepairSymptomIDs(t, database, untouched); got != stored {
		t.Fatalf("a day outside the plan must be left exactly as it was, expected %s, got %s", stored, got)
	}
}

// TestMergingDuplicateSymptomsRefusesADayItCannotRead is the fail-loud half.
// A column this repair cannot decode holds symptoms somebody recorded, and
// treating it as an empty list would silently erase the very data the merge
// exists to carry — so the whole plan rolls back and both rows stay.
func TestMergingDuplicateSymptomsRefusesADayItCannotRead(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-unreadable.db")

	owner := createDailyLogTestUser(t, database, "unreadable-owner@example.com")
	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	absorbed := insertRepairSymptom(t, database, owner, "cramps")
	day := insertRepairDayLog(t, database, owner, "2026-03-03", `{"cramps":true}`)

	_, err := NewSymptomDuplicateRepository(database).MergeDuplicateSymptoms(context.Background(), []models.SymptomMerge{{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
		Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
	}})
	if err == nil {
		t.Fatal("expected a day log the repair cannot read to refuse the merge")
	}
	if !strings.Contains(err.Error(), "symptom_ids") {
		t.Fatalf("the refusal must name what it could not read, got: %v", err)
	}

	var remaining int64
	if err := database.Raw(`SELECT COUNT(*) FROM symptom_types WHERE user_id = ?`, owner).Scan(&remaining).Error; err != nil {
		t.Fatalf("count symptoms after the refusal: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("a refused merge must remove nothing, found %d rows instead of 2", remaining)
	}
	if got := readRepairSymptomIDs(t, database, day); got != `{"cramps":true}` {
		t.Fatalf("a refused merge must leave the day exactly as it was, got %s", got)
	}
}

// TestListingDuplicateSymptomNameGroupsSeesWhatTheIndexWouldRefuse ties the two
// readings together on one engine: every group reported here is one the unique
// index refuses, and a name that differs by more than case is not a group.
func TestListingDuplicateSymptomNameGroupsSeesWhatTheIndexWouldRefuse(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-groups.db")

	owner := createDailyLogTestUser(t, database, "groups-owner@example.com")
	other := createDailyLogTestUser(t, database, "groups-other@example.com")

	insertRepairSymptom(t, database, owner, "Cramps")
	insertRepairSymptom(t, database, owner, "cramps")
	insertRepairSymptom(t, database, owner, "Headache")
	// The same name under a different account is not a conflict: the index is
	// keyed per owner.
	insertRepairSymptom(t, database, other, "Cramps")

	groups, err := NewSymptomDuplicateRepository(database).ListDuplicateSymptomNameGroups(context.Background())
	if err != nil {
		t.Fatalf("list duplicate groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected exactly one group, got %d: %+v", len(groups), groups)
	}
	if groups[0].UserID != owner || groups[0].ConflictKey != "cramps" {
		t.Fatalf("expected owner %d / \"cramps\", got %d / %q", owner, groups[0].UserID, groups[0].ConflictKey)
	}
	if len(groups[0].Symptoms) != 2 {
		t.Fatalf("expected both rows in the group, got %d", len(groups[0].Symptoms))
	}
	for _, symptom := range groups[0].Symptoms {
		if symptom.UserID != owner {
			t.Fatalf("a group may only carry its own owner's rows, got %d", symptom.UserID)
		}
		if strings.ToLower(symptom.Name) != "cramps" {
			t.Fatalf("unexpected row in the group: %q", symptom.Name)
		}
	}
}

// openRepairFixtureDatabase is the database this repair actually meets: the
// per-owner name index is gone, exactly as it was on the version that wrote the
// duplicates, so the rows below can be written at all.
func openRepairFixtureDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()

	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), name))
	if err := database.Exec(`DROP INDEX IF EXISTS ` + symptomNameUniqueIndexName).Error; err != nil {
		t.Fatalf("drop the per-owner name index for the pre-037 fixture: %v", err)
	}
	return database
}

// TestTheRepairReportsWhatItCouldNotReadInsteadOfGuessing covers the storage
// failures on both halves of the read.
//
// They matter more here than in a request path: this repair runs on a stopped
// instance, where the operator has no logs to compare against and no UI to
// re-try in, so a failure that does not say which read failed leaves them with
// a database they cannot judge.
func TestTheRepairReportsWhatItCouldNotReadInsteadOfGuessing(t *testing.T) {
	t.Run("the catalogue cannot be listed at all", func(t *testing.T) {
		database := openRepairFixtureDatabase(t, "list-fails.db")
		if err := database.Exec(`DROP TABLE symptom_types`).Error; err != nil {
			t.Fatalf("drop the catalogue: %v", err)
		}

		_, err := NewSymptomDuplicateRepository(database).ListDuplicateSymptomNameGroups(context.Background())
		if err == nil || !strings.Contains(err.Error(), "list duplicate symptom name groups") {
			t.Fatalf("expected the outer read to be named, got %v", err)
		}
	})

	// The outer query groups on lower(name) and the inner one also reads
	// archived_at, so a catalogue predating migration 004 is exactly the shape
	// where the first read succeeds and the second cannot.
	t.Run("a group cannot be loaded once it is known to exist", func(t *testing.T) {
		database := openRepairFixtureDatabase(t, "group-load-fails.db")
		owner := createDailyLogTestUser(t, database, "group-load@example.com")
		if err := database.Exec(`DROP TABLE symptom_types`).Error; err != nil {
			t.Fatalf("drop the catalogue: %v", err)
		}
		if err := database.Exec(`
CREATE TABLE symptom_types (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  icon TEXT NOT NULL,
  color TEXT NOT NULL,
  is_builtin BOOLEAN NOT NULL DEFAULT 0
)`).Error; err != nil {
			t.Fatalf("recreate the migration 001 catalogue: %v", err)
		}
		insertRepairSymptom(t, database, owner, "Cramps")
		insertRepairSymptom(t, database, owner, "cramps")

		_, err := NewSymptomDuplicateRepository(database).ListDuplicateSymptomNameGroups(context.Background())
		if err == nil || !strings.Contains(err.Error(), "load duplicate symptom name group") {
			t.Fatalf("expected the group read to be named, got %v", err)
		}
		if !strings.Contains(err.Error(), "cramps") {
			t.Fatalf("the failure must name the group it was reading, got %v", err)
		}
	})
}

// TestAPlanWithNothingToAbsorbChangesNothing pins the two shapes that must cost
// no write: an empty plan, and a plan whose entry absorbs nothing. Both arrive
// from a repeated run, which the runbook tells the operator is safe.
//
// Named for what it verifies and no wider: only the EMPTY plan returns before
// the transaction: a merge with an empty Absorbed enters it and is skipped
// inside, so this says nothing about how many transactions were opened.
func TestAPlanWithNothingToAbsorbChangesNothing(t *testing.T) {
	database := openRepairFixtureDatabase(t, "merge-nothing.db")
	owner := createDailyLogTestUser(t, database, "merge-nothing@example.com")
	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	day := insertRepairDayLog(t, database, owner, "2026-03-08", "["+uintText(survivor)+"]")

	repository := NewSymptomDuplicateRepository(database)

	empty, err := repository.MergeDuplicateSymptoms(context.Background(), nil)
	if err != nil {
		t.Fatalf("an empty plan must succeed, got %v", err)
	}
	if empty != (models.SymptomMergeOutcome{}) {
		t.Fatalf("an empty plan must change nothing, got %+v", empty)
	}

	absorbingNothing, err := repository.MergeDuplicateSymptoms(context.Background(), []models.SymptomMerge{{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
	}})
	if err != nil {
		t.Fatalf("a merge with nothing to absorb must succeed, got %v", err)
	}
	if absorbingNothing != (models.SymptomMergeOutcome{}) {
		t.Fatalf("a merge with nothing to absorb must change nothing, got %+v", absorbingNothing)
	}

	if got := readRepairSymptomIDs(t, database, day); got != "["+uintText(survivor)+"]" {
		t.Fatalf("no day may be rewritten by a plan that absorbs nothing, got %s", got)
	}
}

// TestAMergeNamesTheWriteThatFailedAndKeepsNothing covers the failures inside
// the transaction, which are the ones that decide whether a half-done repair
// can exist. Each case leaves the plan rolled back whole.
func TestAMergeNamesTheWriteThatFailedAndKeepsNothing(t *testing.T) {
	t.Run("the day logs cannot be read", func(t *testing.T) {
		database := openRepairFixtureDatabase(t, "daylogs-unreadable.db")
		owner := createDailyLogTestUser(t, database, "daylogs-unreadable@example.com")
		survivor := insertRepairSymptom(t, database, owner, "Cramps")
		absorbed := insertRepairSymptom(t, database, owner, "cramps")
		if err := database.Exec(`DROP TABLE daily_logs`).Error; err != nil {
			t.Fatalf("drop the day logs: %v", err)
		}

		_, err := NewSymptomDuplicateRepository(database).MergeDuplicateSymptoms(context.Background(), []models.SymptomMerge{{
			UserID:   owner,
			Survivor: models.SymptomType{ID: survivor, UserID: owner},
			Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
		}})
		if err == nil || !strings.Contains(err.Error(), "load day logs for owner") {
			t.Fatalf("expected the day-log read to be named, got %v", err)
		}

		var remaining int64
		if err := database.Raw(`SELECT COUNT(*) FROM symptom_types WHERE user_id = ?`, owner).Scan(&remaining).Error; err != nil {
			t.Fatalf("count symptoms after the failure: %v", err)
		}
		if remaining != 2 {
			t.Fatalf("a failed merge must remove nothing, found %d rows instead of 2", remaining)
		}
	})

	// A view answers a SELECT and refuses an UPDATE, which is the one shape
	// where the read this rewrite depends on succeeds and its paired write does
	// not.
	t.Run("a day log cannot be rewritten", func(t *testing.T) {
		database := openRepairFixtureDatabase(t, "daylog-unwritable.db")
		owner := createDailyLogTestUser(t, database, "daylog-unwritable@example.com")
		survivor := insertRepairSymptom(t, database, owner, "Cramps")
		absorbed := insertRepairSymptom(t, database, owner, "cramps")
		day := insertRepairDayLog(t, database, owner, "2026-03-09", "["+uintText(absorbed)+"]")

		if err := database.Exec(`ALTER TABLE daily_logs RENAME TO daily_logs_stored`).Error; err != nil {
			t.Fatalf("rename the day logs: %v", err)
		}
		if err := database.Exec(`CREATE VIEW daily_logs AS SELECT * FROM daily_logs_stored`).Error; err != nil {
			t.Fatalf("put a read-only view in front of the day logs: %v", err)
		}

		_, err := NewSymptomDuplicateRepository(database).MergeDuplicateSymptoms(context.Background(), []models.SymptomMerge{{
			UserID:   owner,
			Survivor: models.SymptomType{ID: survivor, UserID: owner},
			Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
		}})
		if err == nil || !strings.Contains(err.Error(), "rewrite symptom references of day log") {
			t.Fatalf("expected the day-log write to be named, got %v", err)
		}

		// Two assertions, because each catches what the other cannot. The day —
		// read through the table the view hides — proves no partial write
		// landed. The symptom count proves the failure STOPPED the plan: swallow
		// the rewrite error and the merge walks on into the removal, leaving a
		// day that names a row no longer there, which is the one outcome this
		// merge may never produce.
		var stored struct {
			SymptomIDs *string `gorm:"column:symptom_ids"`
		}
		if err := database.Raw(`SELECT symptom_ids FROM daily_logs_stored WHERE id = ?`, day).Scan(&stored).Error; err != nil {
			t.Fatalf("read the day back: %v", err)
		}
		if stored.SymptomIDs == nil || *stored.SymptomIDs != "["+uintText(absorbed)+"]" {
			t.Fatalf("a failed write must leave the day exactly as it was, got %v", stored.SymptomIDs)
		}

		var remaining int64
		if err := database.Raw(`SELECT COUNT(*) FROM symptom_types WHERE user_id = ?`, owner).Scan(&remaining).Error; err != nil {
			t.Fatalf("count symptoms after the failure: %v", err)
		}
		if remaining != 2 {
			t.Fatalf("a failed rewrite must stop the plan before any removal, found %d rows instead of 2", remaining)
		}
	})

	t.Run("the duplicate row cannot be removed", func(t *testing.T) {
		database := openRepairFixtureDatabase(t, "delete-fails.db")
		owner := createDailyLogTestUser(t, database, "delete-fails@example.com")
		survivor := insertRepairSymptom(t, database, owner, "Cramps")
		absorbed := insertRepairSymptom(t, database, owner, "cramps")
		day := insertRepairDayLog(t, database, owner, "2026-03-10", "["+uintText(absorbed)+"]")
		if err := database.Exec(`DROP TABLE symptom_types`).Error; err != nil {
			t.Fatalf("drop the catalogue: %v", err)
		}

		_, err := NewSymptomDuplicateRepository(database).MergeDuplicateSymptoms(context.Background(), []models.SymptomMerge{{
			UserID:   owner,
			Survivor: models.SymptomType{ID: survivor, UserID: owner},
			Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
		}})
		if err == nil || !strings.Contains(err.Error(), "remove duplicate symptom") {
			t.Fatalf("expected the removal to be named, got %v", err)
		}

		// The rewrite ran before the removal failed, and the rollback is what
		// puts the day back on the row that still exists.
		if got := readRepairSymptomIDs(t, database, day); got != "["+uintText(absorbed)+"]" {
			t.Fatalf("a failed merge must leave the day exactly as it was, got %s", got)
		}
	})
}

// TestADayThatStoresAnEmptySymptomListIsLeftAlone covers the legacy shape the
// column can hold beside NULL: the empty string, which is not a JSON array and
// carries no reference to move.
func TestADayThatStoresAnEmptySymptomListIsLeftAlone(t *testing.T) {
	database := openRepairFixtureDatabase(t, "empty-symptom-list.db")
	owner := createDailyLogTestUser(t, database, "empty-list@example.com")
	survivor := insertRepairSymptom(t, database, owner, "Cramps")
	absorbed := insertRepairSymptom(t, database, owner, "cramps")
	empty := insertRepairDayLog(t, database, owner, "2026-03-11", "")
	// The anchor: a day the merge MUST rewrite, so the count below can tell an
	// empty list that was skipped from a pass that reached no day at all.
	named := insertRepairDayLog(t, database, owner, "2026-03-12", "["+uintText(absorbed)+"]")

	outcome := mergeRepairGroups(t, database, models.SymptomMerge{
		UserID:   owner,
		Survivor: models.SymptomType{ID: survivor, UserID: owner},
		Absorbed: []models.SymptomType{{ID: absorbed, UserID: owner}},
	})
	if outcome.DailyLogsRewritten != 1 {
		t.Fatalf("the pass must reach the named day and skip the empty one, got %d", outcome.DailyLogsRewritten)
	}
	if got := readRepairSymptomIDs(t, database, named); got != "["+uintText(survivor)+"]" {
		t.Fatalf("the named day must move onto the survivor, got %s", got)
	}
	if got := readRepairSymptomIDs(t, database, empty); got != "" {
		t.Fatalf("an empty list must be left exactly as it was, got %q", got)
	}
}

func mergeRepairGroups(t *testing.T, database *gorm.DB, merges ...models.SymptomMerge) models.SymptomMergeOutcome {
	t.Helper()

	outcome, err := NewSymptomDuplicateRepository(database).MergeDuplicateSymptoms(context.Background(), merges)
	if err != nil {
		t.Fatalf("merge duplicate symptoms: %v", err)
	}
	return outcome
}

func insertRepairSymptom(t *testing.T, database *gorm.DB, userID uint, name string) uint {
	t.Helper()

	if err := database.Exec(
		`INSERT INTO symptom_types (user_id, name, icon, color, is_builtin) VALUES (?, ?, 'x', '#FF0000', 0)`,
		userID, name,
	).Error; err != nil {
		t.Fatalf("insert symptom %q: %v", name, err)
	}

	var id uint
	if err := database.Raw(
		`SELECT id FROM symptom_types WHERE user_id = ? AND name = ?`, userID, name,
	).Scan(&id).Error; err != nil {
		t.Fatalf("read back symptom %q: %v", name, err)
	}
	return id
}

func insertRepairDayLog(t *testing.T, database *gorm.DB, userID uint, day string, symptomIDs string) uint {
	t.Helper()

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, symptom_ids, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		userID, day, symptomIDs,
	).Error; err != nil {
		t.Fatalf("insert day log for %s: %v", day, err)
	}

	var id uint
	if err := database.Raw(
		`SELECT id FROM daily_logs WHERE user_id = ? AND date = ?`, userID, day,
	).Scan(&id).Error; err != nil {
		t.Fatalf("read back day log for %s: %v", day, err)
	}
	return id
}

func insertRepairDayLogWithoutSymptoms(t *testing.T, database *gorm.DB, userID uint, day string) uint {
	t.Helper()

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		userID, day,
	).Error; err != nil {
		t.Fatalf("insert day log without symptoms for %s: %v", day, err)
	}

	var id uint
	if err := database.Raw(
		`SELECT id FROM daily_logs WHERE user_id = ? AND date = ?`, userID, day,
	).Scan(&id).Error; err != nil {
		t.Fatalf("read back day log for %s: %v", day, err)
	}
	return id
}

func readRepairSymptomIDs(t *testing.T, database *gorm.DB, dayLogID uint) string {
	t.Helper()

	var stored struct {
		SymptomIDs *string `gorm:"column:symptom_ids"`
	}
	if err := database.Raw(`SELECT symptom_ids FROM daily_logs WHERE id = ?`, dayLogID).Scan(&stored).Error; err != nil {
		t.Fatalf("read symptom references of day log %d: %v", dayLogID, err)
	}
	if stored.SymptomIDs == nil {
		return "<null>"
	}
	return *stored.SymptomIDs
}

func uintText(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
