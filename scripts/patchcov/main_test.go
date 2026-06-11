package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseCoverageProfileAndLineState(t *testing.T) {
	profile := "mode: atomic\n" +
		"github.com/ovumcy/ovumcy-web/internal/db/sqlite.go:12.45,17.2 3 1\n" +
		"github.com/ovumcy/ovumcy-web/internal/db/sqlite.go:20.2,22.3 2 0\n"

	cov := parseCoverageProfile(profile, "github.com/ovumcy/ovumcy-web")
	blocks, ok := cov["internal/db/sqlite.go"]
	if !ok {
		t.Fatalf("expected internal/db/sqlite.go in coverage map")
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	cases := []struct {
		line int
		want int
	}{
		{line: 12, want: stateCovered},
		{line: 15, want: stateCovered},
		{line: 21, want: stateUncovered},
		{line: 100, want: stateNonCoverable},
	}
	for _, c := range cases {
		if got := lineState(blocks, c.line); got != c.want {
			t.Fatalf("lineState(line=%d) = %d, want %d", c.line, got, c.want)
		}
	}
}

func TestParseDiffAddedLinesGatesProductionGoOnly(t *testing.T) {
	diff := "" +
		"diff --git a/internal/db/sqlite.go b/internal/db/sqlite.go\n" +
		"--- a/internal/db/sqlite.go\n" +
		"+++ b/internal/db/sqlite.go\n" +
		"@@ -16,1 +16,2 @@\n" +
		"+\tdsn := \"new\"\n" +
		"+\t_ = dsn\n" +
		"diff --git a/internal/db/sqlite_test.go b/internal/db/sqlite_test.go\n" +
		"+++ b/internal/db/sqlite_test.go\n" +
		"@@ -1,0 +5,1 @@\n" +
		"+// test-only line\n" +
		"diff --git a/scripts/patchcov/main.go b/scripts/patchcov/main.go\n" +
		"+++ b/scripts/patchcov/main.go\n" +
		"@@ -1,0 +1,1 @@\n" +
		"+// tooling line\n"

	added := parseDiffAddedLines(diff)
	want := map[string][]int{"internal/db/sqlite.go": {16, 17}}
	if !reflect.DeepEqual(added, want) {
		t.Fatalf("parseDiffAddedLines = %v, want %v (test files and scripts/ must be excluded)", added, want)
	}
}

func TestHunkNewStart(t *testing.T) {
	cases := map[string]int{
		"@@ -16,1 +16,2 @@":          16,
		"@@ -1,0 +5,1 @@ func Foo()": 5,
		"@@ -10 +10 @@":              10,
	}
	for hunk, want := range cases {
		if got := hunkNewStart(hunk); got != want {
			t.Fatalf("hunkNewStart(%q) = %d, want %d", hunk, got, want)
		}
	}
}

func TestIsGatedFile(t *testing.T) {
	for _, f := range []string{"internal/db/sqlite.go", "cmd/ovumcy/main.go"} {
		if !isGatedFile(f) {
			t.Fatalf("expected %q to be gated", f)
		}
	}
	for _, f := range []string{
		"internal/db/sqlite_test.go",
		"scripts/patchcov/main.go",
		"internal/testdb/postgres.go",
		"README.md",
		"web/static/js/app.js",
	} {
		if isGatedFile(f) {
			t.Fatalf("expected %q to NOT be gated", f)
		}
	}
}

func TestMarkedLinesAndLineIgnored(t *testing.T) {
	src := filepath.Join(t.TempDir(), "x.go")
	content := "package x\n" + // 1
		"func A() error {\n" + // 2
		"\tif err := f(); err != nil {\n" + // 3
		"\t\treturn err // codecov:ignore -- unreachable\n" + // 4
		"\t}\n" + // 5
		"\treturn nil\n" + // 6
		"}\n" + // 7
		"// codecov:ignore:start\n" + // 8
		"func B() {}\n" + // 9
		"// codecov:ignore:end\n" // 10
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	marked := markedLines(src)
	for _, ln := range []int{4, 8, 9, 10} {
		if !marked[ln] {
			t.Fatalf("line %d should be marked", ln)
		}
	}
	for _, ln := range []int{1, 2, 3, 5, 6, 7} {
		if marked[ln] {
			t.Fatalf("line %d should NOT be marked", ln)
		}
	}

	// The uncovered if-body block [3,5] shares the marker on line 4, so the
	// closing brace on line 5 is excluded too; the covered return on line 6 is not.
	blocks := []coverBlock{{start: 3, end: 5, covered: false}}
	if !lineIgnored(blocks, 5, marked) {
		t.Fatal("line 5 (closing brace) should be ignored via its annotated block")
	}
	if lineIgnored(blocks, 6, marked) {
		t.Fatal("line 6 should not be ignored")
	}
}

func TestReadModulePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(path, []byte("module github.com/ovumcy/ovumcy-web\n\ngo 1.26.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readModulePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/ovumcy/ovumcy-web" {
		t.Fatalf("readModulePath = %q", got)
	}
}
