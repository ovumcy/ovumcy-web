package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materializes a tree under a fresh temporary directory. Keys are
// slash-separated and relative, so the literal reads like the tree it describes.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// fixture reads one of the Go sources kept under testdata as data. Why they are
// data and not source: testdata/README.md.
func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(body)
}

func scan(t *testing.T, dir string) []finding {
	t.Helper()
	got, err := run(dir)
	if err != nil {
		t.Fatalf("run(%s): %v", dir, err)
	}
	return got
}

// rulesIn names the rules the findings carry, so a failure message says what
// fired rather than only how much did.
func rulesIn(findings []finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.rule)
	}
	return out
}

func TestACleanTreeHasNoFindings(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"internal/api/handler.go":  "package api\n\nimport \"fmt\"\n\nfunc F() { fmt.Println(\"ok\") }\n",
		"internal/services/svc.go": "package services\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n",
		"internal/db/repo.go":      "package db\n\nimport \"database/sql\"\n\nvar _ *sql.DB\n",
	})
	if got := scan(t, dir); len(got) != 0 {
		t.Fatalf("clean tree reported %v: %v", rulesIn(got), got)
	}
}

// The transport rule, one file per spelling. The negatives carry as much as the
// positives: a guard that reads prose as code is the defect that made this an
// import-shaped question rather than a substring one.
func TestTransportProductionCodeMayNotImportPersistence(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want bool
	}{
		{
			name: "grouped import",
			path: "internal/api/handler.go",
			body: "package api\n\nimport (\n\t\"fmt\"\n\t\"gorm.io/gorm\"\n)\n\nvar _ = fmt.Sprint\nvar _ *gorm.DB\n",
			want: true,
		},
		{
			name: "single-line import",
			path: "internal/api/handler.go",
			body: "package api\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n",
			want: true,
		},
		{
			name: "aliased import",
			path: "internal/api/handler.go",
			body: "package api\n\nimport g \"gorm.io/gorm\"\n\nvar _ *g.DB\n",
			want: true,
		},
		{
			name: "blank import of a driver",
			path: "internal/api/handler.go",
			body: "package api\n\nimport _ \"github.com/lib/pq\"\n",
			want: true,
		},
		{
			name: "a package under a forbidden root",
			path: "internal/api/handler.go",
			body: "package api\n\nimport \"gorm.io/driver/postgres\"\n\nvar _ = postgres.Open\n",
			want: true,
		},
		{
			name: "the repositories themselves",
			path: "internal/api/handler.go",
			body: "package api\n\nimport \"github.com/ovumcy/ovumcy-web/internal/db\"\n\nvar _ = db.Open\n",
			want: true,
		},
		{
			name: "the port surface is held to the same rule",
			path: "internal/apideps/ports.go",
			body: "package apideps\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n",
			want: true,
		},
		{
			name: "named in a comment, which is prose",
			path: "internal/api/middleware.go",
			body: "package api\n\n// and database/sql waits for a free connection forever\nfunc F() {}\n",
			want: false,
		},
		{
			name: "a quoted string that is not an import",
			path: "internal/api/handler.go",
			body: "package api\n\nvar driverName = \"gorm.io/gorm\"\n",
			want: false,
		},
		{
			name: "a path that merely begins with a forbidden one",
			path: "internal/api/handler.go",
			body: "package api\n\nimport (\n\t\"gorm.iox/thing\"\n\t\"database/sqlx\"\n)\n\nvar _ = thing.X\nvar _ = sqlx.X\n",
			want: false,
		},
		{
			name: "a test file, where a fixture needs a real database",
			path: "internal/api/handler_test.go",
			body: "package api\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n",
			want: false,
		},
		{
			name: "a layer the rule is not about",
			path: "internal/services/svc.go",
			body: "package services\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scan(t, writeTree(t, map[string]string{tc.path: tc.body}))
			if tc.want && len(got) == 0 {
				t.Fatalf("expected an %s finding, got none", ruleAPIImports)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("expected no finding, got %v: %v", rulesIn(got), got)
			}
			for _, f := range got {
				if f.rule != ruleAPIImports {
					t.Fatalf("expected only %s findings, got %s: %v", ruleAPIImports, f.rule, f)
				}
			}
		})
	}
}

