package services

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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
// WHAT IT CANNOT SEE, stated here and repeated in the failure text: a
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
// file whose whole job is to combine them. It is matched by its full path from
// the module root, not by base name: a same-named file in some later subpackage
// is not this one, and exempting it by name would hand the class a hiding place.
const predictionSuppressionPredicateFile = "internal/services/dashboard_cycle.go"

// predictionSuppressionSweptTrees is every package the binary is built from,
// walked to its leaves.
//
// Naming the two layers a display decision is made in TODAY — services and api —
// would have been a sweep about this file layout rather than about the class:
// DashboardCycleContext and both predicates are exported, so any package can
// read them, and a decision that moves to a third one takes the barrier's
// silence with it. The same reasoning retires the one-directory-deep read a
// package list invites.
var predictionSuppressionSweptTrees = []string{
	"internal",
	"cmd",
}

// predictionSuppressionResidual is one sanctioned site: the signals the entry
// was written ABOUT, and why that particular combination is not the class above.
//
// The signals are part of the match, not decoration. An entry that matched on
// the site alone would forgive whatever single recombination happened to be
// standing there — so a refactor that turns the sanctioned line into a predicate
// call and gives the same function a NEW gate would inherit the exemption
// written for the old one, with the reason beside it now describing nothing.
type predictionSuppressionResidual struct {
	signals string
	reason  string
}

// predictionSuppressionResiduals are the sites that combine two signals ON
// PURPOSE. It is a ratchet, not an allowlist: an entry may only leave this map,
// a new one needs the same kind of reason written beside it, and an entry
// forgives exactly ONE recombination — of exactly its own signals — in the
// function it names.
var predictionSuppressionResiduals = map[string]predictionSuppressionResidual{
	"internal/services/dashboard_view_service.go:resolveDashboardTimingFrame": {
		signals: "the zero-completed-cycle floor + unpredictable-cycle mode",
		reason: "" +
			"the bridge line names no date, so it is not a claim any gate withholds: it asks whether the " +
			"account has predictions at all, and it cannot read the fertility gate because the first-cycle " +
			"floor IS the state it is shown in — a gate carrying that floor would gate the bridge on itself",
	},
}

// predictionSuppressionFinding is one recombination: the site that owns it, the
// line it sits on, and the signals it combines. The key is carried as a field
// rather than parsed back out of the printed line — a key recovered from prose
// breaks on the first site whose name holds a space, and the sanctioned-site
// lookup would then miss silently.
type predictionSuppressionFinding struct {
	key     string
	line    int
	signals []string
}

// combined is the finding's signals in the one spelling a residual is written
// in, so the entry and the site are compared as the same kind of thing.
func (finding predictionSuppressionFinding) combined() string {
	return strings.Join(finding.signals, " + ")
}

func (finding predictionSuppressionFinding) String() string {
	return fmt.Sprintf("  %s  line %d combines %s", finding.key, finding.line, finding.combined())
}

