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

// packageLevel names the owner of a flag literal declared outside every
// function. dashCAllowed never holds it: a package-level flag is reachable from
// any function in the package, so no one probe can answer for it.
const packageLevel = "<package level>"

// dashCSite is one `-c` flag literal, named by the function it is written in.
type dashCSite struct {
	function string
	position string
}

// TestNoExtractedScriptIsHandedToAShellAsAnArgument holds every package under
// scripts/ — the harnesses that run a workflow step's script or the
// self-hosting runbook's commands, and any added later — to running such a
// script from a file. Handed over as an argument, a long script truncates
// silently on Windows, and it runs under whatever flags the call site spelled
// rather than the ones the step declares. The scan keys on the flag literal
// wherever it is written — a call argument, a variable, a constant, a slice of
// arguments — not on `exec.Command` alone, so neither a wrapper that forwards
// its arguments to exec nor a flag held in a name escapes it.
func TestNoExtractedScriptIsHandedToAShellAsAnArgument(t *testing.T) {
	scripts, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve scripts/: %v", err)
	}
	if filepath.Base(scripts) != "scripts" {
		t.Fatalf("this package's parent is %s, not scripts/, so the scan would judge the wrong tree", scripts)
	}

	fset := token.NewFileSet()
	found := map[string]bool{}
	var offenders []string

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
		for _, site := range dashCSitesIn(fset, file, filepath.ToSlash(pkg)) {
			if _, ok := dashCAllowed[site.function]; ok {
				found[site.function] = true
				continue
			}
			offenders = append(offenders, site.function+" at "+site.position)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("scan scripts/: %v", walkErr)
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
// inside a function literal and behind a wrapper, a flag held in a variable or
// a slice are found under the function that spells them, a package-level
// constant under the package, and a script run from a file under `--norc` is
// not found at all.
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

func viaFile(bash, file string) { _ = exec.Command(bash, "--noprofile", "--norc", "-eo", "pipefail", file) }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse the fixture: %v", err)
	}

	var got []string
	for _, site := range dashCSitesIn(fset, file, "fixture") {
		got = append(got, site.function)
	}
	want := "fixture.viaArgument fixture.viaCluster fixture.viaVariable fixture.viaSlice fixture." + packageLevel
	if strings.Join(got, " ") != want {
		t.Errorf("dashCSitesIn found %q, want %q", got, want)
	}
}

// dashCSitesIn returns every string literal in file that spells a `-c` flag,
// named `<pkg>.<enclosing function>`, or `<pkg>.<package level>` for one
// declared outside every function. A literal inside a function literal belongs
// to the declaration that holds it.
func dashCSitesIn(fset *token.FileSet, file *ast.File, pkg string) []dashCSite {
	var sites []dashCSite
	for _, decl := range file.Decls {
		owner := pkg + "." + packageLevel
		if function, ok := decl.(*ast.FuncDecl); ok {
			owner = pkg + "." + function.Name.Name
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(literal.Value); err == nil && dashCFlag.MatchString(value) {
				sites = append(sites, dashCSite{
					function: owner,
					position: fset.Position(literal.Pos()).String(),
				})
			}
			return true
		})
	}
	return sites
}
