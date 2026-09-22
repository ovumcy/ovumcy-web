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
// `var equalize…Timing = func…` must not call the expensive primitive directly.
//
// What it cannot see, stated so its name is not read as more: an equalizer
// declared with `func` rather than as a var (equalizeRecoveryCodeLookupTiming —
// not swappable, and pinned to its literal bcrypt calls by
// TestRecoveryLookupSpendsBothCredentialComparesWithoutShortCircuit), a var not
// named equalize…Timing, and a body that reaches the primitive through some
// other helper. The members it must find are asserted by name below, so the
// sweep cannot pass by matching nothing.

var timingEqualizerVarName = regexp.MustCompile(`^equalize\w*Timing$`)

// timingEqualizerDirectPrimitives are the calls an equalizer body must make
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

// timingEqualizerOffenders returns every equalizer var found in file, and the
// subset whose body calls a timing primitive directly.
func timingEqualizerOffenders(fileSet *token.FileSet, file *ast.File) (found []string, offenders []string) {
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
				if !timingEqualizerVarName.MatchString(name.Name) || index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.FuncLit)
				if !ok {
					continue
				}
				found = append(found, name.Name)
				ast.Inspect(literal.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok && isDirectTimingPrimitiveCall(call) {
						offenders = append(offenders, name.Name+" at "+fileSet.Position(call.Pos()).String())
					}
					return true
				})
			}
		}
	}
	return found, offenders
}

// TestTimingEqualizerSweepClassifiesOwnedFixtures anchors the classifier on
// inputs this test owns, so the sweep below cannot go vacuous if the package's
// own members change shape.
func TestTimingEqualizerSweepClassifiesOwnedFixtures(t *testing.T) {
	const source = `package fixture
var equalizeDirectTiming = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
var equalizeFeedDirectTiming = func(k []byte, s, v string) { _ = VerifyCalendarFeedToken(k, s+v, x) }
var equalizeSeamedTiming = func(p string) { _ = someSeam(nil, []byte(p)) }
var notAnEqualizer = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	found, offenders := timingEqualizerOffenders(fileSet, file)

	if strings.Join(found, ",") != "equalizeDirectTiming,equalizeFeedDirectTiming,equalizeSeamedTiming" {
		t.Fatalf("classifier found %v, want exactly the three equalize…Timing vars", found)
	}
	if len(offenders) != 2 || !strings.HasPrefix(offenders[0], "equalizeDirectTiming ") || !strings.HasPrefix(offenders[1], "equalizeFeedDirectTiming ") {
		t.Fatalf("classifier flagged %v, want the direct bcrypt and the direct feed-verify bodies only", offenders)
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
	var found, offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		fileFound, fileOffenders := timingEqualizerOffenders(fileSet, file)
		found = append(found, fileFound...)
		offenders = append(offenders, fileOffenders...)
	}

	sort.Strings(found)
	for _, want := range []string{"equalizeAuthCredentialsTiming", "equalizeCalendarFeedTiming", "equalizeRegistrationTiming"} {
		if index := sort.SearchStrings(found, want); index >= len(found) || found[index] != want {
			t.Fatalf("the sweep did not find %s among %v — it is no longer measuring the members it exists for", want, found)
		}
	}
	if len(offenders) != 0 {
		t.Fatalf("timing equalizer vars call the expensive primitive directly: %v. A test that swaps the var never runs such a body, "+
			"so emptying it passes the suite; spend through a seam var (authTimingEqualizerCompare, calendarFeedEqualizerVerify) and test the body through it",
			offenders)
	}
}
