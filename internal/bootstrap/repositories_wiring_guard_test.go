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

	var offenders []string
	found := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "node_modules" || name == ".tmp" {
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
		found++
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(filepath.Dir(relative)) != "internal/bootstrap" {
			offenders = append(offenders, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// Anti-vacuity: this package's own call must be among the matches, or the
	// scan found nothing for a reason that has nothing to do with the rule.
	if found == 0 {
		t.Fatal("the scan matched no production call to db.NewRepositories at all, including this package's own: it is measuring nothing")
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("these build repositories without the calendar-feed restore fence, so their feed writes leave no record a backup restore cannot undo — call bootstrap.BuildRepositories instead: %v", offenders)
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
