package services

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Sweep for the equalizer class WEB-56 found three members of.
//
// A timing equalizer declared as a swappable var is tested by swapping it, and
// a test that swaps the whole var never runs its body — so an emptied body
// passes. The members found so far (asserted by name below) now spend through
// a seam var the tests can wrap while the shipped body runs. This
// sweep holds the rule for members added later. Every package-level
// `var equalize…Timing = func…`, and every top-level `func equalize…Timing` —
// not swappable, so a direct primitive call in it could only be read off the
// source text, never observed — must:
//   - call at least one timing seam: a package-level var whose production
//     value is a timing primitive (bcrypt.CompareHashAndPassword, resolved by
//     import path so an aliased import counts, or VerifyCalendarFeedToken);
//   - have every timing seam it calls reassigned in some _test.go of this
//     package, outside any function that declares a local of the same name;
//   - not reference a timing primitive directly, called or bound to a local.
//
// And no non-test file may write a timing seam or take its address, anywhere
// in the file (timingSeamProductionWrites): the body tests swap the
// seam themselves, so a production assignment to a no-op would leave every
// equalizer spending nothing with all of them green.
//
// What it cannot see, stated so its name is not read as more: an equalizer not
// named equalize…Timing, a method, and a body that reaches a primitive through
// some other helper. The primitive list is closed at the two above: a body that calls a
// timing seam and also calls another primitive directly (for example
// security.VerifyCalendarFeedVerifierMAC) passes, and an equalizer whose only
// spend is a new primitive fails until the list names it. A seam counts as
// tested when some test reassigns it, not when that test asserts what the body
// spends through it — that is the per-member body tests' job. The members it
// must find are asserted by name below, so the sweep cannot pass by matching
// nothing.

var timingEqualizerVarName = regexp.MustCompile(`^equalize\w*Timing$`)

const bcryptImportPath = "golang.org/x/crypto/bcrypt"

// bcryptLocalNames returns the names file binds golang.org/x/crypto/bcrypt to.
func bcryptLocalNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != bcryptImportPath {
			continue
		}
		if spec.Name != nil {
			names[spec.Name.Name] = true
		} else {
			names["bcrypt"] = true
		}
	}
	return names
}

// isTimingPrimitiveRef recognises a reference to a timing primitive: the bcrypt
// compare under whatever name the file imports it, and the calendar-feed verify
// path.
func isTimingPrimitiveRef(expr ast.Expr, bcryptNames map[string]bool) bool {
	switch ref := expr.(type) {
	case *ast.SelectorExpr:
		pkg, ok := ref.X.(*ast.Ident)
		return ok && bcryptNames[pkg.Name] && ref.Sel.Name == "CompareHashAndPassword"
	case *ast.Ident:
		return ref.Name == "VerifyCalendarFeedToken"
	case *ast.ParenExpr:
		return isTimingPrimitiveRef(ref.X, bcryptNames)
	}
	return false
}

// localNames returns every name node declares below itself: function
// parameters and results, `:=` targets, and var/const specs.
func localNames(node ast.Node) map[string]bool {
	names := map[string]bool{}
	addFields := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				names[name.Name] = true
			}
		}
	}
	ast.Inspect(node, func(child ast.Node) bool {
		switch decl := child.(type) {
		case *ast.FuncType:
			addFields(decl.Params)
			addFields(decl.Results)
		case *ast.AssignStmt:
			if decl.Tok == token.DEFINE {
				for _, lhs := range decl.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						names[ident.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range decl.Names {
				names[name.Name] = true
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{decl.Key, decl.Value} {
				if ident, ok := expr.(*ast.Ident); ok && decl.Tok == token.DEFINE {
					names[ident.Name] = true
				}
			}
		}
		return true
	})
	return names
}

// timingEqualizerScan is what the production files contribute to the sweep.
type timingEqualizerScan struct {
	found     []string
	offenders []string
	// timingSeam holds the package-level vars whose production value is a
	// timing primitive.
	timingSeam map[string]bool
	// calledIdents maps each equalizer to the bare, unshadowed identifiers its
	// body calls; those naming a timing seam are its seams.
	calledIdents map[string][]string
}

func newTimingEqualizerScan() timingEqualizerScan {
	return timingEqualizerScan{timingSeam: map[string]bool{}, calledIdents: map[string][]string{}}
}

// scanTimingEqualizers adds file's timing seams and equalizer bodies to scan.
func scanTimingEqualizers(scan *timingEqualizerScan, fileSet *token.FileSet, file *ast.File) {
	bcryptNames := bcryptLocalNames(file)
	scanBody := func(name string, function ast.Node, body *ast.BlockStmt) {
		scan.found = append(scan.found, name)
		shadowed := localNames(function)
		ast.Inspect(body, func(node ast.Node) bool {
			if expr, ok := node.(ast.Expr); ok && isTimingPrimitiveRef(expr, bcryptNames) {
				scan.offenders = append(scan.offenders, name+" at "+fileSet.Position(expr.Pos()).String())
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && !shadowed[ident.Name] {
				scan.calledIdents[name] = append(scan.calledIdents[name], ident.Name)
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok {
			if function.Recv == nil && function.Body != nil && timingEqualizerVarName.MatchString(function.Name.Name) {
				scanBody(function.Name.Name, function, function.Body)
			}
			continue
		}
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
				if index >= len(value.Values) {
					continue
				}
				if isTimingPrimitiveRef(value.Values[index], bcryptNames) {
					scan.timingSeam[name.Name] = true
				}
				if !timingEqualizerVarName.MatchString(name.Name) {
					continue
				}
				if literal, ok := value.Values[index].(*ast.FuncLit); ok {
					scanBody(name.Name, literal, literal.Body)
				}
			}
		}
	}
}

// addReassignedIdents adds every bare identifier file assigns with `=`, except
// inside a function that declares a local of that name.
func addReassignedIdents(assigned map[string]bool, file *ast.File) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		shadowed := localNames(function)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ASSIGN {
				return true
			}
			for _, lhs := range assign.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && !shadowed[ident.Name] {
					assigned[ident.Name] = true
				}
			}
			return true
		})
	}
}

