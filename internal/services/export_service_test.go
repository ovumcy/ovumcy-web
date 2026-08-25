package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubExportDayReader struct {
	logs []models.DailyLog
	err  error
	// ownerIDs records the owner operand of every read, so a build that exported
	// under some other account's id can be observed instead of merely assumed.
	ownerIDs []uint
}

func (stub *stubExportDayReader) FetchLogsForOptionalRange(_ context.Context, userID uint, _ *time.Time, _ *time.Time, _ *time.Location) ([]models.DailyLog, error) {
	stub.ownerIDs = append(stub.ownerIDs, userID)
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.DailyLog, len(stub.logs))
	copy(result, stub.logs)
	return result, nil
}

type stubExportSymptomReader struct {
	symptoms []models.SymptomType
	err      error
	// ownerIDs records the owner operand of every catalog read; symptom names are
	// per-owner rows, so an unscoped read here leaks another account's labels.
	ownerIDs []uint
}

func (stub *stubExportSymptomReader) FetchSymptoms(_ context.Context, userID uint) ([]models.SymptomType, error) {
	stub.ownerIDs = append(stub.ownerIDs, userID)
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]models.SymptomType, len(stub.symptoms))
	copy(result, stub.symptoms)
	return result, nil
}

// TestExportServiceReadsEveryEntryPointUnderTheActingOwner pins the owner operand
// of the export read path on both collaborators. Until the stubs above recorded
// it, every export test asserted shapes and counts only, so a read that fetched a
// constant owner's days or symptom catalog stayed green — the privacy boundary in
// docs/SECURITY_INVARIANTS.md forbids exactly that. Each of the four entry points
// is driven separately: they are independent call sites, and one of them losing
// the owner must not be masked by the other three.
func TestExportServiceReadsEveryEntryPointUnderTheActingOwner(t *testing.T) {
	const actingOwnerID uint = 8137

	days := &stubExportDayReader{logs: []models.DailyLog{
		{Date: mustParseExportDay(t, "2026-02-18"), SymptomIDs: []uint{1}},
	}}
	symptoms := &stubExportSymptomReader{symptoms: []models.SymptomType{{ID: 1, Name: "Cramps"}}}
	service := NewExportService(days, symptoms)

	ctx := context.Background()
	if _, _, err := service.LoadDataForRange(ctx, actingOwnerID, nil, nil, time.UTC); err != nil {
		t.Fatalf("LoadDataForRange() unexpected error: %v", err)
	}
	if _, err := service.BuildSummary(ctx, actingOwnerID, nil, nil, time.UTC); err != nil {
		t.Fatalf("BuildSummary() unexpected error: %v", err)
	}
	if _, err := service.BuildJSONEntries(ctx, actingOwnerID, nil, nil, time.UTC); err != nil {
		t.Fatalf("BuildJSONEntries() unexpected error: %v", err)
	}
	if _, err := service.BuildCSVRows(ctx, actingOwnerID, nil, nil, time.UTC); err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}

	// Positive anchors: all four entry points read days, and the three that resolve
	// symptom names read the catalog. A run that reached a collaborator fewer times
	// than that fails here rather than passing as "no mismatched owner found".
	assertExportOwnerObserved(t, "day reader", days.ownerIDs, 4, actingOwnerID)
	assertExportOwnerObserved(t, "symptom reader", symptoms.ownerIDs, 3, actingOwnerID)
}

func assertExportOwnerObserved(t *testing.T, collaborator string, observed []uint, wantCalls int, wantOwnerID uint) {
	t.Helper()

	if len(observed) != wantCalls {
		t.Fatalf("expected %d %s reads, got %d", wantCalls, collaborator, len(observed))
	}
	for index, ownerID := range observed {
		if ownerID != wantOwnerID {
			t.Fatalf("%s read #%d ran for owner %d, want acting owner %d", collaborator, index+1, ownerID, wantOwnerID)
		}
	}
}

