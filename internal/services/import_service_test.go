package services

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

func newImportServiceIntegration(t *testing.T, database *gorm.DB, symptomService *SymptomService) *ImportService {
	t.Helper()
	repositories := db.NewRepositories(database)
	dailyLogs := repositories.DailyLogs
	txRunner := func(ctx context.Context, fn func(DayLogRepository) error) error {
		return dailyLogs.WithinTransaction(ctx, func(tx *db.DailyLogRepository) error {
			return fn(tx)
		})
	}
	return NewImportService(dailyLogs, repositories.Users, symptomService, txRunner)
}

// symptomIDByName seeds the built-in catalog and returns the owner-scoped ID of
// the symptom whose name matches (built-in or custom).
func symptomIDByName(t *testing.T, symptomService *SymptomService, userID uint, name string) uint {
	t.Helper()
	catalog, err := symptomService.FetchSymptoms(context.Background(), userID)
	if err != nil {
		t.Fatalf("FetchSymptoms: %v", err)
	}
	target := normalizeSymptomNameKey(name)
	for _, symptom := range catalog {
		if normalizeSymptomNameKey(symptom.Name) == target {
			return symptom.ID
		}
	}
	t.Fatalf("symptom %q not found in catalog", name)
	return 0
}

// TestImportServiceRoundTripPreservesEntries is the core fidelity guarantee:
// exporting an owner's data, importing it into a fresh account, and exporting
// that account must yield an identical entry set. This exercises the reverse
// symptom mapping end to end — built-in flags (incl. the mood→"Mood swings"
// alias), the swelling→other_symptoms path, and custom-symptom find-or-create —
// against a real database.
func TestImportServiceRoundTripPreservesEntries(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)

	source := createDayServiceTestUser(t, database, "import-roundtrip-source@example.com")

	// Custom symptom + a few built-ins, referenced by real owner-scoped IDs.
	if _, err := symptomService.CreateSymptomForUser(context.Background(), source.ID, "My custom", "", ""); err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}
	crampsID := symptomIDByName(t, symptomService, source.ID, "Cramps")
	moodSwingsID := symptomIDByName(t, symptomService, source.ID, "Mood swings")
	swellingID := symptomIDByName(t, symptomService, source.ID, "Swelling")
	customID := symptomIDByName(t, symptomService, source.ID, "My custom")

	logs := []models.DailyLog{
		{
			UserID: source.ID, Date: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
			IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowHeavy, Mood: 3,
			SexActivity: models.SexActivityProtected, BBT: models.NewBBT(36.7), CervicalMucus: models.CervicalMucusCreamy,
			PregnancyTest: models.PregnancyTestNegative, CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorTravel},
			SymptomIDs: []uint{crampsID, moodSwingsID, customID}, Notes: "heavy day",
		},
		{
			UserID: source.ID, Date: time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC),
			Mood: 5, BBT: models.NewBBT(36.9), PregnancyTest: models.PregnancyTestPositive,
			SymptomIDs: []uint{swellingID}, CycleFactorKeys: []string{},
		},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create source logs: %v", err)
	}

	sourceEntries, err := exportService.BuildJSONEntries(context.Background(), source.ID, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("export source: %v", err)
	}
	raw, err := json.Marshal(importPayload{Entries: sourceEntries})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	target := createDayServiceTestUser(t, database, "import-roundtrip-target@example.com")
	importService := newImportServiceIntegration(t, database, symptomService)

	result, err := importService.ImportJSON(context.Background(), target.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 2 || result.Skipped != 0 || result.Rejected != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	targetEntries, err := exportService.BuildJSONEntries(context.Background(), target.ID, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("export target: %v", err)
	}
	if !reflect.DeepEqual(sourceEntries, targetEntries) {
		t.Fatalf("round-trip mismatch:\n source=%+v\n target=%+v", sourceEntries, targetEntries)
	}
}

