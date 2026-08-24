package services

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Barrier for the hand-mirrored fuzz harness.
//
// The pure parsing/validation policy helpers are fuzzed from three places that
// have to agree and that nothing derived from one another:
//
//   - policy_fuzz_test.go declares the native `go test -fuzz` targets and is
//     the source of truth for the seed corpora and the behavioral oracles;
//   - policy_fuzz_libfuzzer.go repeats those targets for ClusterFuzzLite behind
//     `//go:build gofuzz`, because go-118-fuzz-build cannot read native
//     testing.F fuzzers out of a _test.go file;
//   - .clusterfuzzlite/build.sh names every target a third time, so the build
//     script knows what to compile.
//
// The default build never compiles the tagged file, so `go vet`, the linters
// and the coverage run all walk straight past it. ClusterFuzzLite's PR job does
// build it, which catches a target that stops COMPILING — and catches nothing
// at all when a seed is dropped or an oracle is corrected on one side only.
// That is the drift this barrier exists for: it reads the shipped source of all
// three files and fails when they stop saying the same thing.
//
// It deliberately covers all three sites. Pinning two of them would leave the
// third free to drift, and a target renamed in both Go files but not in
// build.sh is a fuzzing lane that silently stops building a target.
//
// What it cannot see, stated here and repeated in the failure text: it compares
// SOURCE, normalized for comments and whitespace only. Two bodies that differ
// in behavior only through a helper one of the files declares elsewhere read as
// identical here, and a seed corpus loaded from testdata rather than written
// inline is invisible to it.
const (
	fuzzParityNativeFile    = "internal/services/policy_fuzz_test.go"
	fuzzParityLibFuzzerFile = "internal/services/policy_fuzz_libfuzzer.go"
	fuzzParityBuildScript   = ".clusterfuzzlite/build.sh"
)

// TestPolicyFuzzHarnessesDeclareTheSameTargets fails when the native harness and
// the gofuzz-tagged harness stop declaring the same set of fuzz targets.
func TestPolicyFuzzHarnessesDeclareTheSameTargets(t *testing.T) {
	root := fuzzParityRepoRoot(t)
	native := parseFuzzParityFile(t, filepath.Join(root, filepath.FromSlash(fuzzParityNativeFile)))
	tagged := parseFuzzParityFile(t, filepath.Join(root, filepath.FromSlash(fuzzParityLibFuzzerFile)))

	if len(native.targets) == 0 {
		t.Fatalf("%s declares no Fuzz target — the barrier read a file it does not understand, not a tree without drift", fuzzParityNativeFile)
	}

	nativeNames := sortedKeys(native.targets)
	taggedNames := sortedKeys(tagged.targets)
	if strings.Join(nativeNames, ",") != strings.Join(taggedNames, ",") {
		t.Fatalf("the two fuzz harnesses declare different targets:\n  %s: %v\n  %s: %v\nboth files must declare the same set, one testing.F wrapper each",
			fuzzParityNativeFile, nativeNames, fuzzParityLibFuzzerFile, taggedNames)
	}
}

// TestPolicyFuzzHarnessesShareTheirSeedsAndOracles fails when a target body or a
// shared helper is corrected in one harness and left stale in the other.
func TestPolicyFuzzHarnessesShareTheirSeedsAndOracles(t *testing.T) {
	root := fuzzParityRepoRoot(t)
	native := parseFuzzParityFile(t, filepath.Join(root, filepath.FromSlash(fuzzParityNativeFile)))
	tagged := parseFuzzParityFile(t, filepath.Join(root, filepath.FromSlash(fuzzParityLibFuzzerFile)))

	compared := 0
	for _, name := range sortedKeys(tagged.bodies) {
		want, ok := native.bodies[name]
		if !ok {
			// The target-set barrier above owns a missing target; a helper the
			// tagged file declares alone is reported here.
			t.Errorf("%s declares %s, which %s does not — a declaration only the gofuzz build compiles is walked by no analyser",
				fuzzParityLibFuzzerFile, name, fuzzParityNativeFile)
			continue
		}
		compared++
		if want != tagged.bodies[name] {
			t.Errorf("%s drifted from %s (source compared with comments and whitespace normalized away):\n--- %s\n%s\n--- %s\n%s",
				name, fuzzParityNativeFile, fuzzParityNativeFile, want, fuzzParityLibFuzzerFile, tagged.bodies[name])
		}
	}

	if compared == 0 {
		t.Fatalf("no declaration was compared across the two harnesses — the barrier measured nothing")
	}
}