// timingSeamProductionWrites names every place file writes a timing seam or
// takes its address: anywhere in the file, including a func literal in a
// package-level var initializer, which addReassignedIdents does not walk.
// Shadowing is deliberately ignored — a production local named after a seam
// is refused too, loudly, rather than let a real write hide behind it.
func timingSeamProductionWrites(fileSet *token.FileSet, file *ast.File, seams map[string]bool) []string {
	var writes []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch expr := node.(type) {
		case *ast.AssignStmt:
			if expr.Tok != token.ASSIGN {
				return true
			}
			for _, lhs := range expr.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && seams[ident.Name] {
					writes = append(writes, ident.Name+" at "+fileSet.Position(ident.Pos()).String())
				}
			}
		case *ast.UnaryExpr:
			if ident, ok := expr.X.(*ast.Ident); ok && expr.Op == token.AND && seams[ident.Name] {
				writes = append(writes, ident.Name+" at "+fileSet.Position(ident.Pos()).String())
			}
		}
		return true
	})
	return writes
}

// TestTimingSeamProductionWritesClassifiesOwnedFixtures anchors the
// production-write check on inputs this test owns.
func TestTimingSeamProductionWritesClassifiesOwnedFixtures(t *testing.T) {
	const source = `package fixture
var seam = bcrypt.CompareHashAndPassword
var other = 1
func init() { seam = nil }
var _ = func() bool { seam = nil; return true }()
var _ = &seam
func notASeamWrite() { other = 2; x := seam; _ = x }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	var lines []string
	for _, write := range timingSeamProductionWrites(fileSet, file, map[string]bool{"seam": true}) {
		parts := strings.Split(write, ":")
		lines = append(lines, strings.Join(parts[len(parts)-2:], ":"))
	}
	if got, want := strings.Join(lines, ","), "4:15,5:23,6:10"; got != want {
		t.Fatalf("production writes found at line:column %s, want %s — the init body, the var-initializer literal and the address-of", got, want)
	}
}

// timingEqualizerSeamFailures requires each equalizer to call at least one
// timing seam, and every one it calls to be reassigned by a test.
func timingEqualizerSeamFailures(scan timingEqualizerScan, testAssigned map[string]bool) []string {
	var failures []string
	for _, equalizer := range scan.found {
		seams := 0
		for _, called := range scan.calledIdents[equalizer] {
			if !scan.timingSeam[called] {
				continue
			}
			seams++
			if !testAssigned[called] {
				failures = append(failures, equalizer+" spends through "+called+", which no test reassigns")
			}
		}
		if seams == 0 {
			failures = append(failures, equalizer+" calls no timing seam")
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
import (
	"golang.org/x/crypto/bcrypt"
	bc "golang.org/x/crypto/bcrypt"
)
var wrappedSeam = bcrypt.CompareHashAndPassword
var unwrappedSeam = bc.CompareHashAndPassword
var shadowTestedSeam = VerifyCalendarFeedToken
var clockSeam = time.Now
var equalizeDirectTiming = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
var equalizeFeedDirectTiming = func(k []byte, s, v string) { _ = VerifyCalendarFeedToken(k, s+v, x) }
var equalizeAliasedDirectTiming = func(p string) { _ = wrappedSeam(nil, nil); _ = bc.CompareHashAndPassword(nil, []byte(p)) }
var equalizeLocalAliasTiming = func(p string) { f := bcrypt.CompareHashAndPassword; _ = wrappedSeam(nil, nil); _ = f(nil, []byte(p)) }
var equalizeSeamedTiming = func(p string) { _ = wrappedSeam(nil, []byte(p)) }
var equalizeUntestedSeamTiming = func(p string) { _ = unwrappedSeam(nil, []byte(p)) }
var equalizeShadowTestedSeamTiming = func(k []byte) { _ = shadowTestedSeam(k, "", x) }
var equalizeClockOnlyTiming = func(p string) { _ = clockSeam() }
var equalizeShadowedSeamTiming = func(wrappedSeam func([]byte, []byte) error) { _ = wrappedSeam(nil, nil) }
var equalizeOtherPrimitiveTiming = func(k []byte) { _ = security.VerifyCalendarFeedVerifierMAC(k, "", "") }
func equalizeFuncDirectTiming(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
func equalizeFuncSeamedTiming(p string) { _ = wrappedSeam(nil, []byte(p)) }
func (s S) equalizeMethodTiming(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
var notAnEqualizer = func(p string) { _ = bcrypt.CompareHashAndPassword(nil, []byte(p)) }
`
	const testSource = `package fixture
func TestWraps(t *testing.T) { wrappedSeam = func([]byte, []byte) error { return nil }; clockSeam = nil }
func TestShadows(t *testing.T) { var shadowTestedSeam func(); shadowTestedSeam = nil }
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

	wantFound := "equalizeDirectTiming,equalizeFeedDirectTiming,equalizeAliasedDirectTiming,equalizeLocalAliasTiming," +
		"equalizeSeamedTiming,equalizeUntestedSeamTiming,equalizeShadowTestedSeamTiming,equalizeClockOnlyTiming," +
		"equalizeShadowedSeamTiming,equalizeOtherPrimitiveTiming,equalizeFuncDirectTiming,equalizeFuncSeamedTiming"
	if strings.Join(scan.found, ",") != wantFound {
		t.Fatalf("classifier found %v, want exactly the fixture's equalize…Timing vars and top-level funcs", scan.found)
	}
	var offenderNames []string
	for _, offender := range scan.offenders {
		offenderNames = append(offenderNames, strings.Fields(offender)[0])
	}
	wantOffenders := "equalizeDirectTiming,equalizeFeedDirectTiming,equalizeAliasedDirectTiming,equalizeLocalAliasTiming,equalizeFuncDirectTiming"
	if strings.Join(offenderNames, ",") != wantOffenders {
		t.Fatalf("classifier flagged %v, want the direct, feed-direct, aliased-import, local-alias and func-direct bodies only", offenderNames)
	}
	wantFailures := []string{
		"equalizeClockOnlyTiming calls no timing seam",
		"equalizeDirectTiming calls no timing seam",
		"equalizeFeedDirectTiming calls no timing seam",
		"equalizeFuncDirectTiming calls no timing seam",
		"equalizeOtherPrimitiveTiming calls no timing seam",
		"equalizeShadowTestedSeamTiming spends through shadowTestedSeam, which no test reassigns",
		"equalizeShadowedSeamTiming calls no timing seam",
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
	var productionFiles []*ast.File
	var testFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			testFiles = append(testFiles, name)
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanTimingEqualizers(&scan, fileSet, file)
		productionFiles = append(productionFiles, file)
	}
	// Seams are collected across every file first: a write may sit in a file
	// parsed before the one that declares the seam.
	var productionWrites []string
	for _, file := range productionFiles {
		productionWrites = append(productionWrites, timingSeamProductionWrites(fileSet, file, scan.timingSeam)...)
	}
	if len(productionWrites) != 0 {
		t.Fatalf("production code writes a timing seam or takes its address: %v. The equalizer body tests swap the seam themselves, "+
			"so a production no-op would leave every equalizer spending nothing with the suite green", productionWrites)
	}

	// Only a test file that names a timing seam can reassign one; parsing the
	// rest would find nothing.
	testAssigned := map[string]bool{}
	for _, name := range testFiles {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		namesSeam := false
		for seam := range scan.timingSeam {
			if bytes.Contains(content, []byte(seam)) {
				namesSeam = true
				break
			}
		}
		if !namesSeam {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, content, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		addReassignedIdents(testAssigned, file)
	}

	found := map[string]bool{}
	for _, name := range scan.found {
		found[name] = true
	}
	for _, want := range []string{"equalizeAuthCredentialsTiming", "equalizeCalendarFeedTiming", "equalizeRecoveryCodeLookupTiming", "equalizeRegistrationTiming", "equalizeSettingsReauthTiming"} {
		if !found[want] {
			t.Fatalf("the sweep did not find %s among %v — it is no longer measuring the members it exists for", want, scan.found)
		}
	}
	for _, seam := range []string{"authTimingEqualizerCompare", "calendarFeedEqualizerVerify"} {
		if !scan.timingSeam[seam] {
			t.Fatalf("the sweep does not recognise %s as a timing seam — its production value is no longer a primitive it knows", seam)
		}
	}
	if len(scan.offenders) != 0 {
		t.Fatalf("timing equalizers reference the expensive primitive directly: %v. A test that swaps the var never runs such a body, "+
			"and a func-declared one can only be read, never observed; spend through a seam var (authTimingEqualizerCompare, calendarFeedEqualizerVerify) and test the body through it",
			scan.offenders)
	}
	if failures := timingEqualizerSeamFailures(scan, testAssigned); len(failures) != 0 {
		t.Fatalf("timing equalizer seams no test drives: %v. A seam no test wraps leaves the body it serves emptyable with the suite green",
			failures)
	}
}