// TestNoSurfaceRecombinesTheSuppressionSignals fails when a file outside the
// predicate file builds a suppression gate out of two or more signals.
func TestNoSurfaceRecombinesTheSuppressionSignals(t *testing.T) {
	root := predictionSuppressionRepoRoot(t)

	var findings []predictionSuppressionFinding
	scanned := 0
	for _, tree := range predictionSuppressionSweptTrees {
		files, hits := predictionSuppressionScanTree(t, root, tree)
		scanned += files
		findings = append(findings, hits...)
	}
	// The anchor counts FILES, not findings: a clean tree is a state this sweep
	// must be able to report, but a sweep that read nothing at all would report
	// it too, and the two must not look alike.
	if scanned == 0 {
		t.Fatal("the sweep parsed no Go file — its verdict is about a tree nobody looked at")
	}

	perSite := make(map[string]int, len(findings))
	for _, finding := range findings {
		perSite[finding.key]++
	}

	var unexplained []string
	for _, finding := range findings {
		sanctioned, hasEntry := predictionSuppressionResiduals[finding.key]
		if hasEntry && sanctioned.signals == finding.combined() && perSite[finding.key] == 1 {
			continue
		}
		unexplained = append(unexplained, finding.String())
	}
	if len(unexplained) > 0 {
		sort.Strings(unexplained)
		t.Fatalf("a suppression gate is being rebuilt from its disjuncts outside %s — call PredictionsSuppressed or FertilityProjectionSuppressed, or read the decision the cycle context already resolved (docs/SECURITY_INVARIANTS.md -> medical safety):\n%s\n(a residual forgives ONE recombination in the function it names, so a second one there lands here beside it; and the sweep reads ONE boolean expression at a time, so a recombination split across intermediate variables, or assembled in a template, is invisible to it)", predictionSuppressionPredicateFile, strings.Join(unexplained, "\n"))
	}

	// A residual that no longer describes a site is a lie the next reader
	// inherits, so the ratchet is checked in both directions — and against the
	// SIGNALS, since a site whose combination changed is a different gate under
	// an old exemption, not the one the reason beside it explains.
	matched := make(map[string]bool, len(predictionSuppressionResiduals))
	for _, finding := range findings {
		if sanctioned, hasEntry := predictionSuppressionResiduals[finding.key]; hasEntry && sanctioned.signals == finding.combined() {
			matched[finding.key] = true
		}
	}
	for key, sanctioned := range predictionSuppressionResiduals {
		if !matched[key] {
			t.Fatalf("residual %q no longer describes a site combining %s — drop the entry, or write the one the tree now holds, rather than leaving the map describing a tree that has moved on", key, sanctioned.signals)
		}
	}
}

// TestTheSweepKnowsEverySignalThePredicatesDisjoin ties the list above to the
// predicates themselves.
//
// The list is a second spelling of what PredictionsSuppressed and
// FertilityProjectionSuppressed already disjoin, and a second spelling that
// nothing compares is exactly the failure this barrier exists to prevent, one
// level up: a fifth signal added to a predicate would leave the sweep counting
// it as an ordinary operand, so a surface combining it with a known signal would
// read as naming one signal and pass. The predicate file is read either way, so
// the comparison costs one parse.
func TestTheSweepKnowsEverySignalThePredicatesDisjoin(t *testing.T) {
	root := predictionSuppressionRepoRoot(t)

	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(predictionSuppressionPredicateFile)))
	if err != nil {
		t.Fatalf("read %s: %v", predictionSuppressionPredicateFile, err)
	}
	fileSet := token.NewFileSet()
	file, parseErr := parser.ParseFile(fileSet, predictionSuppressionPredicateFile, source, 0)
	if parseErr != nil {
		t.Fatalf("parse %s: %v", predictionSuppressionPredicateFile, parseErr)
	}

	for _, predicate := range []string{"PredictionsSuppressed", "FertilityProjectionSuppressed"} {
		disjuncts := predictionSuppressionDisjunctsOf(t, file, predicate)
		// Anchored on the shape rather than on a count that would freeze the
		// predicate: a return that stopped being a disjunction, or a renamed
		// predicate, must not read as "nothing to check".
		if len(disjuncts) < 2 {
			t.Fatalf("%s returned %d disjunct(s) — either it no longer combines the signals, or the sweep is reading the wrong function, and its verdict about the signal list is vacuous", predicate, len(disjuncts))
		}
		for _, name := range disjuncts {
			if _, known := predictionSuppressionSignals[name]; !known {
				t.Fatalf("%s disjoins %q, which predictionSuppressionSignals does not list — the sweep would read it as an ordinary operand, so a surface combining it with a known signal passes silently. Add it, under the meaning it carries", predicate, name)
			}
		}
	}
}

