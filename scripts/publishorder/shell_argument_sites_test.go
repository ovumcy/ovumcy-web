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

// dashCLetters matches a shell flag cluster that carries `c` — `-c`, and
// `-ec` or `-lc` as well, which hand the shell its script as an argument the
// same way. Bash and sh accept most consonants as single-character options
// (`-c`, `-e`, `-x`, `-u`, `-o`, `-n`, `-t`, `-r`, `-a`, `-v`, ...), the same
// letters unrelated Go-tool flags draw from (`-count`, `-cover`,
// `-coverprofile`, `-exec`, `-race`), so the letters alone cannot tell a real
// cluster from one of those, and neither can its length: `-euxvc` is a real
// five-letter cluster. goToolFlagWords names the words exempted instead, so an
// unknown word fails closed. `-race` and `-exec` stay out of it: each IS a
// lexically valid bash cluster (`-r -a -c -e`), and a guard that stayed quiet
// on one would be worse than the false positive — the reviewer reads the
// offending exec.Command line and sees which one it is.
var dashCLetters = regexp.MustCompile(`^-[A-Za-z]*c[A-Za-z]*$`)

// goToolFlagWords are Go-tool flags dashCLetters matches that no shell
// invocation under scripts/ spells.
var goToolFlagWords = map[string]bool{
	"-count":        true,
	"-cover":        true,
	"-covermode":    true,
	"-coverpkg":     true,
	"-coverprofile": true,
}

// isDashCFlag reports whether value is a shell flag cluster carrying `c`
// rather than one of goToolFlagWords.
func isDashCFlag(value string) bool {
	return dashCLetters.MatchString(value) && !goToolFlagWords[value]
}

// dashCAllowed names every site under scripts/ that may hand a shell its
// command as an argument, keyed `<package directory>.<function>` or, for a
// method, `<package directory>.<receiver type>.<method>` — a bare function
// name would collide two methods of the same name on different receivers
// under one key. Each entry allows exactly one `-c` site: a second one inside
// an already-allowed function is a fresh offender, not a second instance of
// the one reviewed. Each runs a fixed one-liner the test wrote itself, never
// a script read out of a workflow or a document: short enough that no
// command-line limit reaches it, and not a step whose shell and flags it
// could misstate.
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

	var sites []dashCSite
	for _, pkg := range pkgOrder {
		files := filesByPkg[pkg]
		consts := packageConsts(files)
		for _, file := range files {
			sites = append(sites, dashCSitesIn(fset, file, pkg, consts)...)
		}
	}

	offenders, found := classifyDashCSites(sites, dashCAllowed)
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

// classifyDashCSites sorts sites into offenders and the allowed entries they
// used. An allowed entry answers for exactly one site: a second site under a
// key already used is an offender, same as a site under a key allowed never.
func classifyDashCSites(sites []dashCSite, allowed map[string]string) (offenders []string, found map[string]bool) {
	found = map[string]bool{}
	used := map[string]bool{}
	for _, site := range sites {
		if _, ok := allowed[site.function]; ok {
			if used[site.function] {
				offenders = append(offenders, site.function+" at "+site.position+" — a second `-c` site in a function already allowed one")
				continue
			}
			used[site.function] = true
			found[site.function] = true
			continue
		}
		offenders = append(offenders, site.function+" at "+site.position)
	}
	return offenders, found
}

