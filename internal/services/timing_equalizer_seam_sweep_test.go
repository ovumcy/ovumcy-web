package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Sweep for the equalizer class WEB-56 found three members of.
//
// A timing equalizer declared as a swappable var is tested by swapping it, and
// a test that swaps the whole var never runs its body — so an emptied body
// passes. The members found so far (login, registration, calendar feed) now
// spend through a seam var the tests can wrap while the shipped body runs. This
// sweep holds the rule for members added later: every package-level
// `var equalize…Timing = func…` must call at least one package-level seam var,
// every seam var it calls must be reassigned in some _test.go of this package,
// and its body must not call bcrypt.CompareHashAndPassword or
// VerifyCalendarFeedToken directly.
//
// What it cannot see, stated so its name is not read as more: an equalizer
// declared with `func` rather than as a var (equalizeRecoveryCodeLookupTiming —
// not swappable, and pinned to its literal bcrypt calls by
// TestRecoveryLookupSpendsBothCredentialComparesWithoutShortCircuit), a var not
// named equalize…Timing, and a body that reaches a primitive through some other
// helper. The direct-call list is closed at the two primitives above, so a body
// that calls a seam and also calls another primitive directly (for example
// security.VerifyCalendarFeedVerifierMAC) passes. A seam counts as tested when
// some test reassigns it, not when that test asserts what the body spends
// through it — that is the per-member body tests' job. The members it must find
// are asserted by name below, so the sweep cannot pass by matching nothing.

var timingEqualizerVarName = regexp.MustCompile(`^equalize\w*Timing$`)

// isDirectTimingPrimitiveCall recognises the calls an equalizer body must make
// through a seam instead: the bcrypt compare and the calendar-feed verify path.
func isDirectTimingPrimitiveCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := fun.X.(*ast.Ident)
		return ok && pkg.Name == "bcrypt" && fun.Sel.Name == "CompareHashAndPassword"
	case *ast.Ident:
		return fun.Name == "VerifyCalendarFeedToken"
	}
	return false
}

// timingEqualizerScan is what the production files contribute to the sweep.
type timingEqualizerScan struct {
	found      []string
	offenders  []string
	packageVar map[string]bool
	// calledIdents maps each equalizer to the bare identifiers its body calls;
	// those naming a package-level var are its seams.
	calledIdents map[string][]string
}

func newTimingEqualizerScan() timingEqualizerScan {
	return timingEqualizerScan{packageVar: map[string]bool{}, calledIdents: map[string][]string{}}
}

// scanTimingEqualizers adds file's package-level vars and equalizer bodies to scan.
func scanTimingEqualizers(scan *timingEqualizerScan, fileSet *token.FileSet, file *ast.File) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				scan.packageVar[name.Name] = true
				if !timingEqualizerVarName.MatchString(name.Name) || index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.FuncLit)
				if !ok {
					continue
				}
				scan.found = append(scan.found, name.Name)
				ast.Inspect(literal.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					if isDirectTimingPrimitiveCall(call) {
						scan.offenders = append(scan.offenders, name.Name+" at "+fileSet.Position(call.Pos()).String())
					}
					if ident, ok := call.Fun.(*ast.Ident); ok {
						scan.calledIdents[name.Name] = append(scan.calledIdents[name.Name], ident.Name)
					}
					return true
				})
			}
		}
	}
}

// addReassignedIdents adds every bare identifier assigned with `=` in file.
func addReassignedIdents(assigned map[string]bool, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN {
			return true
		}
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok {
				assigned[ident.Name] = true
			}
		}
		return true
	})
}

// timingEqualizerSeamFailures requires each equalizer to call at least one
// package-level var, and every one it calls to be reassigned by a test.
func timingEqualizerSeamFailures(scan timingEqualizerScan, testAssigned map[string]bool) []string {
	var failures []string
	for _, equalizer := range scan.found {
		seams := 0
		for _, called := range scan.calledIdents[equalizer] {
			if !scan.packageVar[called] {
				continue
			}
			seams++
			if !testAssigned[called] {
				failures = append(failures, equalizer+" spends through "+called+", which no test reassigns")
			}
		}
		if seams == 0 {
			failures = append(failures, equalizer+" calls no package-level seam var")
		}
	}
	sort.Strings(failures)
	return failures
}