// predictionSuppressionDisjunctsOf names the head of every operand of the
// boolean expression one predicate returns: the function called, or the field
// read. Those heads are the signals, in the spelling the sweep matches on.
func predictionSuppressionDisjunctsOf(t *testing.T, file *ast.File, predicate string) []string {
	t.Helper()

	for _, decl := range file.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != predicate || function.Body == nil {
			continue
		}
		var heads []string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			ret, isReturn := node.(*ast.ReturnStmt)
			if !isReturn || len(ret.Results) != 1 {
				return true
			}
			for _, operand := range predictionSuppressionOperands(ret.Results[0]) {
				if head := predictionSuppressionHead(operand); head != "" {
					heads = append(heads, head)
				}
			}
			return true
		})
		return heads
	}
	t.Fatalf("%s is not declared in %s — the predicates moved and this check is about a function nobody has", predicate, predictionSuppressionPredicateFile)
	return nil
}

// predictionSuppressionOperands flattens a boolean expression into its leaves.
func predictionSuppressionOperands(expr ast.Expr) []ast.Expr {
	switch typed := expr.(type) {
	case *ast.BinaryExpr:
		if typed.Op != token.LAND && typed.Op != token.LOR {
			return []ast.Expr{expr}
		}
		return append(predictionSuppressionOperands(typed.X), predictionSuppressionOperands(typed.Y)...)
	case *ast.ParenExpr:
		return predictionSuppressionOperands(typed.X)
	case *ast.UnaryExpr:
		return predictionSuppressionOperands(typed.X)
	default:
		return []ast.Expr{expr}
	}
}