// TestClassifyDashCSitesAllowsExactlyOneSitePerEntry proves the allowlist
// consumption directly, on data this test owns rather than on whatever the
// real tree happens to hold today: a first `-c` site in an allowed function
// is exempt, a second in that same function is an offender, and a site under
// a key the allowlist never named is an offender from the start.
func TestClassifyDashCSitesAllowsExactlyOneSitePerEntry(t *testing.T) {
	allowed := map[string]string{"pkg.probe": "a fixed one-liner"}
	sites := []dashCSite{
		{function: "pkg.probe", position: "probe.go:1"},
		{function: "pkg.probe", position: "probe.go:2"},
		{function: "pkg.other", position: "other.go:1"},
	}

	offenders, found := classifyDashCSites(sites, allowed)

	if !found["pkg.probe"] {
		t.Error("classifyDashCSites did not mark pkg.probe found on its first, allowed site")
	}
	if len(offenders) != 2 {
		t.Fatalf("classifyDashCSites returned %d offenders, want 2 (the second pkg.probe site and pkg.other): %q", len(offenders), offenders)
	}
	if !strings.Contains(offenders[0], "probe.go:2") {
		t.Errorf("offenders[0] = %q, want it to name the second pkg.probe site (probe.go:2)", offenders[0])
	}
	if !strings.Contains(offenders[1], "pkg.other") {
		t.Errorf("offenders[1] = %q, want it to name pkg.other, which the allowlist never named", offenders[1])
	}
}

// TestDashCSitesInClassifiesBothWays feeds the scanner a source this test owns,
// so its verdict does not rest on the tree it judges: a plain `-c`, a cluster
// inside a function literal and behind a wrapper, a flag held in a variable, a
// slice, a constant or built by concatenation are found under the function
// that spells or uses them, a package-level one under the package, two
// methods of the same name on different receivers are told apart, a named
// Go-tool flag word is not found, `-race` and a five-letter cluster are, and a
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

type Runner struct{}

type OtherRunner struct{}

func (Runner) exec(bash, script string) { _ = exec.Command(bash, "-c", script) }

func (OtherRunner) exec(bash, script string) { _ = exec.Command(bash, "-c", script) }

func viaGoFlags(bash string) { _ = exec.Command("go", "test", "-coverprofile", "-count", "-cover") }

func viaRace(bash string) { _ = exec.Command("go", "test", "-race") }

func viaLongCluster(bash, script string) { _ = exec.Command(bash, "-euxvc", script) }

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
		"fixture.Runner.exec",
		"fixture.OtherRunner.exec",
		"fixture.viaRace",
		"fixture.viaLongCluster",
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

// funcOwner names the site a FuncDecl's body belongs to: `<pkg>.<function>`,
// or `<pkg>.<receiver type>.<method>` for a method, so that two methods
// named alike on different receivers do not share one dashCAllowed key.
func funcOwner(pkg string, function *ast.FuncDecl) string {
	if function.Recv != nil && len(function.Recv.List) > 0 {
		if recv := receiverTypeName(function.Recv.List[0].Type); recv != "" {
			return pkg + "." + recv + "." + function.Name.Name
		}
	}
	return pkg + "." + function.Name.Name
}

// receiverTypeName returns a method receiver's type name, unwrapping the
// pointer a `*T` receiver carries.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	default:
		return ""
	}
}

// dashCSitesIn returns every `-c` flag file spells or uses, named
// `<pkg>.<enclosing function>` (`<pkg>.<receiver>.<method>` for a method), or
// `<pkg>.<package level>` for one declared outside every function. A flag
// inside a function literal belongs to the declaration that holds it. It
// checks a const or var's own initializer, an assignment's right side, a call
// argument and a composite literal's elements — every shape the doc comment
// above names — folding each through foldConstString first, so a flag built
// by concatenation or held in a named constant is found the same as a plain
// literal.
func dashCSitesIn(fset *token.FileSet, file *ast.File, pkg string, consts map[string]string) []dashCSite {
	var sites []dashCSite
	check := func(owner string, expr ast.Expr) {
		if value, ok := foldConstString(expr, consts); ok && isDashCFlag(value) {
			sites = append(sites, dashCSite{function: owner, position: fset.Position(expr.Pos()).String()})
		}
	}
	for _, decl := range file.Decls {
		owner := pkg + "." + packageLevel
		if function, ok := decl.(*ast.FuncDecl); ok {
			owner = funcOwner(pkg, function)
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