func TestExportBuildSummaryUsesDateBounds(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{Date: mustParseExportDay(t, "2026-02-20")},
				{Date: mustParseExportDay(t, "2026-02-07")},
				{Date: mustParseExportDay(t, "2026-02-12")},
			},
		},
		&stubExportSymptomReader{},
	)

	summary, err := service.BuildSummary(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary() unexpected error: %v", err)
	}
	if !summary.HasData {
		t.Fatalf("expected summary.HasData=true")
	}
	if summary.TotalEntries != 3 {
		t.Fatalf("expected TotalEntries=3, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "2026-02-07" {
		t.Fatalf("expected DateFrom=2026-02-07, got %q", summary.DateFrom)
	}
	if summary.DateTo != "2026-02-20" {
		t.Fatalf("expected DateTo=2026-02-20, got %q", summary.DateTo)
	}
}

func TestExportBuildSummaryReturnsEmptyForNoLogs(t *testing.T) {
	service := NewExportService(&stubExportDayReader{logs: []models.DailyLog{}}, &stubExportSymptomReader{})
	summary, err := service.BuildSummary(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary() unexpected error: %v", err)
	}
	if summary.HasData {
		t.Fatalf("expected summary.HasData=false")
	}
	if summary.TotalEntries != 0 {
		t.Fatalf("expected TotalEntries=0, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "" || summary.DateTo != "" {
		t.Fatalf("expected empty date range, got %q..%q", summary.DateFrom, summary.DateTo)
	}
}

// TestBuildSummaryHistoryAndWindowPropagatesTheReadFailure pins the one-read
// pair's failure path. It reads the owner's entries once and derives both
// aggregates from that slice, so a failed read must surface as an error rather
// than as two empty-but-plausible summaries: the settings page would otherwise
// render "no entries" to an owner whose data simply could not be loaded, and
// the export bounds would silently narrow to nothing.
func TestBuildSummaryHistoryAndWindowPropagatesTheReadFailure(t *testing.T) {
	readErr := errors.New("daily_logs read failed")
	service := NewExportService(&stubExportDayReader{err: readErr}, &stubExportSymptomReader{})

	history, window, err := service.BuildSummaryHistoryAndWindow(
		context.Background(), 42, mustParseExportDay(t, "2026-02-18"), time.UTC)
	if !errors.Is(err, readErr) {
		t.Fatalf("BuildSummaryHistoryAndWindow() error = %v, want the reader's own error", err)
	}
	if history.HasData || window.HasData {
		t.Errorf("a failed read must not report data: history=%+v window=%+v", history, window)
	}
	if history.TotalEntries != 0 || window.TotalEntries != 0 {
		t.Errorf("a failed read must not report entries: history=%d window=%d",
			history.TotalEntries, window.TotalEntries)
	}
}

// TestExportSymptomColumnsAreABijectionOntoTheBuiltinCatalog pins the export's
// symptom columns to the catalog they exist for. The mapping used to be a
// hand-written table keyed on the DISPLAY NAME, with one name too many: both
// "Mood swings" and "Mood" resolved to the built-in Mood column, so it was not
// a bijection and one of the two names was not a built-in at all.
func TestExportSymptomColumnsAreABijectionOntoTheBuiltinCatalog(t *testing.T) {
	builtins := models.DefaultBuiltinSymptoms()
	if len(builtins) == 0 {
		t.Fatal("anchor: the built-in catalog must not be empty")
	}
	if len(exportSymptomFlagSetters) != len(builtins) {
		t.Fatalf("the export has %d symptom columns for %d built-in symptoms — the two must be one set", len(exportSymptomFlagSetters), len(builtins))
	}

	for _, builtin := range builtins {
		if _, ok := exportSymptomFlagSetters[builtin.Key]; !ok {
			t.Fatalf("built-in %q has no export column keyed on it; the columns are keyed on something other than the catalog key", builtin.Key)
		}
		if got := exportSymptomColumn(builtin.Name); got != builtin.Key {
			t.Fatalf("the built-in named %q must fill its own column %q, got %q", builtin.Name, builtin.Key, got)
		}
	}

	// The import direction reads the same vocabulary in reverse, so the two
	// maps are one set or a re-import silently drops the column they disagree
	// on — which is how this key was found.
	if len(importSymptomFlagGetters) != len(exportSymptomFlagSetters) {
		t.Fatalf("export has %d symptom columns, import reads %d", len(exportSymptomFlagSetters), len(importSymptomFlagGetters))
	}
	for column := range exportSymptomFlagSetters {
		if _, ok := importSymptomFlagGetters[column]; !ok {
			t.Fatalf("column %q is written by the export and unknown to the import", column)
		}
	}

	// The extra alias: a name that is NOT a built-in name must not reach a
	// built-in column, however close it reads.
	if got := exportSymptomColumn("Mood"); got != "other" {
		t.Fatalf(`"Mood" is not a built-in name — the catalog calls that symptom "Mood swings" — so it must export as an other symptom, got column %q`, got)
	}
}

// TestBuildExportSymptomFlagsKeepsACustomNameOutOfABuiltinColumn is the owner-
// facing half of the same defect: a custom symptom whose name collided with an
// alias of a built-in column was exported AS that built-in, and — because a
// matched name never reaches the other-symptoms set — vanished from the export
// entirely. Re-importing restored the wrong symptom with nothing to signal it.
func TestBuildExportSymptomFlagsKeepsACustomNameOutOfABuiltinColumn(t *testing.T) {
	names := map[uint]string{
		1: "Mood swings", // the built-in
		2: "Mood",        // an owner's own symptom, allowed: not a reserved name
	}

	builtinFlags, builtinOther := buildExportSymptomFlags([]uint{1}, names)
	if !builtinFlags.Mood {
		t.Fatal("anchor: the built-in Mood swings must still fill its own column")
	}
	if len(builtinOther) != 0 {
		t.Fatalf("a built-in belongs in its column, not in other symptoms: %v", builtinOther)
	}

	customFlags, customOther := buildExportSymptomFlags([]uint{2}, names)
	if customFlags.Mood {
		t.Fatal(`an owner's custom "Mood" was exported as the built-in "Mood swings" — a different symptom's data under a built-in's label`)
	}
	if len(customOther) != 1 || customOther[0] != "Mood" {
		t.Fatalf(`expected the custom symptom to be carried in other symptoms, got %v`, customOther)
	}
}

// TestExportCSVHeadersCannotBeMutatedByACaller pins the header row as a value,
// not as shared state. It used to be an exported package-level slice handed
// straight to the CSV writer, so any consumer — a test, a future caller —
// could rewrite the schema of every export in the process, concurrent ones
// included, with nothing to catch it.
func TestExportCSVHeadersCannotBeMutatedByACaller(t *testing.T) {
	headers := ExportCSVHeaders()
	if len(headers) == 0 {
		t.Fatal("anchor: the CSV export must carry a header row")
	}

	original := headers[0]
	headers[0] = "Rewritten by a caller"

	if got := ExportCSVHeaders()[0]; got != original {
		t.Fatalf("a caller's write reached the next export's header row: expected %q, got %q", original, got)
	}
}

func TestExportBuildJSONEntriesNormalizesFlowAndMapsSymptoms(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:            mustParseExportDay(t, "2026-02-19"),
					Flow:            "unexpected-flow",
					Mood:            4,
					SexActivity:     models.SexActivityProtected,
					BBT:             new(36.55),
					CervicalMucus:   models.CervicalMucusEggWhite,
					PregnancyTest:   models.PregnancyTestPositive,
					CycleStart:      true,
					IsUncertain:     true,
					CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorTravel},
					SymptomIDs:      []uint{1, 2, 3, 3},
					Notes:           "json-note",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Mood swings"},
				{ID: 2, Name: "My Custom"},
				{ID: 3, Name: "Another Custom"},
			},
		},
	)

	entries, err := service.BuildJSONEntries(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildJSONEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}

	entry := entries[0]
	assertExportJSONEntryCoreFields(t, entry)
	assertExportJSONEntryTrackingFields(t, entry)
	assertExportJSONEntrySymptomFields(t, entry)
}