// TestImportServiceSkipsExistingDays proves the additive contract: a day that
// already exists is never overwritten, and its stored data is left intact.
func TestImportServiceSkipsExistingDays(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)

	user := createDayServiceTestUser(t, database, "import-skip@example.com")
	existing := models.DailyLog{
		UserID: user.ID, Date: time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC),
		IsPeriod: true, Flow: models.FlowLight, Notes: "original", CycleFactorKeys: []string{},
	}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatalf("create existing: %v", err)
	}

	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-04-10", Period: true, Flow: models.FlowHeavy, Notes: "incoming", CycleFactors: []string{}},
		{Date: "2026-04-11", Period: false, Notes: "new day", CycleFactors: []string{}},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 || result.Skipped != 1 || result.Rejected != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	stored, err := dayService.FetchLogByDate(context.Background(), user.ID, time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("reload existing: %v", err)
	}
	if stored.Notes != "original" || stored.Flow != models.FlowLight {
		t.Fatalf("existing day was overwritten: %+v", stored)
	}
}

// TestImportServiceDropsCycleStartOnNonPeriodDay ensures a crafted file cannot
// persist an inconsistent anchor (cycle_start without a period day).
func TestImportServiceDropsCycleStartOnNonPeriodDay(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-cyclestart@example.com")

	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-05-02", Period: false, CycleStart: true, IsUncertain: true, CycleFactors: []string{}},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	if _, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC); err != nil {
		t.Fatalf("import: %v", err)
	}

	stored, err := dayService.FetchLogByDate(context.Background(), user.ID, time.Date(2026, time.May, 2, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.ID == 0 {
		t.Fatalf("imported day not found")
	}
	if stored.CycleStart || stored.IsUncertain {
		t.Fatalf("cycle_start/is_uncertain leaked onto a non-period day: %+v", stored)
	}
}

// TestImportServiceRejectsDuplicateDatesWithinFile counts a repeated calendar
// day in the same file as rejected rather than letting it collide on write.
func TestImportServiceRejectsDuplicateDatesWithinFile(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-dup@example.com")

	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-06-01", Period: true, Flow: models.FlowMedium, CycleFactors: []string{}},
		{Date: "2026-06-01", Period: true, Flow: models.FlowHeavy, CycleFactors: []string{}},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 || result.Rejected != 1 {
		t.Fatalf("expected 1 added / 1 rejected, got %+v", result)
	}
}

func TestImportServiceRejectsMalformedPayload(t *testing.T) {
	importService := NewImportService(nil, nil, nil, nil)
	if _, err := importService.ImportJSON(context.Background(), 1, []byte("{not json"), time.UTC); err != ErrImportMalformed {
		t.Fatalf("expected ErrImportMalformed, got %v", err)
	}
}

func TestImportServiceRejectsTooLargePayload(t *testing.T) {
	entries := make([]ExportJSONEntry, MaxImportEntries+1)
	for i := range entries {
		entries[i] = ExportJSONEntry{Date: "2026-01-01", CycleFactors: []string{}}
	}
	raw, _ := json.Marshal(importPayload{Entries: entries})

	importService := NewImportService(nil, nil, nil, nil)
	if _, err := importService.ImportJSON(context.Background(), 1, raw, time.UTC); err != ErrImportTooLarge {
		t.Fatalf("expected ErrImportTooLarge, got %v", err)
	}
}

// TestImportServiceCapsCustomSymptomCreation pins the DoS bound: a file naming
// far more distinct custom symptoms than MaxImportCustomSymptoms creates at most
// that many rows (the rest are dropped), while the day itself still imports — so
// a crafted upload cannot force unbounded catalog growth / DB churn.
func TestImportServiceCapsCustomSymptomCreation(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-symptom-cap@example.com")

	names := make([]string, 0, MaxImportCustomSymptoms+25)
	for i := 0; i < MaxImportCustomSymptoms+25; i++ {
		names = append(names, fmt.Sprintf("custom-%04d", i))
	}
	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-08-01", Period: false, CycleFactors: []string{}, OtherSymptoms: names},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected the day to import, got %+v", result)
	}

	catalog, err := symptomService.FetchSymptoms(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("fetch symptoms: %v", err)
	}
	custom := 0
	for _, symptom := range catalog {
		if !symptom.IsBuiltin {
			custom++
		}
	}
	if custom != MaxImportCustomSymptoms {
		t.Fatalf("expected custom symptom creation capped at %d, got %d", MaxImportCustomSymptoms, custom)
	}
}