// TestTimingEqualizerSweepClassifiesOwnedFixtures anchors the classifier on
// inputs this test owns, so the sweep below cannot go vacuous if the package's
// own members change shape.
func TestTimingEqualizerSweepClassifiesOwnedFixtures(t *testing.T) {
	const source = `package fixture
var wrappedSeam = bcrypt.CompareHashAndPassword
var unwrappedSeam = bcrypt.CompareHashAndPassword
var equalizeDirectTiming = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
var equalizeFeedDirectTiming = func(k []byte, s, v string) { _ = VerifyCalendarFeedToken(k, s+v, x) }
var equalizeSeamedTiming = func(p string) { _ = wrappedSeam(nil, []byte(p)) }
var equalizeUntestedSeamTiming = func(p string) { _ = unwrappedSeam(nil, []byte(p)) }
var equalizeOtherPrimitiveTiming = func(k []byte) { _ = security.VerifyCalendarFeedVerifierMAC(k, "", "") }
var notAnEqualizer = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
`
	const testSource = `package fixture
func TestWraps(t *testing.T) { wrappedSeam = func([]byte, []byte) error { return nil } }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	testFile, err := parser.ParseFile(fileSet, "fixture_test.go", testSource, 0)
	if err != nil {
		t.Fatalf("parse test fixture: %v", err)
	}
	scan := newTimingEqualizerScan()
	scanTimingEqualizers(&scan, fileSet, file)
	testAssigned := map[string]bool{}
	addReassignedIdents(testAssigned, testFile)

	wantFound := "equalizeDirectTiming,equalizeFeedDirectTiming,equalizeSeamedTiming,equalizeUntestedSeamTiming,equalizeOtherPrimitiveTiming"
	if strings.Join(scan.found, ",") != wantFound {
		t.Fatalf("classifier found %v, want exactly the five equalize…Timing vars", scan.found)
	}
	if len(scan.offenders) != 2 || !strings.HasPrefix(scan.offenders[0], "equalizeDirectTiming ") || !strings.HasPrefix(scan.offenders[1], "equalizeFeedDirectTiming ") {
		t.Fatalf("classifier flagged %v, want the direct bcrypt and the direct feed-verify bodies only", scan.offenders)
	}
	wantFailures := []string{
		"equalizeDirectTiming calls no package-level seam var",
		"equalizeFeedDirectTiming calls no package-level seam var",
		"equalizeOtherPrimitiveTiming calls no package-level seam var",
		"equalizeUntestedSeamTiming spends through unwrappedSeam, which no test reassigns",
	}
	if failures := timingEqualizerSeamFailures(scan, testAssigned); strings.Join(failures, "\n") != strings.Join(wantFailures, "\n") {
		t.Fatalf("seam judgement = %q, want %q", failures, wantFailures)
	}
}

// TestTimingEqualizerVarsSpendThroughASeam is the sweep over the shipped
// package.
func TestTimingEqualizerVarsSpendThroughASeam(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fileSet := token.NewFileSet()
	scan := newTimingEqualizerScan()
	testAssigned := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if strings.HasSuffix(name, "_test.go") {
			addReassignedIdents(testAssigned, file)
			continue
		}
		scanTimingEqualizers(&scan, fileSet, file)
	}

	found := map[string]bool{}
	for _, name := range scan.found {
		found[name] = true
	}
	for _, want := range []string{"equalizeAuthCredentialsTiming", "equalizeCalendarFeedTiming", "equalizeRegistrationTiming"} {
		if !found[want] {
			t.Fatalf("the sweep did not find %s among %v — it is no longer measuring the members it exists for", want, scan.found)
		}
	}
	if len(scan.offenders) != 0 {
		t.Fatalf("timing equalizer vars call the expensive primitive directly: %v. A test that swaps the var never runs such a body, "+
			"so emptying it passes the suite; spend through a seam var (authTimingEqualizerCompare, calendarFeedEqualizerVerify) and test the body through it",
			scan.offenders)
	}
	if failures := timingEqualizerSeamFailures(scan, testAssigned); len(failures) != 0 {
		t.Fatalf("timing equalizer seams no test drives: %v. A seam no test wraps leaves the body it serves emptyable with the suite green",
			failures)
	}
}
