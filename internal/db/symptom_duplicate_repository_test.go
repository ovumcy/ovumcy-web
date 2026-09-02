package db

import (
	"context"
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