func TestExportBuildCSVRowsBuildsExpectedColumns(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:            mustParseExportDay(t, "2026-02-18"),
					IsPeriod:        true,
					Flow:            models.FlowLight,
					Mood:            5,
					SexActivity:     models.SexActivityUnprotected,
					BBT:             new(36.7),
					CervicalMucus:   models.CervicalMucusCreamy,
					PregnancyTest:   models.PregnancyTestNegative,
					CycleStart:      true,
					IsUncertain:     true,
					CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorMedicationChange},
					SymptomIDs:      []uint{1, 2},
					Notes:           "note",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "Cramps"},
				{ID: 2, Name: "Custom Symptom"},
			},
		},
	)

	rows, err := service.BuildCSVRows(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	if headers := ExportCSVHeaders(); len(columns) != len(headers) {
		t.Fatalf("expected %d csv columns, got %d", len(headers), len(columns))
	}
	assertExportCSVFixedColumns(t, columns)
	indexByHeader := exportCSVIndexByHeader()
	assertExportCSVTrackingColumns(t, columns, indexByHeader)
	assertExportCSVSymptomColumns(t, columns, indexByHeader)
}

func TestExportBuildCSVRowsNeutralizesFormulaLikeCells(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:       mustParseExportDay(t, "2026-02-18"),
					SymptomIDs: []uint{1},
					Notes:      "  =cmd|' /C calc'!A0",
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: "@Doctor export"},
			},
		},
	)

	rows, err := service.BuildCSVRows(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	indexByHeader := exportCSVIndexByHeader()

	if columns[indexByHeader["Other"]] != "'@Doctor export" {
		t.Fatalf("expected sanitized other symptom cell, got %q", columns[indexByHeader["Other"]])
	}
	if columns[indexByHeader["Notes"]] != "'  =cmd|' /C calc'!A0" {
		t.Fatalf("expected sanitized notes cell, got %q", columns[indexByHeader["Notes"]])
	}
}

