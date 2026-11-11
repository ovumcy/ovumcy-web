package testpackages

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestIsolatedTestRunsSerialisedUnderRace pins both halves of the mechanism:
// outside -race nothing is re-run, and under -race the body runs only in a
// child where GOMAXPROCS is 1 and Load is allowed.
func TestIsolatedTestRunsSerialisedUnderRace(t *testing.T) {
	if Isolate(t) {
		if !raceEnabled {
			t.Fatal("Isolate re-ran the test outside -race")
		}
		return
	}
	if raceEnabled && runtime.GOMAXPROCS(0) != 1 {
		t.Fatalf("isolated body runs with GOMAXPROCS=%d, want 1", runtime.GOMAXPROCS(0))
	}
	if _, err := Load(&packages.Config{Mode: packages.NeedName}, "."); err != nil {
		t.Fatalf("Load inside an isolated test: %v", err)
	}
}

// TestLoadRefusesAnUnisolatedTestUnderRace: a guard that forgets Isolate fails
// on the race lanes instead of racing go/types again.
func TestLoadRefusesAnUnisolatedTestUnderRace(t *testing.T) {
	if !raceEnabled {
		t.Skip("the refusal exists only under -race")
	}
	if os.Getenv(serialChildEnv) == "1" {
		t.Skip("this test asserts the parent-process refusal")
	}
	if _, err := Load(&packages.Config{Mode: packages.NeedName}, "."); !errors.Is(err, ErrNotIsolated) {
		t.Fatalf("Load outside Isolate under -race: err = %v, want ErrNotIsolated", err)
	}
}

// TestNoTestCallsPackagesLoadDirectly keeps every type-checking load in the
// tree behind this package, so a new guard cannot bring the go/types race
// back. scripts/archcheck is a program, never run under -race.
func TestNoTestCallsPackagesLoadDirectly(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not at %s: %v", root, err)
	}
	direct := regexp.MustCompile(`\bpackages\.Load\(`)
	exempt := map[string]bool{
		"internal/testpackages/testpackages.go": true,
		"scripts/archcheck/main.go":             true,
	}

	var offenders []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if exempt[rel] {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if direct.Match(source) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("call testpackages.Load (and open the test with testpackages.Isolate) instead of packages.Load in:\n  %s", strings.Join(offenders, "\n  "))
	}
}
