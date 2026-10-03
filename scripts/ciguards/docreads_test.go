package ciguards

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A docs-only diff clears the Go lanes, which is correct for text no test reads
// and wrong for a document one does: such a diff reaches `main` with every Go
// lane skipped, and the break surfaces on the next unrelated pull request or
// after a tag has published. The detect step's allowlist names what is inert;
// this file holds it to the other side of that sentence — a path a Go test
// reads is not inert.
//
// The read set is derived from the tests, never listed here: a test added
// tomorrow that reads another document is covered the day it is written. The
// verdict comes from the real detect step run over a one-file diff, not from a
// copy of its pattern, so a filter reworked into another shape is judged by what
// it does.

// docReadSkipDir reports the directories no test source of this repository lives
// in: VCS and dependency trees, recorded fixtures, and agent worktrees.
func docReadSkipDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." || name == "node_modules" || name == "testdata"
}

// testPathReferences returns every repo-relative path an `_test.go` file under
// root names and that exists in the tree, mapped to the test files naming it. A
// path is named by a string literal or by a filepath.Join/path.Join whose
// literal segments spell it, with leading `..` segments dropped (a test reads
// `../../docs/openapi.yaml` from its own directory). The set over-approximates
// what is READ — a fixture named after a real document counts — and that is the
// safe direction for a rule that forces a lane.
//
// The second result holds the references a test builds with a Join whose LAST
// argument is a string literal: the joined value is that very path, so a bare
// directory name spelled that way (`filepath.Join(root, "changelog.d")`) is a
// read of the directory, where the same name met elsewhere as a bare literal is
// as likely a tree a test skips.
//
// A Join that ends in a glob segment (`filepath.Glob(filepath.Join(dir, "*.md"))`)
// is a read of the directory in front of the wildcard, so it is reduced to that
// directory and recorded in the second result. A directory held in a variable
// the same file assigns from a Join of literals (`dir := filepath.Join(root,
// "changelog.d")`) is followed through the variable. A glob whose directory
// cannot be derived either way — nothing literal in front of the wildcard — is
// returned, positioned, as the third result, which the guard fails on instead of
// dropping the read.
func testPathReferences(t *testing.T, root string) (map[string][]string, map[string]bool, []string) {
	t.Helper()

	found := map[string]map[string]bool{}
	joined := map[string]bool{}
	var unresolved []string
	note := func(candidate, source string) string {
		candidate = path.Clean(filepath.ToSlash(candidate))
		for strings.HasPrefix(candidate, "../") {
			candidate = strings.TrimPrefix(candidate, "../")
		}
		candidate = strings.TrimPrefix(candidate, "./")
		if candidate == "" || candidate == "." || candidate == ".." || strings.HasPrefix(candidate, "/") || len(candidate) > 200 || strings.ContainsAny(candidate, " \t\n*{}%?<>|:\"\\") || strings.Contains(candidate, "...") {
			// `./migrations/...` is a Go package pattern, not a path: Windows
			// resolves its trailing dots to the directory, Linux finds nothing.
			return ""
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate))); err != nil {
			return ""
		}
		if found[candidate] == nil {
			found[candidate] = map[string]bool{}
		}
		found[candidate][source] = true
		return candidate
	}

	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && docReadSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		source, _ := filepath.Rel(root, p)
		source = filepath.ToSlash(source)

		// Variables this file assigns from a Join, so a glob over one of them
		// (`filepath.Join(dir, "*.md")`) still names the directory behind it.
		directories := map[string][]string{}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			name, isIdent := assign.Lhs[0].(*ast.Ident)
			call, isCall := assign.Rhs[0].(*ast.CallExpr)
			if !isIdent || !isCall || !isPathJoin(call) {
				return true
			}
			if segments, _ := joinSegments(call, directories); len(segments) > 0 {
				directories[name.Name] = segments
			}
			return true
		})

		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if value, err := strconv.Unquote(n.Value); err == nil {
						note(value, source)
					}
				}
			case *ast.CallExpr:
				if !isPathJoin(n) {
					return true
				}
				segments, endsInLiteral := joinSegments(n, directories)
				if len(segments) == 0 {
					return true
				}
				wildcard := firstGlobSegment(segments)
				if wildcard == 0 {
					unresolved = append(unresolved, fset.Position(n.Pos()).String()+" ("+segments[0]+")")
					return true
				}
				if wildcard > 0 {
					if candidate := note(strings.Join(segments[:wildcard], "/"), source); candidate != "" {
						joined[candidate] = true
					}
					return true
				}
				if len(segments) > 1 || endsInLiteral {
					if candidate := note(strings.Join(segments, "/"), source); candidate != "" && endsInLiteral {
						joined[candidate] = true
					}
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}

	out := map[string][]string{}
	for candidate, sources := range found {
		for source := range sources {
			out[candidate] = append(out[candidate], source)
		}
		sort.Strings(out[candidate])
	}
	sort.Strings(unresolved)
	return out, joined, unresolved
}