// predictionSuppressionHead is the name one operand is built on.
func predictionSuppressionHead(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.CallExpr:
		return predictionSuppressionHead(typed.Fun)
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.Ident:
		return typed.Name
	default:
		return ""
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
	const twiceInOneFunction = `package fixture

func gate(ctx C, stats S) bool {
	if ctx.PredictionDisabled || stats.PregnancyPaused {
		return true
	}
	return ctx.AwaitingFirstCycle && !ctx.PredictionDisabled
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
	// Two gates in one function share a key, which is what a residual is keyed
	// by: they must arrive as two findings, or the count that keeps one entry
	// from forgiving both has nothing to count.
	hits := predictionSuppressionScanSource(t, "fix/twice.go", twiceInOneFunction)
	if len(hits) != 2 {
		t.Fatalf("two separate gates in one function must be reported twice, got %d: %v", len(hits), hits)
	}
	if hits[0].key != hits[1].key {
		t.Fatalf("both gates sit in the same function, so they must share a key: %q vs %q", hits[0].key, hits[1].key)
	}

	// Same method name, different receivers: one site each, or neither can be
	// sanctioned without forgiving the other.
	const sameNameTwoTypes = `package fixture

func (v dashboardView) render(stats S) bool { return v.PredictionDisabled || stats.PregnancyPaused }

func (s *statsView) render(stats S) bool { return s.AwaitingFirstCycle || stats.PregnancyPaused }
`
	methods := predictionSuppressionScanSource(t, "fix/methods.go", sameNameTwoTypes)
	if len(methods) != 2 {
		t.Fatalf("both methods recombine, so both must be reported, got %d: %v", len(methods), methods)
	}
	if methods[0].key == methods[1].key {
		t.Fatalf("methods on different types are different sites — the receiver belongs in the key: %q", methods[0].key)
	}
	if methods[0].key != "fix/methods.go:dashboardView.render" || methods[1].key != "fix/methods.go:statsView.render" {
		t.Fatalf("a method's key names its receiver type: %q, %q", methods[0].key, methods[1].key)
	}
}

// TestPredictionSuppressionBarrierReadsATreeToItsLeaves walks a synthesized tree
// rather than the repository's, because what it pins is the sweep's REACH: the
// first spelling read one directory deep, so a display file moved into a
// subpackage would have gone unread while every anchor still reported a tree
// that had been looked at.
func TestPredictionSuppressionBarrierReadsATreeToItsLeaves(t *testing.T) {
	const clean = `package top

func gate(ctx C, count int) bool { return ctx.PredictionDisabled || count == 0 }
`
	const recombined = `package nested

func gate(ctx C, stats S) bool { return ctx.PredictionDisabled || stats.PregnancyPaused }
`
	root := t.TempDir()
	predictionSuppressionWriteFixture(t, root, "internal/services/top.go", clean)
	predictionSuppressionWriteFixture(t, root, "internal/services/nested/deep.go", recombined)
	// testdata holds inputs, not surfaces: a recombination written there is a
	// fixture deciding nothing, and Go does not build it.
	predictionSuppressionWriteFixture(t, root, "internal/services/testdata/sample.go", recombined)

	parsed, findings := predictionSuppressionScanTree(t, root, "internal/services")
	if parsed != 2 {
		t.Fatalf("the two buildable files must both be parsed and testdata skipped, parsed %d", parsed)
	}
	if len(findings) != 1 {
		t.Fatalf("the subpackage recombination must be the one finding, got %d: %v", len(findings), findings)
	}
	if findings[0].key != "internal/services/nested/deep.go:gate" {
		t.Fatalf("a finding is keyed by its path from the module root: %q", findings[0].key)
	}
}

func predictionSuppressionWriteFixture(t *testing.T, root string, relative string, source string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(relative), err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

// predictionSuppressionScanTree reports how many non-test files it parsed under
// one tree and one finding per recombination in them.
func predictionSuppressionScanTree(t *testing.T, root string, tree string) (int, []predictionSuppressionFinding) {
	t.Helper()

	parsed := 0
	var findings []predictionSuppressionFinding
	walkErr := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(tree)), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// testdata holds inputs, not surfaces, and Go itself does not build
			// it — a fixture there deciding nothing must not read as a defect.
			if entry.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		display := filepath.ToSlash(relative)
		if display == predictionSuppressionPredicateFile {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		parsed++
		findings = append(findings, predictionSuppressionScanSource(t, display, string(source))...)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", tree, walkErr)
	}
	if parsed == 0 {
		t.Fatalf("%s yielded no non-test Go file — the sweep read nothing and its verdict about this tree is vacuous", tree)
	}
	return parsed, findings
}

// predictionSuppressionScanSource reports one finding per boolean expression in
// the file naming two or more distinct suppression signals, keyed by the
// enclosing function so a residual survives the line moving.
func predictionSuppressionScanSource(t *testing.T, display string, source string) []predictionSuppressionFinding {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, display, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", display, err)
	}

	var findings []predictionSuppressionFinding
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
		// twice, and the per-site count below would read one gate as two.
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
		findings = append(findings, predictionSuppressionFinding{
			key:     display + ":" + predictionSuppressionEnclosing(file, binary.Pos()),
			line:    fileSet.Position(binary.Pos()).Line,
			signals: signals,
		})
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
// file scope when it sits outside every function body. The placeholder carries
// no space: a key is compared whole, and one that reads as two words invites a
// residual written the natural way and matched by nothing.
//
// A METHOD is named with its receiver type. Without it, two same-named methods
// on different types in one file share a key: the per-site count then reads two
// unrelated gates as one site twice over, no residual can ever apply to either,
// and the only way to green the tree is to rename a method — a barrier whose
// correct state is unreachable is the one that gets deleted rather than obeyed.
func predictionSuppressionEnclosing(file *ast.File, pos token.Pos) string {
	name := "file-scope"
	for _, decl := range file.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if !isFunction || pos < function.Pos() || pos > function.End() {
			continue
		}
		name = function.Name.Name
		if receiver := predictionSuppressionReceiver(function); receiver != "" {
			name = receiver + "." + name
		}
	}
	return name
}

// predictionSuppressionReceiver is the receiver type's name, pointer or value,
// or the empty string for a plain function.
func predictionSuppressionReceiver(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	expr := function.Recv.List[0].Type
	if star, isPointer := expr.(*ast.StarExpr); isPointer {
		expr = star.X
	}
	// A generic receiver is written `T[P]`; the type is the thing being indexed,
	// and the parameter says nothing about which declaration this is.
	if index, isIndexed := expr.(*ast.IndexExpr); isIndexed {
		expr = index.X
	}
	if ident, isIdent := expr.(*ast.Ident); isIdent {
		return ident.Name
	}
	return ""
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
