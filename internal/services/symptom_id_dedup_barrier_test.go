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
// Three properties keep the sweep honest, and each was added after the sweep
// was measured passing without it:
//
//   - an exemption clears ONE loop, not a whole function, and an exempted loop
//     that increments anything is itself an error — a counting loop added
//     beside the cleared one used to be unexamined;
//   - a single-assignment local is resolved back to the expression it was
//     assigned from, because `ids := logEntry.SymptomIDs` followed by
//     `range ids` is an everyday refactor and used to walk straight past;
//   - the violation matcher is anchored on fixture sources this test owns, one
//     that must be flagged and one that must not, because the real tree's six
//     compliant sites keep the "found something" counter above zero on their
//     own and would hide a matcher that had stopped matching.
//
// What it still cannot see, stated here and repeated in the failure text: a
// local assigned more than once, a slice handed to a helper of this package's
// own before being counted, an index loop rather than `range`, and any field
// not named SymptomIDs. Those are why the three SurvivesARepeatedID guards
// below exist beside it — the sweep is the barrier for sites added tomorrow,
// and it is not the only one for the sites that exist today.
var symptomIDDedupHelpers = map[string]struct{}{
	"uniqueSymptomIDs":      {},
	"uniqueKnownSymptomIDs": {},
}

// symptomIDLoopExemption clears exactly one loop: the one in `function` whose
// range expression reads `rangeExpr` after alias resolution. Naming the
// expression rather than the function is what stops the exemption from
// spreading to a second loop somebody adds later.
type symptomIDLoopExemption struct {
	function  string
	rangeExpr string
	reason    string
}

// symptomIDLoopExemptions are the loops whose walk over a day's symptom ids is
// provably idempotent under a repeat, so routing them through a helper would
// buy nothing. A loop that increments anything does not belong here, and the
// sweep enforces that rather than trusting this comment.
var symptomIDLoopExemptions = []symptomIDLoopExemption{
	{
		function:  "buildExportSymptomFlags",
		rangeExpr: "symptomIDs",
		reason:    "sets booleans and collects names into a set, so a repeated id changes no output",
	},
}

// Verdicts the sweep can reach for one loop over a day's symptom ids.
const (
	symptomLoopRouted          = "routed"
	symptomLoopViolation       = "violation"
	symptomLoopExempt          = "exempt"
	symptomLoopExemptButCounts = "exempt-but-counts"
)

type symptomIDLoopFinding struct {
	verdict   string
	function  string
	line      int
	source    string // range expression, after alias resolution
	written   string // range expression as written, when resolution changed it
	exemption int    // index into the exemption slice, for exempt verdicts
}

// TestEverySymptomIDLoopDedupes fails when a production loop in this package
// walks a day's symptom ids without routing them through a dedup helper.
func TestEverySymptomIDLoopDedupes(t *testing.T) {
	root := servicesSourceBarrierRoot(t)
	dir := filepath.Join(root, "internal", "services")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	inspected, found := 0, 0
	exemptionUses := make([]int, len(symptomIDLoopExemptions))

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

		for _, finding := range symptomIDLoopFindings(t, fset, parsed, symptomIDLoopExemptions) {
			found++
			switch finding.verdict {
			case symptomLoopViolation:
				t.Errorf("%s:%d ranges over %s%s without deduping: a repeated id counts its day twice, which renders a phase percentage above 100 and skews both the frequency list and the picker's usage ranking. Route it through %v",
					name, finding.line, finding.source, symptomLoopAliasNote(finding), sortedKeys(symptomIDDedupHelpers))
			case symptomLoopExemptButCounts:
				t.Errorf("%s:%d is exempted as %q, and its body increments a counter: an exemption clears a loop whose output a repeat cannot change, which this loop's no longer is",
					name, finding.line, symptomIDLoopExemptions[finding.exemption].reason)
				exemptionUses[finding.exemption]++
			case symptomLoopExempt:
				exemptionUses[finding.exemption]++
			}
		}
	}

	if inspected == 0 {
		t.Fatalf("no production file was parsed in %s — the barrier swept nothing", dir)
	}
	if found == 0 {
		t.Fatalf("no loop over a day's symptom ids was found in %s — the barrier's matcher no longer recognises the shape it judges", dir)
	}
	for index, uses := range exemptionUses {
		if uses != 1 {
			t.Errorf("the exemption for %s over %q matched %d loops, want exactly 1: an exemption that matches nothing hides nothing and should be dropped, and one that matches twice is clearing a loop nobody cleared",
				symptomIDLoopExemptions[index].function, symptomIDLoopExemptions[index].rangeExpr, uses)
		}
	}
}

