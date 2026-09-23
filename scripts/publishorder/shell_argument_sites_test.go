package publishorder

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// dashCFlag matches a shell flag cluster that carries `c` — `-c`, and `-ec` or
// `-lc` as well, which hand the shell its script as an argument the same way.
var dashCFlag = regexp.MustCompile(`^-[A-Za-z]*c[A-Za-z]*$`)

// dashCAllowed names every function under scripts/ that may hand a shell its
// command as an argument, keyed `<package directory>.<function>`. Each runs a
// fixed one-liner the test wrote itself, never a script read out of a workflow
// or a document: short enough that no command-line limit reaches it, and not a
// step whose shell and flags it could misstate.
var dashCAllowed = map[string]string{
	"publishorder.requireBash":      "probes that bash answers `printf ok`",
	"publishorder.requireShellTool": "probes one tool through the shell with a one-line command",
	"backuprestoredoc.writeVolume":  "a fixed `sh -c` inside the throwaway container that fills a volume",
	"backuprestoredoc.readVolume":   "a fixed `sh -c` inside the throwaway container that reads a volume",
	"ciguards.requireBash":          "probes that bash can cd into the fixture and run git",
	"ciguards.requireJq":            "probes that jq answers from inside bash with a one-line filter",
}

// packageLevel names the owner of a flag declared outside every function.
// dashCAllowed never holds it: a package-level flag is reachable from any
// function in the package, so no one probe can answer for it.
const packageLevel = "<package level>"

// dashCSite is one `-c` flag, named by the function it is written or used in.
type dashCSite struct {
	function string
	position string
}

// TestNoExtractedScriptIsHandedToAShellAsAnArgument holds every package under
// scripts/ — the harnesses that run a workflow step's script or the
// self-hosting runbook's commands, and any added later — to running such a
// script from a file. Handed over as an argument, a long script truncates
// silently on Windows, and it runs under whatever flags the call site spelled
// rather than the ones the step declares. The scan keys on the flag wherever
// it is written — a call argument, a variable, a constant, a slice of
// arguments, one built by concatenation — not on `exec.Command` alone, so
// neither a wrapper that forwards its arguments to exec nor a flag held in a
// name or an expression escapes it.
func TestNoExtractedScriptIsHandedToAShellAsAnArgument(t *testing.T) {
	scripts, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve scripts/: %v", err)
	}
	if filepath.Base(scripts) != "scripts" {
		t.Fatalf("this package's parent is %s, not scripts/, so the scan would judge the wrong tree", scripts)
	}

	fset := token.NewFileSet()
	// filesByPkg groups every parsed file by the package directory that
	// holds it: a top-level const or var is visible package-wide, wherever
	// in the package its identifier is used, so folding one at a use site
	// needs every file of the package read first, not just the one holding
	// the use.
	filesByPkg := map[string][]*ast.File{}
	var pkgOrder []string

	walkErr := filepath.WalkDir(scripts, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Fixture sources a package parses as data, which the go tool
			// never builds or runs.
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg, err := filepath.Rel(scripts, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg = filepath.ToSlash(pkg)
		if _, seen := filesByPkg[pkg]; !seen {
			pkgOrder = append(pkgOrder, pkg)
		}
		filesByPkg[pkg] = append(filesByPkg[pkg], file)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("scan scripts/: %v", walkErr)
	}

	found := map[string]bool{}
	var offenders []string
	for _, pkg := range pkgOrder {
		files := filesByPkg[pkg]
		consts := packageConsts(files)
		for _, file := range files {
			for _, site := range dashCSitesIn(fset, file, pkg, consts) {
				if _, ok := dashCAllowed[site.function]; ok {
					found[site.function] = true
					continue
				}
				offenders = append(offenders, site.function+" at "+site.position)
			}
		}
	}

	if len(offenders) > 0 {
		t.Errorf("these sites spell the `-c` flag that hands a shell its script as an argument:\n  %s\nWrite the script to a file and run that file under the flags its step declares, as runBashScript here, runGate in releasegate and runScript in backuprestoredoc do.",
			strings.Join(offenders, "\n  "))
	}

	names := make([]string, 0, len(dashCAllowed))
	for name := range dashCAllowed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !found[name] {
			t.Errorf("%s is allowed a `-c` argument (%s) and no longer passes one, or was renamed: drop the entry, so the list names only sites that exist", name, dashCAllowed[name])
		}
	}
}

