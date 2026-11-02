package webhookdocs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The matrix and its public mirror cite tests by name, and a rename left the
// citation naming nothing while the row kept reading as evidenced (the notify
// fence row cited two tests that were called something else). This sweep
// resolves every backticked `Test…` name in both documents against the test
// functions the repository defines. A name that is only a PREFIX of a defined
// test resolves too: a row may cite a family (`TestNotify`) deliberately. The
// documents are read in full, not by section, so a row added anywhere joins.

var (
	backtickedSpan   = regexp.MustCompile("`([^`\n]+)`")
	leadingTestIdent = regexp.MustCompile(`^Test[A-Za-z0-9_]*`)
	definedTestFunc  = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
)

// citedTestNames returns every distinct test name a document cites in
// backticks, taking the leading identifier of a span so `TestX` and
// `TestX/subtest` both yield TestX.
func citedTestNames(doc string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, span := range backtickedSpan.FindAllStringSubmatch(doc, -1) {
		name := leadingTestIdent.FindString(span[1])
		if name == "" || name == "Test" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// danglingTestCitations lists the cited names that are neither a defined test
// nor a prefix of one.
func danglingTestCitations(doc string, defined []string) []string {
	dangling := []string{}
	for _, name := range citedTestNames(doc) {
		resolved := false
		for _, candidate := range defined {
			if strings.HasPrefix(candidate, name) {
				resolved = true
				break
			}
		}
		if !resolved {
			dangling = append(dangling, name)
		}
	}
	return dangling
}

func definedTestFuncs(t *testing.T, root string) []string {
	t.Helper()

	names := []string{}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", ".tmp", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range definedTestFunc.FindAllStringSubmatch(string(content), -1) {
			names = append(names, match[1])
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s for test functions: %v", root, walkErr)
	}
	return names
}

func TestEveryTestNameTheSecurityDocsCiteResolvesToADefinedTest(t *testing.T) {
	root := repoRoot(t)
	defined := definedTestFuncs(t, root)
	if len(defined) < 100 {
		t.Fatalf("found only %d test functions under %s: the sweep is measuring the wrong tree", len(defined), root)
	}

	for _, rel := range []string{matrixPath, mirrorPath} {
		doc := readDoc(t, root, rel)
		if len(citedTestNames(doc)) == 0 && rel == matrixPath {
			t.Fatalf("%s cites no test at all: the extractor is measuring nothing", rel)
		}
		for _, name := range danglingTestCitations(doc, defined) {
			t.Errorf("%s cites `%s`, which is neither a defined Go test function nor a prefix of one: rename the citation to the test that exists, or restore the test", rel, name)
		}
	}
}

// TestDanglingTestCitationsFlagsOnlyUnresolvedNames drives the sweep's own
// classifier: a renamed citation must be flagged, an exact name, a subtest
// suffix and a deliberate prefix must resolve, and prose that merely starts
// with "Test" inside a longer span is read by its leading identifier.
func TestDanglingTestCitationsFlagsOnlyUnresolvedNames(t *testing.T) {
	defined := []string{"TestRealThing", "TestFamilyOne", "TestFamilyTwo"}
	doc := "| `TestRealThing`, `TestRealThing/sub case`, `TestFamily`, `TestGoneThing`, `TestRealThing` | x |\n" +
		"and `not a test`, `Test` alone"

	got := danglingTestCitations(doc, defined)
	if len(got) != 1 || got[0] != "TestGoneThing" {
		t.Fatalf("danglingTestCitations = %v, want exactly [TestGoneThing]", got)
	}
}
