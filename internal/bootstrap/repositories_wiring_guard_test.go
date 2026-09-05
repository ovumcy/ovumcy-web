package bootstrap

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestOnlyBootstrapBuildsRepositories keeps the calendar-feed restore fence from
// being wired in some binaries and not others.
//
// db.NewRepositories returns a set whose feed writes record nothing outside the
// database. That is correct for a test, which owns its own database and never
// restores one, and wrong for anything that serves or administers a real
// instance: a revocation recorded only inside the database is undone by
// restoring a backup taken before it, which is the whole finding. Compile-time
// cannot tell the two callers apart — both get a valid *Repositories — so this
// guard does, by refusing the direct call in production code anywhere but the
// one constructor that attaches the fence.
func TestOnlyBootstrapBuildsRepositories(t *testing.T) {
	root := repositoryRoot(t)

	result, err := scanRepositoryWiring(root)
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// Anti-vacuity: this package's own call must be among the matches, or the
	// scan found nothing for a reason that has nothing to do with the rule.
	if result.found == 0 {
		t.Fatal("the scan matched no production call to db.NewRepositories at all, including this package's own: it is measuring nothing")
	}

	if len(result.offenders) > 0 {
		t.Fatalf("these build repositories without the calendar-feed restore fence, so their feed writes leave no record a backup restore cannot undo — call bootstrap.BuildRepositories instead: %v", result.offenders)
	}
}

// wiringScanResult is the population scanRepositoryWiring judged: how many
// production call sites it matched, and which of those lie outside the one
// constructor allowed to hold them.
type wiringScanResult struct {
	found     int
	offenders []string
}

// scanRepositoryWiring walks root — the tracked tree of a single Go module —
// for production (non-test) Go source calling db.NewRepositories(, and
// reports every call site that is not in internal/bootstrap.
//
// root is a parameter, rather than always repositoryRoot(t), so the walk and
// its filtering can be exercised directly against a fixture, independent of
// this repository's own tree.
func scanRepositoryWiring(root string) (wiringScanResult, error) {
	var result wiringScanResult
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && isOutsideTrackedTree(path, entry) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(source), "db.NewRepositories(") {
			return nil
		}
		result.found++
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(filepath.Dir(relative)) != "internal/bootstrap" {
			result.offenders = append(result.offenders, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(result.offenders)
	return result, err
}

// isOutsideTrackedTree reports whether directory path lies outside the
// tracked tree of the module the walk started in: a dot-directory
// (build/tooling state — .git, .tmp, an editor's or another agent's own
// dotfile area — never part of the tracked source tree), a node_modules
// vendor tree, or a directory that is itself the root of a *different*
// module or repository checkout nested under this one.
//
// The last case is judged by property, not by name: a directory carrying
// its own go.mod and/or its own git marker is a separate population — a
// worktree's ".git" is a regular file (a "gitdir:" pointer), not a
// directory, so what is checked is presence, not directory-ness. A
// hardcoded list of nested-checkout directory names would be the same
// defect in a new wrapper; this checks what makes a directory a module or
// repository root, not what it happens to be called.
func isOutsideTrackedTree(path string, entry fs.DirEntry) bool {
	if strings.HasPrefix(entry.Name(), ".") {
		return true
	}
	if entry.Name() == "node_modules" {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	return false
}

// TestWiringScanCatchesAnUnwiredCallOutsideBootstrap is the guard's positive
// control: nested-checkout filtering must not have quietly disabled
// detection of a real violation living inside the tracked tree itself.
func TestWiringScanCatchesAnUnwiredCallOutsideBootstrap(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module fixture\n\ngo 1.23\n")
	writeFixtureFile(t, root, filepath.Join("internal", "bootstrap", "repositories.go"),
		"package bootstrap\n\nfunc BuildRepositories() {\n\tdb.NewRepositories(nil)\n}\n")
	writeFixtureFile(t, root, filepath.Join("internal", "api", "handlers.go"),
		"package api\n\nfunc unwired() {\n\tdb.NewRepositories(nil)\n}\n")

	result, err := scanRepositoryWiring(root)
	if err != nil {
		t.Fatalf("scanRepositoryWiring(%s): %v", root, err)
	}
	if result.found != 2 {
		t.Fatalf("found = %d, want 2 (one wired in internal/bootstrap, one unwired in internal/api)", result.found)
	}
	if len(result.offenders) != 1 || result.offenders[0] != "internal/api/handlers.go" {
		t.Fatalf("offenders = %v, want exactly [internal/api/handlers.go]: the guard must still catch a real violation inside the tracked tree", result.offenders)
	}
}

// TestWiringScanIgnoresANestedRepositoryCheckout reproduces TS-M19: a
// checkout of another repository (or another worktree of this one) sitting
// physically under the module root — complete with its own go.mod, its own
// git marker, and a stale file that happens to contain the exact
// db.NewRepositories( text the guard looks for — must not make the guard
// fail on an otherwise-unchanged product tree. The nested checkout is a
// different population; the guard's job is this module's tracked tree only.
func TestWiringScanIgnoresANestedRepositoryCheckout(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module fixture\n\ngo 1.23\n")

	const nested = "nested-checkout"
	writeFixtureFile(t, root, filepath.Join(nested, "go.mod"), "module nestedfixture\n\ngo 1.23\n")
	writeFixtureFile(t, root, filepath.Join(nested, ".git"), "gitdir: ../../.git/worktrees/nested-checkout\n")
	writeFixtureFile(t, root, filepath.Join(nested, "internal", "old", "stale.go"),
		"package old\n\nfunc build() {\n\tdb.NewRepositories(nil)\n}\n")

	result, err := scanRepositoryWiring(root)
	if err != nil {
		t.Fatalf("scanRepositoryWiring(%s): %v", root, err)
	}
	if len(result.offenders) != 0 {
		t.Fatalf("scan reported %v from inside a nested repository checkout, a population it must not judge", result.offenders)
	}
	if result.found != 0 {
		t.Fatalf("scan counted %d match(es) inside a nested repository checkout; it should never have descended into it", result.found)
	}
}

// writeFixtureFile creates relative (and any missing parent directories)
// under root with the given content, for building walk fixtures.
func writeFixtureFile(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// repositoryRoot walks up from this package to the module root.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("no go.mod above the bootstrap package: cannot locate the module root")
		}
		directory = parent
	}
}
