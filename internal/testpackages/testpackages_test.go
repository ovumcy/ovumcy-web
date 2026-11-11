package testpackages

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const goPackagesImportPath = "golang.org/x/tools/go/packages"

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

// usesGoPackagesLoad reports whether a file reaches go/packages' Load under
// any import name: a call, a method value, or a dot import.
func usesGoPackagesLoad(file *ast.File) bool {
	var names []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != goPackagesImportPath {
			continue
		}
		name := "packages"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch name {
		case "_":
			continue
		case ".":
			return true
		}
		names = append(names, name)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Load" {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok {
			for _, name := range names {
				if ident.Name == name {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// TestGoPackagesLoadDetectorSeesEveryForm pins the detector the tree sweep
// relies on, so an aliased or indirect use cannot slip past it.
func TestGoPackagesLoadDetectorSeesEveryForm(t *testing.T) {
	for _, testCase := range []struct {
		what   string
		source string
		uses   bool
	}{
		{"a plain call", `import "golang.org/x/tools/go/packages"; func f() { packages.Load(nil) }`, true},
		{"an aliased call", `import xp "golang.org/x/tools/go/packages"; func f() { xp.Load(nil) }`, true},
		{"a method value", `import "golang.org/x/tools/go/packages"; var load = packages.Load`, true},
		{"a dot import", `import . "golang.org/x/tools/go/packages"; func f() { Load(nil) }`, true},
		{"the wrapper", `import "golang.org/x/tools/go/packages"; func f() { testpackages.Load(&packages.Config{}) }`, false},
		{"another package's Load", `import packages "example.com/other"; func f() { packages.Load(nil) }`, false},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture; "+testCase.source, 0)
		if err != nil {
			t.Fatalf("%s: parse: %v", testCase.what, err)
		}
		if got := usesGoPackagesLoad(file); got != testCase.uses {
			t.Errorf("%s: usesGoPackagesLoad = %v, want %v", testCase.what, got, testCase.uses)
		}
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
	exempt := map[string]bool{
		"internal/testpackages/testpackages.go": true,
		"scripts/archcheck/main.go":             true,
	}

	var offenders []string
	// The package trees the repository's Go steps scope themselves to.
	for _, tree := range []string{"cmd", "internal", "migrations", "scripts", "web"} {
		err = filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "testdata" {
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
			if !strings.Contains(string(source), strconv.Quote(goPackagesImportPath)) {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
			if err != nil {
				return err
			}
			if usesGoPackagesLoad(file) {
				offenders = append(offenders, rel)
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("walking %s: %v", tree, err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("call testpackages.Load (and open the test with testpackages.Isolate) instead of go/packages' Load in:\n  %s", strings.Join(offenders, "\n  "))
	}
}