// The class this command exists for. Each half of the file, taken alone, is text
// no pattern for the forbidden import can match — which is exactly what an
// event-shaped rule sees when the two halves arrive as two edits. The tree is
// asked instead, and the tree has the import.
func TestTheRuleReadsTheTreeAndNotTheEditThatProducedIt(t *testing.T) {
	const forbidden = "gorm.io/gorm"
	firstEdit := "package api\n\nimport (\n\t\"gorm.i"
	secondEdit := "o/gorm\"\n)\n"

	for _, half := range []string{firstEdit, secondEdit} {
		if strings.Contains(half, forbidden) {
			t.Fatalf("this test is not about the fragment window if a half already holds %q: %q", forbidden, half)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "api", "handler.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(firstEdit), 0o644); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := handle.WriteString(secondEdit); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := scan(t, dir)
	if len(got) != 1 || got[0].rule != ruleAPIImports {
		t.Fatalf("expected one %s finding for the assembled file, got %v: %v", ruleAPIImports, rulesIn(got), got)
	}
}

func TestAFileThatDoesNotParseIsReportedRatherThanSkipped(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"internal/api/handler.go": "package api\n\nfunc F( {\n",
	})
	got := scan(t, dir)
	if len(got) != 1 || got[0].rule != ruleUnreadable {
		t.Fatalf("expected one %s finding, got %v: %v", ruleUnreadable, rulesIn(got), got)
	}
}

// Dot directories hold checkouts of this same repository — the worktrees under
// .tmp, git's own store — and testdata holds source the go tool does not build
// either. Scanning any of them reports one violation once per copy.
func TestTreesThatAreNotThisModuleAreNotScanned(t *testing.T) {
	violation := "package api\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n"
	dir := writeTree(t, map[string]string{
		".tmp/worktree/internal/api/handler.go":   violation,
		"internal/api/testdata/golden/handler.go": violation,
		"node_modules/pkg/internal/api/x.go":      violation,
		"vendor/dep/internal/api/x.go":            violation,
	})
	if got := scan(t, dir); len(got) != 0 {
		t.Fatalf("expected nothing outside the module to be scanned, got %v: %v", rulesIn(got), got)
	}
}

// probeModule writes a module that resolves gorm.io/gorm to a local stand-in, so
// the typed pass runs offline against a package whose shape the test states.
func probeModule(t *testing.T, probe string) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"go.mod":           "module example.com/probe\n\ngo 1.26\n\nrequire gorm.io/gorm v0.0.0\n\nreplace gorm.io/gorm => ./gormstub\n",
		"gormstub/go.mod":  "module gorm.io/gorm\n\ngo 1.26\n",
		"gormstub/gorm.go": fixture(t, "gormstub.go.txt"),
		"probe.go":         fixture(t, probe),
	})
}

// The receiver decides, not the spelling. Both directions are the point: the
// rule must catch gorm's method reached through a wrapper, and must let through
// a method of the same name that gorm did not declare.
func TestTheSchemaRuleIsJudgedByTheReceiverAndNotByTheName(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    bool
	}{
		{name: "gorm's own DB", fixture: "gorm-db.go.txt", want: true},
		{name: "gorm's DB reached through an embedding wrapper", fixture: "embedded-wrapper.go.txt", want: true},
		{name: "a same-named method gorm did not declare", fixture: "same-name-not-gorm.go.txt", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scan(t, probeModule(t, tc.fixture))
			if tc.want && (len(got) != 1 || got[0].rule != ruleAutoMigrate) {
				t.Fatalf("expected one %s finding, got %v: %v", ruleAutoMigrate, rulesIn(got), got)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("expected no finding, got %v: %v", rulesIn(got), got)
			}
		})
	}
}

// A tree that does not type-check is a tree whose receivers did not resolve, and
// that is the one question the typed pass exists to answer. Answering "clean"
// there would hand every surface above it an exit code that means nothing.
func TestATreeThatDoesNotTypeCheckIsRefusedRatherThanPassed(t *testing.T) {
	_, err := run(probeModule(t, "does-not-type-check.go.txt"))
	if err == nil {
		t.Fatal("expected a refusal for a tree that does not type-check, got a clean scan")
	}
	if !strings.Contains(err.Error(), "does not type-check") {
		t.Fatalf("the refusal should name its cause, got %q", err)
	}
}

// The prefilter is what keeps the common case cheap: with no candidate anywhere,
// no package is loaded at all. A tree with no go.mod cannot be loaded, so a clean
// scan of one is the proof that the typed pass was never entered.
func TestTheTypedPassIsNotEnteredWithoutACandidate(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"internal/db/repo.go": "package db\n\nimport \"gorm.io/gorm\"\n\nfunc Save(db *gorm.DB) error { return db.Save(nil).Error }\n",
	})
	if got := scan(t, dir); len(got) != 0 {
		t.Fatalf("expected a clean scan without a module, got %v: %v", rulesIn(got), got)
	}
}