// TestExportBuildCSVRowsNeutralizesEveryDangerousPrefix drives every prefix the
// policy treats as dangerous through the finished CSV column, not through the
// helper. Only '=' and '@' were ever exercised, so the other five switch cases
// could be deleted together and the export still looked neutralized: a note
// opening with '+', '-', a tab or a bare CR/LF went into the file as a live
// formula for whatever spreadsheet the owner opens their own health export in.
//
// The leading-space variants matter separately — the check trims spaces before
// reading the first byte, which is exactly the disguise a hostile value uses.
func TestExportBuildCSVRowsNeutralizesEveryDangerousPrefix(t *testing.T) {
	dangerous := []struct {
		name   string
		prefix string
		// printable marks the prefixes a symptom NAME can still carry: names
		// reach the cell through TrimSpace (export_service.go), which strips a
		// tab or a bare CR/LF along with the padding. Notes are stored verbatim
		// and carry all seven.
		printable bool
	}{
		{name: "equals", prefix: "=", printable: true},
		{name: "plus", prefix: "+", printable: true},
		{name: "minus", prefix: "-", printable: true},
		{name: "at", prefix: "@", printable: true},
		{name: "tab", prefix: "\t"},
		{name: "carriage return", prefix: "\r"},
		{name: "line feed", prefix: "\n"},
	}

	for _, entry := range dangerous {
		for _, lead := range []struct {
			name  string
			value string
		}{
			{name: "bare", value: ""},
			{name: "space padded", value: "  "},
		} {
			t.Run(entry.name+" "+lead.name, func(t *testing.T) {
				note := lead.value + entry.prefix + `cmd|' /C calc'!A0`
				symptom := entry.prefix + "Doctor export"

				columns, indexByHeader := exportCSVColumnsForCell(t, note, symptom)

				if got := columns[indexByHeader["Notes"]]; got != "'"+note {
					t.Fatalf("expected the notes cell to be neutralized, got %q", got)
				}
				if !entry.printable {
					return
				}
				if got := columns[indexByHeader["Other"]]; got != "'"+symptom {
					t.Fatalf("expected the other-symptom cell to be neutralized, got %q", got)
				}
			})
		}
	}

	// Positive control: an ordinary note must reach the file unchanged, so the
	// guard is not simply quoting every cell.
	t.Run("ordinary text is untouched", func(t *testing.T) {
		columns, indexByHeader := exportCSVColumnsForCell(t, "cramps in the evening", "Doctor export")

		if got := columns[indexByHeader["Notes"]]; got != "cramps in the evening" {
			t.Fatalf("expected an ordinary note to survive unquoted, got %q", got)
		}
		if got := columns[indexByHeader["Other"]]; got != "Doctor export" {
			t.Fatalf("expected an ordinary symptom name to survive unquoted, got %q", got)
		}
	})
}