// TestImportServiceSanitizesGarbageValues proves untrusted enum/number fields
// are coerced to safe defaults rather than persisted verbatim or rejecting the
// day: a record full of nonsense values still imports, but clean.
func TestImportServiceSanitizesGarbageValues(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-garbage@example.com")

	payload := importPayload{Entries: []ExportJSONEntry{
		{
			Date: "2026-10-01", Period: true, Flow: "ZZZ", MoodRating: 999, BBT: models.NewBBT(9999),
			SexActivity: "??", CervicalMucus: "??", PregnancyTest: "??",
			CycleFactors: []string{"not_a_factor"},
		},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected the garbage day to import sanitized, got %+v", result)
	}

	stored, err := dayService.FetchLogByDate(context.Background(), user.ID, time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Flow != models.FlowNone || stored.Mood != 0 || stored.BBT != nil {
		t.Fatalf("garbage flow/mood/bbt not sanitized: flow=%q mood=%d bbt=%v", stored.Flow, stored.Mood, stored.BBT)
	}
	if stored.SexActivity != models.SexActivityNone || stored.CervicalMucus != models.CervicalMucusNone || stored.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("garbage sex/mucus/pregnancy not sanitized: %+v", stored)
	}
	if len(stored.CycleFactorKeys) != 0 {
		t.Fatalf("invalid cycle-factor key not dropped: %v", stored.CycleFactorKeys)
	}
}

// TestImportServiceRejectsInvalidDates counts unparseable dates as rejected
// without aborting the valid records around them.
func TestImportServiceRejectsInvalidDates(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-baddate@example.com")

	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-13-45", Period: true, CycleFactors: []string{}},
		{Date: "today", CycleFactors: []string{}},
		{Date: "", CycleFactors: []string{}},
		{Date: "2026-10-05", Period: true, CycleFactors: []string{}},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 || result.Rejected != 3 {
		t.Fatalf("expected 1 added / 3 rejected, got %+v", result)
	}
}

// TestImportServiceForeignJSONImportsNothing accepts a syntactically valid JSON
// document that simply isn't an Ovumcy export: no entries means nothing is
// written, and it is not treated as an error.
func TestImportServiceForeignJSONImportsNothing(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-foreign@example.com")

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, []byte(`{"foo":1,"bar":[1,2,3]}`), time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 0 || result.Skipped != 0 || result.Rejected != 0 {
		t.Fatalf("expected an empty result for foreign JSON, got %+v", result)
	}
}

// TestImportServiceRejectsNonObjectPayload rejects a JSON value of the wrong
// top-level shape (array/string) as malformed before touching the database.
func TestImportServiceRejectsNonObjectPayload(t *testing.T) {
	importService := NewImportService(nil, nil, nil, nil)
	for _, raw := range [][]byte{[]byte(`[1,2,3]`), []byte(`"just a string"`), []byte(`42`)} {
		if _, err := importService.ImportJSON(context.Background(), 1, raw, time.UTC); err != ErrImportMalformed {
			t.Fatalf("expected ErrImportMalformed for %q, got %v", raw, err)
		}
	}
}

// TestImportServiceRejectsMarkupSymptomNames proves a crafted custom-symptom
// name containing markup is dropped (not stored) while the day still imports and
// a clean custom name alongside it is created — no stored-XSS vector via import.
func TestImportServiceRejectsMarkupSymptomNames(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	user := createDayServiceTestUser(t, database, "import-markup@example.com")

	payload := importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-10-10", Period: false, CycleFactors: []string{}, OtherSymptoms: []string{"<script>alert(1)</script>", "Valid Custom"}},
	}}
	raw, _ := json.Marshal(payload)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected the day to import, got %+v", result)
	}

	catalog, err := symptomService.FetchSymptoms(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("fetch symptoms: %v", err)
	}
	sawValid := false
	for _, symptom := range catalog {
		if strings.Contains(symptom.Name, "<") || strings.Contains(symptom.Name, ">") {
			t.Fatalf("markup symptom name was stored: %q", symptom.Name)
		}
		if symptom.Name == "Valid Custom" {
			sawValid = true
		}
	}
	if !sawValid {
		t.Fatalf("expected the clean custom symptom to be created alongside the dropped markup one")
	}
}

