package services

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Barrier for the per-day symptom-id counting class.
//
// A day's SymptomIDs is a stored slice, and every surface that counts symptoms
// walks it once per day. A slice that repeated an id would count that day
// twice: the phase insights divide the count by the number of days in the
// phase, so a repeat renders a percentage above 100, and the frequency list and
// the picker's usage ranking are skewed the same way.
//
// ValidateSymptomIDs builds a unique map before persisting, so no stored slice
// repeats an id today. That is what makes this internal rather than a live
// defect — and it is exactly the shape this repository treats as a defect in
// its own right when it is fixed at some sites and not others: three read sites
// deduped and three not, with the deduped ones making the bare ones look
// deliberate. A second write path, an import, or a repaired row is all it takes
// for the bare sites to diverge from the deduped ones.
//
// So the class is closed at every site, and this barrier reads the shipped
// source to keep it closed: a new loop over a day's symptom ids that does not
// route through one of the two helpers fails here the moment it is written.
//
// What it cannot see, stated here and repeated in the failure text: it matches
// a range expression whose source ends in `symptomids`, case-insensitively. A
// slice first assigned to a differently named local, or counted with an index
// loop rather than `range`, is invisible to it.
var symptomIDDedupHelpers = map[string]struct{}{
	"uniqueSymptomIDs":      {},
	"uniqueKnownSymptomIDs": {},
}

// symptomIDLoopExemptions are the functions whose loop over a day's symptom ids
// is provably idempotent under a repeat, so routing it through a helper would
// buy nothing. Each entry carries the reason it is not a counting site; a loop
// that increments anything does not belong here.
var symptomIDLoopExemptions = map[string]string{
	"buildExportSymptomFlags": "sets booleans and collects names into a set, so a repeated id changes no output",
}

// TestEverySymptomIDLoopDedupes fails when a production loop in this package
// walks a day's symptom ids without routing them through a dedup helper. The
// helpers' own bodies are exempt — one of them is where the deduping happens.
func TestEverySymptomIDLoopDedupes(t *testing.T) {
	root := servicesSourceBarrierRoot(t)
	dir := filepath.Join(root, "internal", "services")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	inspected, found, exempted := 0, 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		inspected++

		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if _, helper := symptomIDDedupHelpers[fn.Name.Name]; helper {
				continue
			}
			if _, exempt := symptomIDLoopExemptions[fn.Name.Name]; exempt {
				exempted++
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				loop, ok := node.(*ast.RangeStmt)
				if !ok {
					return true
				}
				source := symptomLoopSource(t, fset, loop.X)
				switch {
				case symptomLoopRoutesThroughADedupHelper(loop.X):
					found++
				case strings.HasSuffix(strings.ToLower(source), "symptomids"):
					found++
					t.Errorf("%s:%d ranges over %s without deduping: a repeated id counts its day twice, which renders a phase percentage above 100 and skews both the frequency list and the picker's usage ranking. Route it through %v",
						name, fset.Position(loop.Pos()).Line, source, sortedKeys(symptomIDDedupHelpers))
				}
				return true
			})
		}
	}

	if inspected == 0 {
		t.Fatalf("no production file was parsed in %s — the barrier swept nothing", dir)
	}
	if found == 0 {
		t.Fatalf("no loop over a day's symptom ids was found in %s — the barrier's matcher no longer recognises the shape it judges", dir)
	}
	if exempted != len(symptomIDLoopExemptions) {
		t.Errorf("%d of the %d exempted functions were swept: an exemption for a function this package no longer declares hides nothing and should be dropped", exempted, len(symptomIDLoopExemptions))
	}
}

// TestSymptomDedupHelpersAgreeOnARepeatedID anchors the sweep on fixtures this
// test owns, so it cannot pass by pointing every loop at a helper that dedupes
// nothing.
func TestSymptomDedupHelpersAgreeOnARepeatedID(t *testing.T) {
	known := map[uint]models.SymptomType{1: {ID: 1}, 2: {ID: 2}}

	if got := uniqueSymptomIDs([]uint{1, 1, 2, 1}); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("uniqueSymptomIDs did not collapse a repeat in stored order: %v", got)
	}
	if got := uniqueSymptomIDs([]uint{2, 1}); len(got) != 2 || got[0] != 2 {
		t.Errorf("uniqueSymptomIDs dropped or reordered ids that repeat nothing: %v", got)
	}
	if got := uniqueKnownSymptomIDs([]uint{1, 1, 3}, known); len(got) != 1 || got[0] != 1 {
		t.Errorf("uniqueKnownSymptomIDs kept a repeat or an unknown id: %v", got)
	}
}

// TestPhaseSymptomPercentageSurvivesARepeatedID pins the consequence the sweep
// above is written for: a day whose stored slice repeats an id must not push a
// phase percentage past 100.
func TestPhaseSymptomPercentageSurvivesARepeatedID(t *testing.T) {
	symptomByID := map[uint]models.SymptomType{
		1: {ID: 1, Name: "Cramps", Icon: "C"},
	}
	counter := &phaseSymptomCounter{counts: map[uint]int{}}
	appendPhaseSymptomCounts(counter, []uint{1, 1, 1}, symptomByID)
	counter.totalDays++

	items := phaseSymptomInsightItems(counter, symptomByID)
	if len(items) != 1 {
		t.Fatalf("expected one phase symptom item, got %d", len(items))
	}
	if items[0].Count != 1 {
		t.Errorf("one day carrying a repeated id counted %d times", items[0].Count)
	}
	if items[0].Percentage > 100 {
		t.Errorf("a repeated id rendered %.1f%% of the phase's days", items[0].Percentage)
	}
}

// TestSymptomFrequencySurvivesARepeatedID pins the same consequence on the
// frequency list, whose count is read against the day total.
func TestSymptomFrequencySurvivesARepeatedID(t *testing.T) {
	service := NewSymptomService(&stubSymptomRepo{
		builtinCnt: 1,
		listed:     []models.SymptomType{{ID: 1, Name: "Cramps", Icon: "C"}},
	})

	logs := []models.DailyLog{{SymptomIDs: []uint{1, 1}}}
	result, err := service.CalculateFrequencies(context.Background(), 10, logs)
	if err != nil {
		t.Fatalf("CalculateFrequencies() unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected one frequency, got %d", len(result))
	}
	if result[0].Count > result[0].TotalDays {
		t.Errorf("a repeated id counted %d times across %d days", result[0].Count, result[0].TotalDays)
	}
}

// TestPickerRankingSurvivesARepeatedID pins the third bare site: one day that
// repeats an id must not outrank two days that each logged another.
func TestPickerRankingSurvivesARepeatedID(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 1, Name: "Cramps"},
		{ID: 2, Name: "Headache"},
	}
	logs := []models.DailyLog{
		{SymptomIDs: []uint{1, 1, 1}},
		{SymptomIDs: []uint{2}},
		{SymptomIDs: []uint{2}},
	}

	ranked := RankSymptomsForEntryPicker(symptoms, logs)
	if len(ranked) != 2 {
		t.Fatalf("expected two ranked symptoms, got %d", len(ranked))
	}
	if ranked[0].ID != 2 {
		t.Errorf("one day repeating id 1 outranked the two days that logged id 2: got id %d first", ranked[0].ID)
	}
}

func symptomLoopSource(t *testing.T, fset *token.FileSet, expr ast.Expr) string {
	t.Helper()

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		t.Fatalf("printing a range expression: %v", err)
	}
	return buf.String()
}

func symptomLoopRoutesThroughADedupHelper(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	callee, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	_, known := symptomIDDedupHelpers[callee.Name]
	return known
}