// exportCSVColumnsForCell builds the single CSV row of a day carrying `note`
// and one custom symptom named `symptom`.
func exportCSVColumnsForCell(t *testing.T, note string, symptom string) ([]string, map[string]int) {
	t.Helper()

	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date:       mustParseExportDay(t, "2026-02-18"),
					SymptomIDs: []uint{1},
					Notes:      note,
				},
			},
		},
		&stubExportSymptomReader{
			symptoms: []models.SymptomType{
				{ID: 1, Name: symptom},
			},
		},
	)

	rows, err := service.BuildCSVRows(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	return rows[0].Columns(), exportCSVIndexByHeader()
}

func TestExportServicePropagatesDependencyErrors(t *testing.T) {
	dayErrService := NewExportService(
		&stubExportDayReader{err: errors.New("load failed")},
		&stubExportSymptomReader{},
	)
	if _, err := dayErrService.BuildSummary(context.Background(), 1, nil, nil, time.UTC); err == nil {
		t.Fatalf("expected summary error when day reader fails")
	}

	symptomErrService := NewExportService(
		&stubExportDayReader{logs: []models.DailyLog{{Date: mustParseExportDay(t, "2026-02-18")}}},
		&stubExportSymptomReader{err: errors.New("symptom load failed")},
	)
	if _, err := symptomErrService.BuildJSONEntries(context.Background(), 1, nil, nil, time.UTC); err == nil {
		t.Fatalf("expected json entries error when symptom reader fails")
	}
}