// TestClusterFuzzLiteBuildsEveryDeclaredFuzzTarget fails when the build script's
// hand-written target list stops matching the harnesses.
func TestClusterFuzzLiteBuildsEveryDeclaredFuzzTarget(t *testing.T) {
	root := fuzzParityRepoRoot(t)
	native := parseFuzzParityFile(t, filepath.Join(root, filepath.FromSlash(fuzzParityNativeFile)))
	scripted := parseClusterFuzzLiteTargets(t, filepath.Join(root, filepath.FromSlash(fuzzParityBuildScript)))

	declared := sortedKeys(native.targets)
	if len(scripted) == 0 {
		t.Fatalf("%s enumerates no target — the barrier read a script it does not understand", fuzzParityBuildScript)
	}
	if strings.Join(declared, ",") != strings.Join(scripted, ",") {
		t.Fatalf("%s builds a different set of targets than %s declares:\n  declared: %v\n  built:    %v\na target the harnesses declare and the script omits is never fuzzed in CI",
			fuzzParityBuildScript, fuzzParityNativeFile, declared, scripted)
	}
}

// TestFuzzParityComparisonDistinguishesDriftFromFormatting anchors the
// comparison on fixtures this test owns, so the three barriers above cannot
// report success because their normalizer collapsed everything to the same
// string. One pair must compare equal, one pair must not.
func TestFuzzParityComparisonDistinguishesDriftFromFormatting(t *testing.T) {
	const commented = `package p

func FuzzThing(f *testing.F) {
	// A doc-shaped comment the tagged copy does not carry.
	for _, seed := range []string{
		"a", // at the limit
		"b",
	} {
		f.Add(seed)
	}
}
`
	const bare = `package p

func FuzzThing(f *testing.F) {
	for _, seed := range []string{
		"a",
		"b",
	} {
		f.Add(seed)
	}
}
`
	const drifted = `package p

func FuzzThing(f *testing.F) {
	for _, seed := range []string{
		"a",
	} {
		f.Add(seed)
	}
}
`
	commentedBodies := parseFuzzParitySource(t, "commented.go", commented)
	bareBodies := parseFuzzParitySource(t, "bare.go", bare)
	driftedBodies := parseFuzzParitySource(t, "drifted.go", drifted)

	if commentedBodies["FuzzThing"] != bareBodies["FuzzThing"] {
		t.Fatalf("the normalizer reports drift between two bodies that differ only in comments:\n%s\n---\n%s",
			commentedBodies["FuzzThing"], bareBodies["FuzzThing"])
	}
	if commentedBodies["FuzzThing"] == driftedBodies["FuzzThing"] {
		t.Fatalf("the normalizer reports agreement between bodies with different seed corpora — it cannot detect real drift:\n%s",
			driftedBodies["FuzzThing"])
	}
}

// fuzzParityFile is one harness file reduced to what the barrier compares: the
// set of fuzz-target names, and the normalized source of every top-level
// function body in it (targets and shared oracles alike).
type fuzzParityFile struct {
	targets map[string]struct{}
	bodies  map[string]string
}

func parseFuzzParityFile(t *testing.T, path string) fuzzParityFile {
	t.Helper()

	source, err := os.ReadFile(path) // #nosec G304 -- a path this test builds from the module root
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	bodies := parseFuzzParitySource(t, filepath.Base(path), string(source))

	targets := make(map[string]struct{}, len(bodies))
	for name := range bodies {
		if strings.HasPrefix(name, "Fuzz") {
			targets[name] = struct{}{}
		}
	}
	return fuzzParityFile{targets: targets, bodies: bodies}
}

// parseFuzzParitySource returns each top-level function's body as normalized
// source. Parsing without parser.ParseComments is what drops the commentary the
// two harnesses legitimately word differently; the line normalization below
// drops the blank lines those comments leave behind.
func parseFuzzParitySource(t *testing.T, name, source string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}

	bodies := map[string]string{}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil {
			continue
		}
		bodies[fn.Name.Name] = normalizeFuzzParityBody(t, fset, fn.Body)
	}
	return bodies
}

func normalizeFuzzParityBody(t *testing.T, fset *token.FileSet, body *ast.BlockStmt) string {
	t.Helper()

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, body); err != nil {
		t.Fatalf("printing a function body: %v", err)
	}

	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

// parseClusterFuzzLiteTargets reads the `for target in … do` list out of the
// build script. It fails rather than returning an empty list when the loop is
// not where it expects, so a rewritten script cannot pass by saying nothing.
func parseClusterFuzzLiteTargets(t *testing.T, path string) []string {
	t.Helper()

	file, err := os.Open(path) // #nosec G304 -- a path this test builds from the module root
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("closing %s: %v", path, closeErr)
		}
	}()

	var (
		targets []string
		inLoop  bool
		closed  bool
	)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case !inLoop && strings.HasPrefix(line, "for target in"):
			inLoop = true
		case inLoop && line == "do":
			closed = true
		case inLoop && !closed:
			if name := strings.TrimSpace(strings.TrimSuffix(line, `\`)); name != "" {
				targets = append(targets, name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning %s: %v", path, err)
	}
	if !inLoop || !closed {
		t.Fatalf("%s has no `for target in … do` loop where the barrier expects one — rewrite the barrier with the script", path)
	}

	sort.Strings(targets)
	return targets
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fuzzParityRepoRoot(t *testing.T) string {
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
			t.Fatalf("no go.mod above the working directory — the barrier cannot find the module root")
		}
		dir = parent
	}
}