// TestImportServiceScopesResolvedSymptomsToImportingOwner pins the owner-isolation
// invariant on the restore path (household self-hosting hosts multiple independent
// owners). When owner A imports a day whose other_symptoms name collides with a
// custom symptom already owned by owner B, the resolved SymptomIDs must reference
// A's own row — never B's id — and B's catalog and logs must be untouched.
func TestImportServiceScopesResolvedSymptomsToImportingOwner(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)

	ownerB := createDayServiceTestUser(t, database, "import-idor-b@example.com")
	bSymptom, err := symptomService.CreateSymptomForUser(context.Background(), ownerB.ID, "Shared Name", "", "")
	if err != nil {
		t.Fatalf("seed owner B symptom: %v", err)
	}

	ownerA := createDayServiceTestUser(t, database, "import-idor-a@example.com")
	raw, _ := json.Marshal(importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-06-15", Period: false, CycleFactors: []string{}, OtherSymptoms: []string{"Shared Name"}},
	}})
	importService := newImportServiceIntegration(t, database, symptomService)
	if _, err := importService.ImportJSON(context.Background(), ownerA.ID, raw, time.UTC); err != nil {
		t.Fatalf("import for owner A: %v", err)
	}

	stored, err := dayService.FetchLogByDate(context.Background(), ownerA.ID, time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("reload owner A day: %v", err)
	}
	if len(stored.SymptomIDs) != 1 {
		t.Fatalf("expected exactly one resolved symptom, got %v", stored.SymptomIDs)
	}
	if stored.SymptomIDs[0] == bSymptom.ID {
		t.Fatalf("cross-owner leak: owner A's day references owner B's symptom id %d", bSymptom.ID)
	}

	// The referenced row belongs to A and carries the same name.
	catalogA, err := symptomService.FetchSymptoms(context.Background(), ownerA.ID)
	if err != nil {
		t.Fatalf("fetch owner A catalog: %v", err)
	}
	matched := false
	for _, symptom := range catalogA {
		if symptom.ID != stored.SymptomIDs[0] {
			continue
		}
		if symptom.UserID != ownerA.ID {
			t.Fatalf("resolved symptom %d is owned by %d, not importing owner A (%d)", symptom.ID, symptom.UserID, ownerA.ID)
		}
		if normalizeSymptomNameKey(symptom.Name) != "shared name" {
			t.Fatalf("resolved symptom name mismatch: %q", symptom.Name)
		}
		matched = true
	}
	if !matched {
		t.Fatalf("owner A's day references a symptom absent from A's own catalog")
	}

	// Owner B is untouched: no day logs created for B by A's import.
	logsB, err := dayService.FetchAllLogsForUser(context.Background(), ownerB.ID)
	if err != nil {
		t.Fatalf("load owner B logs: %v", err)
	}
	if len(logsB) != 0 {
		t.Fatalf("owner B gained %d unexpected day logs from owner A's import", len(logsB))
	}
}

