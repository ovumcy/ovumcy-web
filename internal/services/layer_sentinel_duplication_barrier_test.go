package services

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Barrier for the duplicated cross-layer sentinel class.
//
// internal/services does not import internal/db — doing so would drag the
// driver and the migration set into the business layer's build graph — so a
// sentinel that both layers need has historically been declared twice, once per
// package, with byte-identical text. Two separate errors.New values are never
// errors.Is-equal, so a caller testing the services-level name against an error
// the repository raised gets false for an error whose message is
// character-for-character the one it asked about. It reads as a working
// comparison in every review and in every log line.
//
// The one home both layers already import is internal/models, and a sentinel
// that lives there is a single value each layer re-exports under its own name.
// This sweep reads the shipped source rather than a list of known sites, so a
// sentinel added later is covered the moment it is written.
//
// internal/models is swept alongside the two layers on purpose, and it is the
// half that is easy to leave out. Once a value has moved there, re-splitting it
// takes only ONE side: a layer that goes back to its own errors.New with the
// same text is the whole defect again, while the other layer's re-export is not
// an errors.New at all and a db-versus-services comparison sees a single
// declaration. Every pair of the three packages is therefore checked.
//
// What it cannot see, stated here and repeated in the failure text: it reads
// package-level `var … = errors.New("literal")` declarations only. A sentinel
// assembled with fmt.Errorf, built from a constant, or declared in some fourth
// package is invisible to it, as is an errors.New inside a function body —
// which is not a sentinel at all, since no caller can hold its identity.
var layerSentinelBarrierPackages = []string{
	"internal/models",
	"internal/db",
	"internal/services",
}

// TestNoErrorSentinelTextIsDeclaredInMoreThanOneLayer fails when the same
// sentinel text is declared by errors.New in two of the swept packages,
// whichever of them added it.
func TestNoErrorSentinelTextIsDeclaredInMoreThanOneLayer(t *testing.T) {
	root := layerSentinelBarrierRepoRoot(t)

	// scanPackage fails on a package it could not read a single source file
	// from, so a moved directory or a renamed package cannot turn this into a
	// clean verdict about a tree nobody looked at. The anchor counts FILES
	// rather than sentinels on purpose: a layer that legitimately declares none
	// is a state this tree can reach, and an anchor conditioned on the data
	// being judged stops firing on the day that data empties.
	scanned := make(map[string]map[string]string, len(layerSentinelBarrierPackages))
	for _, pkg := range layerSentinelBarrierPackages {
		scanned[pkg] = layerSentinelBarrierScanPackage(t, root, pkg)
	}

	var duplicates []string
	for first := range layerSentinelBarrierPackages {
		for second := first + 1; second < len(layerSentinelBarrierPackages); second++ {
			duplicates = append(duplicates, layerSentinelBarrierDuplicates(
				scanned[layerSentinelBarrierPackages[first]],
				scanned[layerSentinelBarrierPackages[second]],
			)...)
		}
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		t.Fatalf("sentinel text declared separately in two packages, so errors.Is across the layer boundary is false for the very error being tested for — keep one value in internal/models and re-export it from each layer:\n%s\n(the sweep sees package-level `var … = errors.New(\"literal\")` only)", strings.Join(duplicates, "\n"))
	}
}

// TestLayerSentinelBarrierClassifiesItsOwnFixtures proves the sweep can report
// both verdicts, on sources the test owns rather than on the tree it judges.
func TestLayerSentinelBarrierClassifiesItsOwnFixtures(t *testing.T) {
	const shared = `package fixture

import "errors"

var ErrShared = errors.New("shared sentinel text")
`
	const distinct = `package fixture

import "errors"

var ErrDistinct = errors.New("some other text")

func caller() error { return errors.New("shared sentinel text") }
`

	duplicated := layerSentinelBarrierScanSource(t, "a/shared.go", shared)
	other := layerSentinelBarrierScanSource(t, "b/shared.go", shared)
	if hits := layerSentinelBarrierDuplicates(duplicated, other); len(hits) != 1 {
		t.Fatalf("two packages declaring the same sentinel text must report exactly one duplicate, got %d: %v", len(hits), hits)
	}

	clean := layerSentinelBarrierScanSource(t, "b/distinct.go", distinct)
	if hits := layerSentinelBarrierDuplicates(duplicated, clean); len(hits) != 0 {
		t.Fatalf("a distinct sentinel plus a function-local errors.New must report no duplicate, got %v", hits)
	}
	if _, found := clean["shared sentinel text"]; found {
		t.Fatal("a function-local errors.New was recorded as a sentinel — it has no identity a caller can hold, and counting it would flag safe code")
	}
}

// layerSentinelBarrierDuplicates returns one line per text declared in both
// scanned packages, each naming both declaration sites.
func layerSentinelBarrierDuplicates(first map[string]string, second map[string]string) []string {
	var duplicates []string
	for text, siteA := range first {
		siteB, found := second[text]
		if !found {
			continue
		}
		duplicates = append(duplicates, fmt.Sprintf("  %q\n    %s\n    %s", text, siteA, siteB))
	}
	return duplicates
}

// layerSentinelBarrierScanPackage maps sentinel text to its declaration site
// for every non-test file of one package.
func layerSentinelBarrierScanPackage(t *testing.T, root string, pkg string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(pkg)))
	if err != nil {
		t.Fatalf("read %s: %v", pkg, err)
	}

	sentinels := make(map[string]string)
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(pkg), name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		parsed++
		for text, site := range layerSentinelBarrierScanSource(t, pkg+"/"+name, string(source)) {
			sentinels[text] = site
		}
	}
	if parsed == 0 {
		t.Fatalf("%s yielded no non-test Go file — the sweep read nothing and its verdict about this package is vacuous", pkg)
	}
	return sentinels
}

// layerSentinelBarrierScanSource records the text of every package-level
// `var … = errors.New("literal")` in one file, keyed by that text.
func layerSentinelBarrierScanSource(t *testing.T, display string, source string) map[string]string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, display, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", display, err)
	}

	sentinels := make(map[string]string)
	for _, decl := range file.Decls {
		general, isGeneral := decl.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for index, expr := range value.Values {
				text, isSentinel := layerSentinelBarrierErrorText(expr)
				if !isSentinel {
					continue
				}
				position := fileSet.Position(expr.Pos())
				name := "?"
				if index < len(value.Names) {
					name = value.Names[index].Name
				}
				sentinels[text] = fmt.Sprintf("%s:%d %s", display, position.Line, name)
			}
		}
	}
	return sentinels
}

// layerSentinelBarrierErrorText reports the literal handed to errors.New, if
// the expression is exactly that call on a plain string literal.
func layerSentinelBarrierErrorText(expr ast.Expr) (string, bool) {
	call, isCall := expr.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return "", false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "New" {
		return "", false
	}
	pkg, isIdent := selector.X.(*ast.Ident)
	if !isIdent || pkg.Name != "errors" {
		return "", false
	}
	literal, isLiteral := call.Args[0].(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return text, true
}

func layerSentinelBarrierRepoRoot(t *testing.T) string {
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