// TestSymptomIDSweepMatcherFlagsAKnownViolation anchors the sweep on fixture
// sources this test owns. The real tree's compliant sites keep the sweep's
// "found something" counter above zero by themselves, so without a fixture that
// MUST be flagged, a matcher that had stopped matching would report success
// while detecting nothing.
func TestSymptomIDSweepMatcherFlagsAKnownViolation(t *testing.T) {
	exemptions := []symptomIDLoopExemption{
		{function: "exemptedFixture", rangeExpr: "logEntry.SymptomIDs", reason: "fixture"},
	}

	tests := []struct {
		name        string
		source      string
		wantVerdict string
		wantSource  string
	}{
		{
			name: "a bare loop is a violation",
			source: `package p
func f(logEntry L, counts map[uint]int) {
	for _, id := range logEntry.SymptomIDs {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopViolation,
			wantSource:  "logEntry.SymptomIDs",
		},
		{
			name: "a helper-routed loop is compliant",
			source: `package p
func f(logEntry L, counts map[uint]int) {
	for _, id := range uniqueSymptomIDs(logEntry.SymptomIDs) {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopRouted,
			wantSource:  "uniqueSymptomIDs(logEntry.SymptomIDs)",
		},
		{
			name: "a single-assignment alias is resolved and still a violation",
			source: `package p
func f(logEntry L, counts map[uint]int) {
	ids := logEntry.SymptomIDs
	for _, id := range ids {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopViolation,
			wantSource:  "logEntry.SymptomIDs",
		},
		{
			name: "an alias of a helper call is resolved and compliant",
			source: `package p
func f(logEntry L, counts map[uint]int) {
	ids := uniqueSymptomIDs(logEntry.SymptomIDs)
	for _, id := range ids {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopRouted,
			wantSource:  "uniqueSymptomIDs(logEntry.SymptomIDs)",
		},
		{
			name: "an exempted loop that counts nothing stays exempt",
			source: `package p
func exemptedFixture(logEntry L, flags map[uint]bool) {
	for _, id := range logEntry.SymptomIDs {
		flags[id] = true
	}
}`,
			wantVerdict: symptomLoopExempt,
			wantSource:  "logEntry.SymptomIDs",
		},
		{
			name: "an exempted loop that increments is an error in its own right",
			source: `package p
func exemptedFixture(logEntry L, counts map[uint]int) {
	for _, id := range logEntry.SymptomIDs {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopExemptButCounts,
			wantSource:  "logEntry.SymptomIDs",
		},
		{
			name: "a second loop beside an exempted one is still swept",
			source: `package p
func exemptedFixture(logEntry L, counts map[uint]int) {
	for _, id := range logEntry.SymptomIDs {
		_ = id
	}
	for _, id := range logEntry.OtherSymptomIDs {
		counts[id]++
	}
}`,
			wantVerdict: symptomLoopViolation,
			wantSource:  "logEntry.OtherSymptomIDs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, "fixture.go", tc.source, 0)
			if err != nil {
				t.Fatalf("parsing the fixture: %v", err)
			}

			findings := symptomIDLoopFindings(t, fset, parsed, exemptions)
			matched := 0
			for _, finding := range findings {
				if finding.source != tc.wantSource {
					continue
				}
				matched++
				if finding.verdict != tc.wantVerdict {
					t.Errorf("the sweep called %s %q, want %q", tc.wantSource, finding.verdict, tc.wantVerdict)
				}
			}
			if matched != 1 {
				t.Fatalf("the sweep returned %d findings for %s, want exactly 1 — the matcher no longer recognises the shape it judges (all findings: %+v)", matched, tc.wantSource, findings)
			}
		})
	}
}

// TestSymptomDedupHelpersAgreeOnARepeatedID anchors the helpers themselves, so
// the sweep cannot pass by pointing every loop at a helper that dedupes
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

// symptomIDLoopFindings classifies every `range` over a day's symptom ids in
// one parsed file. The helpers' own bodies are skipped — one of them is where
// the deduping happens — and everything else is judged loop by loop.
func symptomIDLoopFindings(t *testing.T, fset *token.FileSet, file *ast.File, exemptions []symptomIDLoopExemption) []symptomIDLoopFinding {
	t.Helper()

	var findings []symptomIDLoopFinding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, helper := symptomIDDedupHelpers[fn.Name.Name]; helper {
			continue
		}

		aliases := singleAssignmentLocals(fn.Body)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			loop, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}

			written := symptomLoopSource(t, fset, loop.X)
			expr := loop.X
			if ident, isIdent := loop.X.(*ast.Ident); isIdent {
				if resolved, known := aliases[ident.Name]; known {
					expr = resolved
				}
			}
			source := symptomLoopSource(t, fset, expr)

			routed := symptomLoopRoutesThroughADedupHelper(expr)
			if !routed && !strings.HasSuffix(strings.ToLower(source), "symptomids") {
				return true
			}

			finding := symptomIDLoopFinding{
				function:  fn.Name.Name,
				line:      fset.Position(loop.Pos()).Line,
				source:    source,
				exemption: -1,
			}
			if source != written {
				finding.written = written
			}

			switch {
			case routed:
				finding.verdict = symptomLoopRouted
			default:
				finding.verdict = symptomLoopViolation
				for index, exemption := range exemptions {
					if exemption.function != fn.Name.Name || exemption.rangeExpr != source {
						continue
					}
					finding.exemption = index
					finding.verdict = symptomLoopExempt
					if loopBodyIncrements(loop.Body) {
						finding.verdict = symptomLoopExemptButCounts
					}
					break
				}
			}

			findings = append(findings, finding)
			return true
		})
	}
	return findings
}

// singleAssignmentLocals maps each local that is assigned exactly once in the
// body to the expression it was assigned from. Assigned twice, or assigned by a
// `range` clause, and it is left unresolved: this is one dataflow step, enough
// for the extract-a-local refactor and deliberately not more.
func singleAssignmentLocals(body *ast.BlockStmt) map[string]ast.Expr {
	assignments := map[string]int{}
	candidates := map[string]ast.Expr{}

	note := func(expr ast.Expr) string {
		ident, ok := expr.(*ast.Ident)
		if !ok || ident.Name == "_" {
			return ""
		}
		assignments[ident.Name]++
		return ident.Name
	}

	ast.Inspect(body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range stmt.Lhs {
				name := note(lhs)
				if name != "" && stmt.Tok == token.DEFINE && len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
					candidates[name] = stmt.Rhs[0]
				}
			}
		case *ast.RangeStmt:
			if stmt.Tok == token.DEFINE {
				note(stmt.Key)
				note(stmt.Value)
			}
		case *ast.IncDecStmt:
			note(stmt.X)
		case *ast.ValueSpec:
			for index, name := range stmt.Names {
				assignments[name.Name]++
				if len(stmt.Names) == 1 && len(stmt.Values) == 1 {
					candidates[name.Name] = stmt.Values[index]
				}
			}
		}
		return true
	})

	resolved := map[string]ast.Expr{}
	for name, expr := range candidates {
		if assignments[name] == 1 {
			resolved[name] = expr
		}
	}
	return resolved
}

// loopBodyIncrements reports whether the loop counts anything — `x++`, `x--` or
// a `+=`/`-=`. An exemption is a statement that a repeated id changes no
// output, and a counter is the one construct that always makes that false.
func loopBodyIncrements(body *ast.BlockStmt) bool {
	counts := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.IncDecStmt:
			counts = true
		case *ast.AssignStmt:
			if stmt.Tok == token.ADD_ASSIGN || stmt.Tok == token.SUB_ASSIGN {
				counts = true
			}
		}
		return !counts
	})
	return counts
}

func symptomLoopAliasNote(finding symptomIDLoopFinding) string {
	if finding.written == "" {
		return ""
	}
	return " (written as " + finding.written + ")"
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
