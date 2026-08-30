package services

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Barrier for the recombined-suppression class.
//
// Every medical-safety suppression signal lives in ONE predicate here —
// PredictionsSuppressed for the whole projection, FertilityProjectionSuppressed
// for the fertility half — and a surface either calls that predicate or reads
// the decision the cycle context already resolved. What it may not do is spell
// the disjuncts out again for itself. That is not a style preference: the three
// signals WERE written out once per surface, so the fourth had to be found at
// four sites and the one that was missed stayed missing silently, which is the
// estimate-presented-as-fact the medical-safety invariant forbids
// (docs/SECURITY_INVARIANTS.md -> medical safety).
//
// The prose ban is not new and it is not enough: it was breached three times
// running and caught by review each time, which is a control that works only
// while somebody is looking. This sweep looks.
//
// WHAT IT FLAGS. One boolean expression, outside the file that declares the
// predicates, whose operands name TWO OR MORE distinct suppression signals. Two
// signals in one condition is a hand-built gate; one signal beside an ordinary
// operand (a count, a date, a settings flag) is a surface asking its own
// question and stays legal — flagging those would red every honest call site
// and the barrier would be deleted within a week.
//
// WHAT IT CANNOT SEE, stated here and repeated in the failure text. A
// recombination assembled through intermediate variables — `disabled := …` on
// one line and `disabled || awaiting` on the next — reads as one signal per
// expression and passes. So does one built in a template: the owner templates
// receive PredictionDisabled and no second signal, so today a template CANNOT
// spell a second disjunct, and the day one is handed over is the day this sweep
// stops covering the surface that reads it.
var predictionSuppressionSignals = map[string]string{
	"PredictionDisabled":            "unpredictable-cycle mode",
	"DashboardPredictionDisabled":   "unpredictable-cycle mode",
	"PregnancyPaused":               "pregnancy pause",
	"DashboardCycleOverdue":         "cycle overdue past its own reference length",
	"AwaitingFirstCycle":            "the zero-completed-cycle floor",
	"DashboardAwaitingFirstCycle":   "the zero-completed-cycle floor",
	"PredictionsSuppressed":         "the whole-projection gate",
	"FertilitySuppressed":           "the fertility gate",
	"FertilityProjectionSuppressed": "the fertility gate",
}

// predictionSuppressionPredicateFile declares the predicates, so it is the one
// file whose whole job is to combine them.
const predictionSuppressionPredicateFile = "dashboard_cycle.go"

// predictionSuppressionSweptPackages are the layers a display decision can be
// made in: the business layer that resolves it and the transport layer that
// publishes it.
var predictionSuppressionSweptPackages = []string{
	"internal/services",
	"internal/api",
}

// predictionSuppressionResiduals are the sites that combine two signals ON
// PURPOSE, each with the reason it is not the class above. It is a ratchet, not
// an allowlist: a site may only leave this map, and a new entry needs the same
// kind of reason written beside it.
var predictionSuppressionResiduals = map[string]string{
	"internal/services/dashboard_view_service.go:resolveDashboardTimingFrame": "" +
		"the bridge line names no date, so it is not a claim any gate withholds: it asks whether the " +
		"account has predictions at all, and it cannot read the fertility gate because the first-cycle " +
		"floor IS the state it is shown in — a gate carrying that floor would gate the bridge on itself",
}

// TestNoSurfaceRecombinesTheSuppressionSignals fails when a file outside the
// predicate file builds a suppression gate out of two or more signals.
func TestNoSurfaceRecombinesTheSuppressionSignals(t *testing.T) {
	root := predictionSuppressionRepoRoot(t)

	var findings []string
	scanned := 0
	for _, pkg := range predictionSuppressionSweptPackages {
		files, hits := predictionSuppressionScanPackage(t, root, pkg)
		scanned += files
		findings = append(findings, hits...)
	}
	// The anchor counts FILES, not findings: a clean tree is a state this sweep
	// must be able to report, but a sweep that read nothing at all would report
	// it too, and the two must not look alike.
	if scanned == 0 {
		t.Fatal("the sweep parsed no Go file — its verdict is about a tree nobody looked at")
	}

	var unexplained []string
	for _, finding := range findings {
		if _, sanctioned := predictionSuppressionResiduals[predictionSuppressionKey(finding)]; sanctioned {
			continue
		}
		unexplained = append(unexplained, finding)
	}
	if len(unexplained) > 0 {
		sort.Strings(unexplained)
		t.Fatalf("a suppression gate is being rebuilt from its disjuncts outside %s — call PredictionsSuppressed or FertilityProjectionSuppressed, or read the decision the cycle context already resolved (docs/SECURITY_INVARIANTS.md -> medical safety):\n%s\n(the sweep reads ONE boolean expression at a time: a recombination split across intermediate variables, or assembled in a template, is invisible to it)", predictionSuppressionPredicateFile, strings.Join(unexplained, "\n"))
	}

	// A residual that no longer describes a site is a lie the next reader
	// inherits, so the ratchet is checked in both directions.
	present := make(map[string]bool, len(findings))
	for _, finding := range findings {
		present[predictionSuppressionKey(finding)] = true
	}
	for key := range predictionSuppressionResiduals {
		if !present[key] {
			t.Fatalf("residual %q names a recombination that is no longer there — drop the entry rather than leaving the map describing a tree that has moved on", key)
		}
	}
}