// TestDashCSitesInClassifiesBothWays feeds the scanner a source this test owns,
// so its verdict does not rest on the tree it judges: a plain `-c`, a cluster
// inside a function literal and behind a wrapper, a flag held in a variable, a
// slice, a constant or built by concatenation are found under the function
// that spells or uses them, a package-level one under the package, and a
// script run from a file under `--norc` is not found at all.
func TestDashCSitesInClassifiesBothWays(t *testing.T) {
	const source = `package fixture

import "os/exec"

func viaArgument(bash, script string) { _ = exec.Command(bash, "-c", script) }

func viaCluster(bash, script string) { go func() { _ = run(bash, "-ec", script) }() }

func viaVariable(bash, script string) {
	flag := "-c"
	_ = exec.Command(bash, flag, script)
}

func viaSlice(bash, script string) {
	args := []string{"-lc", script}
	_ = exec.Command(bash, args...)
}

const shellFlag = "-c"

func viaConstant(bash, script string) { _ = exec.Command(bash, shellFlag, script) }

func viaConcatenation(bash, script string) { _ = exec.Command(bash, "-"+"c", script) }

const concatFlag = ("-" + "c")

func viaConcatConstant(bash, script string) { _ = exec.Command(bash, concatFlag, script) }

func viaFile(bash, file string) { _ = exec.Command(bash, "--noprofile", "--norc", "-eo", "pipefail", file) }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse the fixture: %v", err)
	}

	consts := packageConsts([]*ast.File{file})
	var got []string
	for _, site := range dashCSitesIn(fset, file, "fixture", consts) {
		got = append(got, site.function)
	}
	want := strings.Join([]string{
		"fixture.viaArgument",
		"fixture.viaCluster",
		"fixture.viaVariable",
		"fixture.viaSlice",
		"fixture." + packageLevel,
		"fixture.viaConstant",
		"fixture.viaConcatenation",
		"fixture." + packageLevel,
		"fixture.viaConcatConstant",
	}, " ")
	if strings.Join(got, " ") != want {
		t.Errorf("dashCSitesIn found %q, want %q", got, want)
	}
}

// packageConsts folds every top-level const and var in files whose
// initializer is a single constant string expression — a literal, one built
// by `+` concatenation, or parenthesized — into a name-to-value table, so
// that dashCSitesIn can resolve an identifier used anywhere in the package
// back to the flag it names. Folded over a few passes so a const that refers
// to another declared later, or earlier, in the package still resolves.
func packageConsts(files []*ast.File) map[string]string {
	type binding struct {
		name  string
		value ast.Expr
	}
	var bindings []binding
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || (genDecl.Tok != token.CONST && genDecl.Tok != token.VAR) {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok || len(valueSpec.Names) != 1 || len(valueSpec.Values) != 1 {
					continue
				}
				bindings = append(bindings, binding{name: valueSpec.Names[0].Name, value: valueSpec.Values[0]})
			}
		}
	}

	consts := map[string]string{}
	for range len(bindings) + 1 {
		added := false
		for _, b := range bindings {
			if _, ok := consts[b.name]; ok {
				continue
			}
			if value, ok := foldConstString(b.value, consts); ok {
				consts[b.name] = value
				added = true
			}
		}
		if !added {
			break
		}
	}
	return consts
}

// foldConstString reduces expr to a string value if it is a literal, a `+`
// concatenation of foldable expressions, a parenthesized foldable
// expression, or an identifier bound in consts — the shapes a flag escapes
// a literal-only scan through.
func foldConstString(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(e.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return foldConstString(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := foldConstString(e.X, consts)
		if !ok {
			return "", false
		}
		right, ok := foldConstString(e.Y, consts)
		if !ok {
			return "", false
		}
		return left + right, true
	case *ast.Ident:
		value, ok := consts[e.Name]
		return value, ok
	default:
		return "", false
	}
}

// dashCSitesIn returns every `-c` flag file spells or uses, named
// `<pkg>.<enclosing function>`, or `<pkg>.<package level>` for one declared
// outside every function. A flag inside a function literal belongs to the
// declaration that holds it. It checks a const or var's own initializer, an
// assignment's right side, a call argument and a composite literal's
// elements — every shape the doc comment above names — folding each through
// foldConstString first, so a flag built by concatenation or held in a named
// constant is found the same as a plain literal.
func dashCSitesIn(fset *token.FileSet, file *ast.File, pkg string, consts map[string]string) []dashCSite {
	var sites []dashCSite
	check := func(owner string, expr ast.Expr) {
		if value, ok := foldConstString(expr, consts); ok && dashCFlag.MatchString(value) {
			sites = append(sites, dashCSite{function: owner, position: fset.Position(expr.Pos()).String()})
		}
	}
	for _, decl := range file.Decls {
		owner := pkg + "." + packageLevel
		if function, ok := decl.(*ast.FuncDecl); ok {
			owner = pkg + "." + function.Name.Name
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.ValueSpec:
				for _, value := range n.Values {
					check(owner, value)
				}
			case *ast.AssignStmt:
				for _, rhs := range n.Rhs {
					check(owner, rhs)
				}
			case *ast.CallExpr:
				for _, arg := range n.Args {
					check(owner, arg)
				}
			case *ast.CompositeLit:
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						check(owner, kv.Value)
						continue
					}
					check(owner, elt)
				}
			}
			return true
		})
	}
	return sites
}