// isPathJoin reports a call of filepath.Join or path.Join.
func isPathJoin(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Join" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && (pkg.Name == "filepath" || pkg.Name == "path")
}

// joinSegments returns the literal segments of a Join in order — a variable the
// file assigned from a Join of literals contributes the segments of that Join —
// and whether the last argument is a string literal.
func joinSegments(call *ast.CallExpr, directories map[string][]string) ([]string, bool) {
	var segments []string
	for _, arg := range call.Args {
		switch a := arg.(type) {
		case *ast.BasicLit:
			if a.Kind != token.STRING {
				continue
			}
			if value, err := strconv.Unquote(a.Value); err == nil {
				segments = append(segments, value)
			}
		case *ast.Ident:
			segments = append(segments, directories[a.Name]...)
		}
	}
	endsInLiteral := false
	if len(call.Args) > 0 {
		last, ok := call.Args[len(call.Args)-1].(*ast.BasicLit)
		endsInLiteral = ok && last.Kind == token.STRING
	}
	return segments, endsInLiteral
}

// firstGlobSegment is the index of the first segment holding a glob character,
// or -1.
func firstGlobSegment(segments []string) int {
	for i, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			return i
		}
	}
	return -1
}

// docReadProbes turns the referenced paths into the files to probe the filter
// with: a file is itself; a directory is one file per extension found under it,
// because an allowlist can treat `.md` and `.yml` under one directory
// differently. Code files are left out — `.go` is compiled, so it can never
// read as documentation — and so is the tests' own source tree.
func docReadProbes(t *testing.T, root string, references map[string][]string, joined map[string]bool) map[string][]string {
	t.Helper()

	probes := map[string][]string{}
	for reference, sources := range references {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(reference)))
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if filepath.Ext(reference) != ".go" {
				probes[reference] = sources
			}
			continue
		}
		// A bare top-level name (`docs`, `web`, `e2e`) is what a test writes to
		// SKIP a tree or to join a longer path from, so it is a read only when a
		// test hands it to Join as the path's final segment
		// (`filepath.Join(root, "changelog.d")`) or spells at least two
		// segments of it.
		if !strings.Contains(reference, "/") && !joined[reference] {
			continue
		}
		perExtension := map[string]string{}
		walkRoot := filepath.Join(root, filepath.FromSlash(reference))
		err = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The root is the directory the test reads, whatever it is
				// called: only what is nested under it is held to the skip list.
				if p != walkRoot && docReadSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			ext := filepath.Ext(rel)
			if ext == ".go" {
				return nil
			}
			if _, seen := perExtension[ext]; !seen {
				perExtension[ext] = rel
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", reference, err)
		}
		for _, rel := range perExtension {
			probes[rel] = sources
		}
	}
	return probes
}

// TestDocReadDerivationSeesGlobsDirectoriesAndDotRoots holds the derivation to
// the three read shapes it has missed, over a scratch tree whose answers are
// known: a glob over a directory held in a variable, a glob over a literal
// directory, and a directory whose own name starts with a dot (the walk applies
// the skip list to what is nested under a directory a test reads, never to the
// directory itself). A glob with no derivable directory is reported, not dropped.
func TestDocReadDerivationSeesGlobsDirectoriesAndDotRoots(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create the directory of %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("docs/a.md", "x")
	write("pages/b.md", "x")
	write(".sandbox/notes.md", "x")
	write(".sandbox/.cache/skipped.md", "x")
	write("pkg/reader_test.go", `package pkg

import "path/filepath"

func reads(root string) {
	dir := filepath.Join(root, "docs")
	_, _ = filepath.Glob(filepath.Join(dir, "*.md"))
	_, _ = filepath.Glob(filepath.Join(root, "pages", "*.md"))
	_ = filepath.Join(root, ".sandbox")
}
`)

	references, joined, unresolved := testPathReferences(t, root)
	if len(unresolved) != 0 {
		t.Fatalf("every glob in the scratch tree has a derivable directory, yet the derivation reports %v", unresolved)
	}
	probes := docReadProbes(t, root, references, joined)
	for _, want := range []string{"docs/a.md", "pages/b.md", ".sandbox/notes.md"} {
		if _, ok := probes[want]; !ok {
			t.Errorf("the derivation does not probe %s (probes: %v): a read of that shape would go unjudged", want, probes)
		}
	}
	if _, ok := probes[".sandbox/.cache/skipped.md"]; ok {
		t.Errorf("a directory nested under the one a test reads is held to the skip list, yet %v is probed", probes)
	}

	write("pkg/blind_test.go", `package pkg

import "path/filepath"

func blind(unknown string) {
	_, _ = filepath.Glob(filepath.Join(unknown, "*.md"))
}
`)
	if _, _, unresolved = testPathReferences(t, root); len(unresolved) != 1 {
		t.Errorf("a glob with no literal directory must be reported exactly once, got %v", unresolved)
	}
}