// TestPredictionSuppressionBarrierClassifiesItsOwnFixtures proves the sweep can
// report both verdicts, on sources the test owns rather than on the tree it
// judges.
func TestPredictionSuppressionBarrierClassifiesItsOwnFixtures(t *testing.T) {
	const recombined = `package fixture

func gate(ctx C, stats S) bool {
	return ctx.PredictionDisabled || stats.PregnancyPaused
}
`
	const single = `package fixture

func gate(ctx C, count int) bool {
	return ctx.PredictionDisabled || count == 0
}

func resolved(ctx C) bool { return !ctx.FertilitySuppressed }
`
	const negated = `package fixture

func bridge(user U, stats S) bool {
	return !DashboardPredictionDisabled(user) && DashboardAwaitingFirstCycle(stats)
}
`

	if hits := predictionSuppressionScanSource(t, "fix/recombined.go", recombined); len(hits) != 1 {
		t.Fatalf("two signals in one expression must be reported exactly once, got %d: %v", len(hits), hits)
	}
	if hits := predictionSuppressionScanSource(t, "fix/single.go", single); len(hits) != 0 {
		t.Fatalf("one signal beside an ordinary operand, and a resolved decision read on its own, must both pass: %v", hits)
	}
	// A negation is how the sanctioned site is actually written, so the fixture
	// pins that a `!` does not hide a signal from the count.
	if hits := predictionSuppressionScanSource(t, "fix/negated.go", negated); len(hits) != 1 {
		t.Fatalf("a negated signal is still that signal — expected one finding, got %d: %v", len(hits), hits)
	}
}

// predictionSuppressionKey reduces a finding line to its file:function key, the
// shape the residual map is written in.
func predictionSuppressionKey(finding string) string {
	trimmed := strings.TrimSpace(finding)
	if space := strings.Index(trimmed, " "); space > 0 {
		return trimmed[:space]
	}
	return trimmed
}

// predictionSuppressionScanPackage reports how many non-test files it parsed
// and one finding line per recombination in them.
func predictionSuppressionScanPackage(t *testing.T, root string, pkg string) (int, []string) {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(pkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", pkg, err)
	}

	parsed := 0
	var findings []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == predictionSuppressionPredicateFile {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s/%s: %v", pkg, name, err)
		}
		parsed++
		findings = append(findings, predictionSuppressionScanSource(t, pkg+"/"+name, string(source))...)
	}
	if parsed == 0 {
		t.Fatalf("%s yielded no non-test Go file — the sweep read nothing and its verdict about this package is vacuous", pkg)
	}
	return parsed, findings
}

// predictionSuppressionScanSource reports one line per boolean expression in
// the file naming two or more distinct suppression signals, keyed by the
// enclosing function so a residual survives the line moving.
func predictionSuppressionScanSource(t *testing.T, display string, source string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, display, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", display, err)
	}

	var findings []string
	nested := make(map[ast.Node]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary || (binary.Op != token.LAND && binary.Op != token.LOR) {
			return true
		}
		if nested[node] {
			return true
		}
		// The whole boolean tree is one gate, so the OUTERMOST expression is the
		// finding and its operands are marked off: reporting an inner `a || b`
		// beside the `(a || b) && c` that contains it would name one defect
		// twice and leave the residual map ambiguous about which it forgives.
		ast.Inspect(binary, func(inner ast.Node) bool {
			if inner != node {
				nested[inner] = true
			}
			return true
		})
		signals := predictionSuppressionSignalsIn(binary)
		if len(signals) < 2 {
			return true
		}
		sort.Strings(signals)
		findings = append(findings, fmt.Sprintf(
			"  %s:%s  line %d combines %s",
			display,
			predictionSuppressionEnclosing(file, binary.Pos()),
			fileSet.Position(binary.Pos()).Line,
			strings.Join(signals, " + "),
		))
		return true
	})
	return findings
}

// predictionSuppressionSignalsIn lists the distinct signals named anywhere in
// one boolean expression, each under the meaning it carries rather than under
// the spelling it was written with: the field and the function answer the same
// question, and counting them apart would let a gate rebuilt out of both halves
// read as two ordinary operands.
func predictionSuppressionSignalsIn(expr ast.Expr) []string {
	distinct := make(map[string]bool)
	ast.Inspect(expr, func(node ast.Node) bool {
		var name string
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			name = typed.Sel.Name
		case *ast.Ident:
			name = typed.Name
		}
		if meaning, isSignal := predictionSuppressionSignals[name]; isSignal {
			distinct[meaning] = true
		}
		return true
	})
	signals := make([]string, 0, len(distinct))
	for meaning := range distinct {
		signals = append(signals, meaning)
	}
	return signals
}

// predictionSuppressionEnclosing names the function a position sits in, or the
// file scope when it sits outside every function body.
func predictionSuppressionEnclosing(file *ast.File, pos token.Pos) string {
	name := "(file scope)"
	for _, decl := range file.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if !isFunction || pos < function.Pos() || pos > function.End() {
			continue
		}
		name = function.Name.Name
	}
	return name
}

func predictionSuppressionRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the working directory — the sweep cannot find the module root")
		}
		dir = parent
	}
}