// TestImportServiceBBTCompatibilityAcrossLegacyAndCurrentPayloads pins the
// public JSON restore contract for the nullable-BBT change: an importer must
// accept every historical and current spelling of "not measured" — an absent
// key, an explicit null, and the legacy numeric 0 sentinel — and store all of
// them as NULL (nil), while a genuine reading imports verbatim. Each case is
// driven from raw JSON bytes so the wire format (not a Go struct) is exercised,
// and each day is re-exported to prove the round-trip semantics: unmeasured
// days omit `bbt` entirely, and a measured day re-emits the same value.
func TestImportServiceBBTCompatibilityAcrossLegacyAndCurrentPayloads(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)
	user := createDayServiceTestUser(t, database, "import-bbt-compat@example.com")

	// Raw JSON: legacy sentinel 0, explicit null, absent key, and a real reading.
	raw := []byte(`{"entries":[
		{"date":"2026-07-01","period":false,"bbt":0,"cycle_factors":[]},
		{"date":"2026-07-02","period":false,"bbt":null,"cycle_factors":[]},
		{"date":"2026-07-03","period":false,"cycle_factors":[]},
		{"date":"2026-07-04","period":false,"bbt":36.7,"cycle_factors":[]}
	]}`)

	importService := newImportServiceIntegration(t, database, symptomService)
	result, err := importService.ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 4 || result.Rejected != 0 {
		t.Fatalf("expected 4 added / 0 rejected, got %+v", result)
	}

	// Legacy 0, explicit null, and absent all persist as "not measured" (nil).
	for _, day := range []string{"2026-07-01", "2026-07-02", "2026-07-03"} {
		assertStoredBBTNil(t, dayService, user.ID, day)
	}

	// A real reading survives verbatim.
	measured, err := dayService.FetchLogByDate(context.Background(), user.ID, mustParseExportDay(t, "2026-07-04"), time.UTC)
	if err != nil {
		t.Fatalf("reload measured day: %v", err)
	}
	if measured.BBT == nil || *measured.BBT != 36.7 {
		t.Fatalf("expected measured BBT 36.7, got %v", measured.BBT)
	}

	// Round-trip: unmeasured days omit the bbt key entirely; the measured day
	// re-emits its value. Re-exporting proves the stored semantics survive the
	// full export path (pointer + omitempty).
	entries, err := exportService.BuildJSONEntries(context.Background(), user.ID, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}
	byDate := make(map[string]ExportJSONEntry, len(entries))
	for _, entry := range entries {
		byDate[entry.Date] = entry
	}
	for _, day := range []string{"2026-07-01", "2026-07-02", "2026-07-03"} {
		if got := byDate[day].BBT; got != nil {
			t.Fatalf("expected re-exported %s to omit bbt (nil), got %v", day, *got)
		}
	}
	if got := byDate["2026-07-04"].BBT; got == nil || *got != 36.7 {
		t.Fatalf("expected re-exported measured day bbt 36.7, got %v", got)
	}

	// The serialized JSON of an unmeasured day must carry no "bbt" key at all
	// (omitempty), while the measured day must include it — the wire contract.
	assertExportEntryOmitsBBT(t, byDate["2026-07-01"])
	assertExportEntryHasBBT(t, byDate["2026-07-04"], "36.7")
}

func assertStoredBBTNil(t *testing.T, dayService *DayService, userID uint, day string) {
	t.Helper()
	stored, err := dayService.FetchLogByDate(context.Background(), userID, mustParseExportDay(t, day), time.UTC)
	if err != nil {
		t.Fatalf("reload %s: %v", day, err)
	}
	if stored.BBT != nil {
		t.Fatalf("expected %s BBT to be nil (not measured), got %v", day, *stored.BBT)
	}
}

func assertExportEntryOmitsBBT(t *testing.T, entry ExportJSONEntry) {
	t.Helper()
	marshaled, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal %s entry: %v", entry.Date, err)
	}
	if strings.Contains(string(marshaled), "\"bbt\"") {
		t.Fatalf("expected %s export entry to omit the bbt key, got %s", entry.Date, marshaled)
	}
}

func assertExportEntryHasBBT(t *testing.T, entry ExportJSONEntry, want string) {
	t.Helper()
	marshaled, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal %s entry: %v", entry.Date, err)
	}
	if !strings.Contains(string(marshaled), "\"bbt\":"+want) {
		t.Fatalf("expected %s export entry to include bbt:%s, got %s", entry.Date, want, marshaled)
	}
}