// runDetectOnList runs ci.yml's detect step, as a pull request, over a file list
// holding exactly one path — the list the diff action would have written for a
// diff of that one file. The list step itself is not run: it needs a repository
// and a base, costs seconds a probe, and has its own guards; the detect step
// reads the list from a file, so this is the same input it gets on the runner.
func runDetectOnList(t *testing.T, bash, dir string, detect scriptStep, name string) map[string]string {
	t.Helper()

	list := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(list, []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("write the file list: %v", err)
	}
	github := map[string]string{
		"github.event_name":                  "pull_request",
		"github.event.pull_request.base.ref": "main",
		"github.event.merge_group.base_sha":  "",
		"github.event.merge_group.head_ref":  "",
		"github.token":                       "",
	}
	listed := map[string]string{"files-path": filepath.ToSlash(list), "verdict": ""}
	env := append(hermeticGitEnv(), "RUNNER_TEMP="+filepath.ToSlash(t.TempDir()), "LC_ALL=C.UTF-8")
	outputs, _ := runScriptStep(t, bash, dir, env, detect, github, listed, ciWorkflow+" detect step")
	return outputs
}

// docReadProbeBudget bounds how many one-file diffs the guard runs: each is a
// real detect step in a scratch repository. It is a ceiling, not a target — a
// derived set past it means the extraction is sweeping in something that is not
// a document, and the fix is to narrow what it collects.
const docReadProbeBudget = 80

// TestNoDocumentAGoTestReadsIsInertToTheDetectFilter is the guard. Anti-vacuity
// names the load-bearing member: docs/openapi.yaml is read by the API contract
// test, so a derivation that stops finding it has stopped looking, and the
// loop below would pass over nothing.
func TestNoDocumentAGoTestReadsIsInertToTheDetectFilter(t *testing.T) {
	root := repoRoot(t)
	references, joined, unresolved := testPathReferences(t, root)
	for _, position := range unresolved {
		t.Errorf("%s: a Join ending in a glob has no literal directory in front of it, so the directory it reads cannot be derived and a document there would go unjudged. Spell the directory as a literal Join in that test, or assign it from one in the same file", position)
	}
	probes := docReadProbes(t, root, references, joined)

	const loadBearing = "docs/openapi.yaml"
	if _, ok := probes[loadBearing]; !ok {
		t.Fatalf("the derivation no longer finds %s among the paths Go tests name (%d probes): it reads them from internal/api/openapi_contract_test.go, so the extraction stopped seeing a read it must see", loadBearing, len(probes))
	}
	// The directory branch has its own load-bearing member: the changelog tool's
	// test reads the real fragments through a bare directory name. A fragment
	// markdown file probed on that test's account can only come from expanding
	// the directory — no test names a real fragment file there — so a derivation
	// that drops bare directories fails here instead of passing every file read
	// above while a fragment-only diff goes unjudged.
	const loadBearingDir, loadBearingDirSource = "changelog.d/", "scripts/changelogd/main_test.go"
	directoryRead := false
	for name, sources := range probes {
		if strings.HasPrefix(name, loadBearingDir) && strings.HasSuffix(name, ".md") && strings.Contains(strings.Join(sources, ","), loadBearingDirSource) {
			directoryRead = true
		}
	}
	if !directoryRead {
		t.Fatalf("the derivation no longer expands %s into the files under it: %s reads that directory through a bare name, so the extraction stopped seeing a directory read it must see (%d probes)", strings.TrimSuffix(loadBearingDir, "/"), loadBearingDirSource, len(probes))
	}
	if len(probes) > docReadProbeBudget {
		t.Fatalf("%d probes exceed the budget of %d: the derived set is sweeping in more than documents", len(probes), docReadProbeBudget)
	}

	names := make([]string, 0, len(probes))
	for name := range probes {
		names = append(names, name)
	}
	sort.Strings(names)

	detect := detectStep(t, ciWorkflow)
	dir := t.TempDir()
	bash := requireBash(t, dir)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runDetectOnList(t, bash, dir, detect, name)
			for _, lane := range []string{"run_core", "run_unit"} {
				// The verdict must be the literal "true": an output the step stopped
				// emitting or renamed reads as empty, and "not false" would pass it.
				if got[lane] != "true" {
					t.Errorf("%s is named by %s, so a pull request touching only it must run the Go lanes, but the detect step leaves %s=%q (all outputs: %v). Add it to the document-reader force in ci.yml's detect step — and to the `run_unit` condition beside it — rather than to a skip list; an empty value means the step no longer emits that output",
						name, strings.Join(probes[name], ", "), lane, got[lane], got)
				}
			}
		})
	}
}