func TestCsvPregnancyTestLabel(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{models.PregnancyTestPositive, "Positive"},
		{models.PregnancyTestNegative, "Negative"},
		{models.PregnancyTestNone, "None"},
		{"bogus-value", "None"},
		{"", "None"},
	}
	for _, testCase := range cases {
		if got := csvPregnancyTestLabel(testCase.value); got != testCase.want {
			t.Fatalf("csvPregnancyTestLabel(%q) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

func mustParseExportDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

func assertExportJSONEntryCoreFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if entry.Date != "2026-02-19" {
		t.Fatalf("expected Date=2026-02-19, got %q", entry.Date)
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected normalized flow=%q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.Notes != "json-note" {
		t.Fatalf("expected notes preserved, got %q", entry.Notes)
	}
}

func assertExportJSONEntryTrackingFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if entry.MoodRating != 4 {
		t.Fatalf("expected mood rating 4, got %d", entry.MoodRating)
	}
	if entry.SexActivity != models.SexActivityProtected {
		t.Fatalf("expected protected sex activity, got %q", entry.SexActivity)
	}
	if entry.BBT == nil || *entry.BBT != 36.55 {
		t.Fatalf("expected BBT 36.55, got %v", entry.BBT)
	}
	if entry.CervicalMucus != models.CervicalMucusEggWhite {
		t.Fatalf("expected eggwhite cervical mucus, got %q", entry.CervicalMucus)
	}
	if entry.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected positive pregnancy test, got %q", entry.PregnancyTest)
	}
	if !entry.CycleStart {
		t.Fatalf("expected cycle_start=true")
	}
	if !entry.IsUncertain {
		t.Fatalf("expected is_uncertain=true")
	}
	if len(entry.CycleFactors) != 2 || entry.CycleFactors[0] != models.CycleFactorStress || entry.CycleFactors[1] != models.CycleFactorTravel {
		t.Fatalf("expected normalized cycle factors, got %#v", entry.CycleFactors)
	}
}

func assertExportJSONEntrySymptomFields(t *testing.T, entry ExportJSONEntry) {
	t.Helper()

	if !entry.Symptoms.Mood {
		t.Fatalf("expected mood flag=true")
	}
	if len(entry.OtherSymptoms) != 2 || entry.OtherSymptoms[0] != "Another Custom" || entry.OtherSymptoms[1] != "My Custom" {
		t.Fatalf("expected sorted deduped other symptoms, got %#v", entry.OtherSymptoms)
	}
}

func assertExportCSVFixedColumns(t *testing.T, columns []string) {
	t.Helper()

	if columns[0] != "2026-02-18" || columns[1] != "Yes" || columns[2] != "Light" {
		t.Fatalf("unexpected fixed csv columns: %#v", columns[:3])
	}
}

func assertExportCSVTrackingColumns(t *testing.T, columns []string, indexByHeader map[string]int) {
	t.Helper()

	if columns[indexByHeader["Mood rating"]] != "5" {
		t.Fatalf("expected mood rating column 5, got %q", columns[indexByHeader["Mood rating"]])
	}
	if columns[indexByHeader["Sex activity"]] != "Unprotected" {
		t.Fatalf("expected sex activity column Unprotected, got %q", columns[indexByHeader["Sex activity"]])
	}
	if columns[indexByHeader["BBT (C)"]] != "36.70" {
		t.Fatalf("expected BBT column 36.70, got %q", columns[indexByHeader["BBT (C)"]])
	}
	if columns[indexByHeader["Cervical mucus"]] != "Creamy" {
		t.Fatalf("expected cervical mucus column Creamy, got %q", columns[indexByHeader["Cervical mucus"]])
	}
	if columns[indexByHeader["Pregnancy test"]] != "Negative" {
		t.Fatalf("expected pregnancy test column Negative, got %q", columns[indexByHeader["Pregnancy test"]])
	}
	if columns[indexByHeader["Cycle factors"]] != "Stress; Medication change" {
		t.Fatalf("expected cycle factors column, got %q", columns[indexByHeader["Cycle factors"]])
	}
	if columns[indexByHeader["Cycle start"]] != "Yes" {
		t.Fatalf("expected cycle start column Yes, got %q", columns[indexByHeader["Cycle start"]])
	}
	if columns[indexByHeader["Uncertain"]] != "Yes" {
		t.Fatalf("expected uncertain column Yes, got %q", columns[indexByHeader["Uncertain"]])
	}
	// Append-only stability contract: the two new tracking columns must remain the
	// final two headers, after Pregnancy test (docs/export.md "Stability").
	headers := ExportCSVHeaders()
	if got := headers[len(headers)-2]; got != "Cycle start" {
		t.Fatalf("expected second-to-last header to be Cycle start, got %q", got)
	}
	if got := headers[len(headers)-1]; got != "Uncertain" {
		t.Fatalf("expected last header to be Uncertain, got %q", got)
	}
}

func assertExportCSVSymptomColumns(t *testing.T, columns []string, indexByHeader map[string]int) {
	t.Helper()

	if columns[indexByHeader["Cramps"]] != "Yes" {
		t.Fatalf("expected cramps column Yes, got %q", columns[indexByHeader["Cramps"]])
	}
	if columns[indexByHeader["Other"]] != "Custom Symptom" {
		t.Fatalf("expected other symptom column, got %q", columns[indexByHeader["Other"]])
	}
	if columns[indexByHeader["Notes"]] != "note" {
		t.Fatalf("expected notes column, got %q", columns[indexByHeader["Notes"]])
	}
}

func exportCSVIndexByHeader() map[string]int {
	headers := ExportCSVHeaders()
	indexByHeader := make(map[string]int, len(headers))
	for index, header := range headers {
		indexByHeader[header] = index
	}
	return indexByHeader
}
