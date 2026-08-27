package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// The layering rule, one direction per case. The negatives carry the weight
// here: this rule denies upward edges and says nothing about the cross-cutting
// packages or about net/http, and a rule that quietly widened to either would
// refuse work the contract permits.
func TestTheLayersImportInOneDirectionOnly(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want bool
	}{
		{
			name: "services reaching up into transport",
			path: "internal/services/cycle.go",
			body: "package services\n\nimport \"github.com/ovumcy/ovumcy-web/internal/api\"\n\nvar _ = api.New\n",
			want: true,
		},
		{
			name: "services reaching up into the port surface",
			path: "internal/services/cycle.go",
			body: "package services\n\nimport \"github.com/ovumcy/ovumcy-web/internal/apideps\"\n\nvar _ = apideps.X\n",
			want: true,
		},
		{
			name: "persistence reaching up into domain logic",
			path: "internal/db/repo.go",
			body: "package db\n\nimport \"github.com/ovumcy/ovumcy-web/internal/services\"\n\nvar _ = services.New\n",
			want: true,
		},
		{
			name: "persistence reaching up into transport",
			path: "internal/db/repo.go",
			body: "package db\n\nimport \"github.com/ovumcy/ovumcy-web/internal/api\"\n\nvar _ = api.New\n",
			want: true,
		},
		{
			name: "a persisted type reaching sideways into a cross-cutting package",
			path: "internal/models/day.go",
			body: "package models\n\nimport \"github.com/ovumcy/ovumcy-web/internal/security\"\n\nvar _ = security.Seal\n",
			want: true,
		},
		{
			name: "a persisted type reaching down into the migrations",
			path: "internal/models/day.go",
			body: "package models\n\nimport \"github.com/ovumcy/ovumcy-web/migrations\"\n\nvar _ = migrations.FS\n",
			want: true,
		},
		{
			name: "the direction the contract allows: transport calls a service",
			path: "internal/api/handler.go",
			body: "package api\n\nimport \"github.com/ovumcy/ovumcy-web/internal/services\"\n\nvar _ = services.New\n",
			want: false,
		},
		{
			name: "the direction the contract allows: persistence maps rows to models",
			path: "internal/db/repo.go",
			body: "package db\n\nimport \"github.com/ovumcy/ovumcy-web/internal/models\"\n\nvar _ models.Day\n",
			want: false,
		},
		{
			name: "a cross-cutting package, which the layering does not order",
			path: "internal/services/locale.go",
			body: "package services\n\nimport \"github.com/ovumcy/ovumcy-web/internal/i18n\"\n\nvar _ = i18n.T\n",
			want: false,
		},
		{
			name: "the standard library, which the rule is not about",
			path: "internal/services/webhook.go",
			body: "package services\n\nimport \"net/http\"\n\nvar _ = http.Post\n",
			want: false,
		},
		{
			name: "another module whose path merely begins with this one",
			path: "internal/models/day.go",
			body: "package models\n\nimport \"github.com/ovumcy/ovumcy-web-extras/thing\"\n\nvar _ = thing.X\n",
			want: false,
		},
		{
			name: "a test file, where a repository is proved against its own service",
			path: "internal/db/repo_test.go",
			body: "package db\n\nimport \"github.com/ovumcy/ovumcy-web/internal/services\"\n\nvar _ = services.New\n",
			want: false,
		},
		{
			name: "a test file on the other side of the same seam",
			path: "internal/services/cycle_test.go",
			body: "package services\n\nimport \"github.com/ovumcy/ovumcy-web/internal/api\"\n\nvar _ = api.New\n",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scan(t, writeTree(t, map[string]string{tc.path: tc.body}))
			if tc.want && len(got) == 0 {
				t.Fatalf("expected a %s finding, got none", ruleLayerImports)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("expected no finding, got %v: %v", rulesIn(got), got)
			}
			for _, f := range got {
				if f.rule != ruleLayerImports {
					t.Fatalf("expected only %s findings, got %s: %v", ruleLayerImports, f.rule, f)
				}
			}
		})
	}
}

// A build-tagged file is the hole a package-listing instrument cannot see:
// `go list` answers about the build it was asked for, and internal/services
// really does hold a file (the gofuzz libFuzzer harness) that no ordinary GOOS
// selects. The parse-every-file pass is what closes it, so the closure is
// asserted rather than assumed.
func TestAFileBehindABuildTagIsStillRead(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"internal/services/harness.go": "//go:build gofuzz\n\npackage services\n\nimport \"github.com/ovumcy/ovumcy-web/internal/api\"\n\nvar _ = api.New\n",
	})
	got := scan(t, dir)
	if len(got) != 1 || got[0].rule != ruleLayerImports {
		t.Fatalf("expected one %s finding behind the build tag, got %v: %v", ruleLayerImports, rulesIn(got), got)
	}
}

// A finding has to say which direction was crossed and what to do instead: the
// reader of the refusal is someone who just wrote the import and believes it is
// reasonable.
func TestALayerFindingNamesTheDirectionAndTheRemedy(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"internal/db/repo.go": "package db\n\nimport \"github.com/ovumcy/ovumcy-web/internal/services\"\n\nvar _ = services.New\n",
	})
	got := scan(t, dir)
	if len(got) != 1 {
		t.Fatalf("expected one finding, got %v: %v", rulesIn(got), got)
	}
	for _, want := range []string{
		"internal/db must not import github.com/ovumcy/ovumcy-web/internal/services",
		"internal/services is what calls it",
	} {
		if !strings.Contains(got[0].msg, want) {
			t.Fatalf("the finding should carry %q, got %q", want, got[0].msg)
		}
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

// The absent-role rule, both verdicts, on a tree the test owns. The positives
// are the shapes the defect actually takes; the negatives carry as much, because
// a name-shaped rule that reads prose or a role VALUE as a declaration is the
// failure this one is built to avoid.
func TestAnIdentifierMayNotNameARoleTheProductDoesNotHave(t *testing.T) {
	dir := writeTree(t, map[string]string{
		// Six declarations, one per shape: a type, a method, a short variable
		// declaration, both halves of a range clause, and a plural.
		"internal/services/day_read.go": `package services

type ViewerService struct{ days int }

func (service *ViewerService) FetchDayLogForViewer() int { return service.days }

func listAccounts(accounts []int) int {
	partner := accounts[0]
	for guestIndex, admins := range accounts {
		partner += guestIndex + admins
	}
	return partner
}

func FetchViewersForOwner() []int { return nil }
`,
		// The rule reaches every tree the module builds, not internal/ alone.
		"cmd/ovumcy/config.go": "package main\n\ntype ViewerConfig struct{ partnerMode bool }\n",
		"migrations/embed.go":  "package migrations\n\nvar tenantSchema string\n",
	})

	got := scan(t, dir)
	if len(got) != 9 {
		t.Fatalf("expected nine absent-role findings (six shapes in services, a type and a field in cmd, a var in migrations), got %d: %v", len(got), got)
	}
	for _, f := range got {
		if f.rule != ruleAbsentRole {
			t.Fatalf("expected only %s findings, got %v: %v", ruleAbsentRole, rulesIn(got), got)
		}
	}
}

func TestTheAbsentRoleRuleReadsDeclarationsAndNotProseOrValues(t *testing.T) {
	dir := writeTree(t, map[string]string{
		// Every construct here must pass: the declared role itself, a token that
		// merely CONTAINS a forbidden word, an identifier whose camel boundary
		// spells one across two tokens, the role named in a comment, and the role
		// named as a string VALUE.
		"internal/models/user.go": `package models

// There is no viewer or partner role in this product.
const RoleOwner = "owner"
`,
		"internal/services/policy.go": `package services

func IsOwnerUser(role string) bool { return role == "owner" }

func reviewerCount(unsupportedRole string) int {
	if unsupportedRole == "legacy_viewer" {
		return 0
	}
	return 1
}
`,
		"internal/api/errors.go": "package api\n\nfunc mapDashboardViewError(err error) error { return err }\n",
		// A test file NAMES the absent role on purpose, to prove it is refused.
		// The rule must leave it standing.
		"internal/services/policy_test.go": "package services\n\nfunc TestAViewerRoleIsRefused() { var viewerUser string; _ = viewerUser }\n",
	})

	if got := scan(t, dir); len(got) != 0 {
		t.Fatalf("a declared role, the token \"reviewer\", the View|Error camel boundary, a comment, a role VALUE and a test file must all pass — a rule that flags them teaches suppression instead of renaming; got %v: %v", rulesIn(got), got)
	}
}

// absentRoleSpecimens holds one realistic identifier per forbidden word, written
// out by hand rather than generated from absentRoleWords.
//
// That independence is the whole point. A fixture built FROM the word under test
// proves only that the matcher handles whatever string is in the list — rename
// "moderator" to "moderatr" and a generated ModeratrService still matches, so
// the typo passes. These specimens are fixed text, so deleting a word from
// absentRoleWords, or misspelling it, stops its specimen being flagged and this
// test goes red. It is the lower bound the list would otherwise lack: without
// it, five of the eight words could be dropped and every other test stays green.
var absentRoleSpecimens = map[string]string{
	"viewer":        "ViewerService",
	"partner":       "PartnerAccount",
	"admin":         "AdminConsole",
	"administrator": "AdministratorSeat",
	"guest":         "GuestSession",
	"moderator":     "ModeratorQueue",
	"subscriber":    "SubscriberList",
	"tenant":        "TenantIsolation",
}

func TestEveryAbsentRoleWordIsProvedByARealisticIdentifier(t *testing.T) {
	// Iterate the SPECIMENS, never absentRoleWords. Driving the loop from the
	// list is what makes this shape vacuous: a deleted word is then simply a
	// skipped iteration, and the test stays green about a rule that no longer
	// exists. Driven from fixed text, a deleted or misspelled word stops matching
	// its specimen and this fails.
	words := make([]string, 0, len(absentRoleSpecimens))
	for word := range absentRoleSpecimens {
		words = append(words, word)
	}
	sort.Strings(words)

	for _, word := range words {
		specimen := absentRoleSpecimens[word]
		t.Run(word, func(t *testing.T) {
			got, found := absentRoleWordIn(specimen)
			if !found {
				t.Fatalf("%s must be flagged, and is not — %q is no longer in absentRoleWords, or it is misspelled there. Restore it; a word that stops being enforced silently narrows the rule.", specimen, word)
			}
			if got != word {
				t.Fatalf("%s must be reported as %q, got %q", specimen, word, got)
			}
			// The plural is the shape a list-of-role accessor takes.
			plural := specimen + "s"
			if _, found := absentRoleWordIn(plural); !found {
				t.Fatalf("%s must be flagged too — a plural is the likeliest form of this defect", plural)
			}
		})
	}
}

// The other direction: a word may not be added to absentRoleWords without a
// specimen proving it bites. This one is driven from the list on purpose — it is
// asking about the list's contents, not standing in for them.
func TestEveryAbsentRoleWordHasASpecimen(t *testing.T) {
	for _, word := range absentRoleWords {
		if _, ok := absentRoleSpecimens[word]; !ok {
			t.Fatalf("absentRoleWords lists %q with no specimen in absentRoleSpecimens — a word with no fixture cannot be shown to bite; add a realistic identifier that uses it", word)
		}
	}
}

// The role model is stated in docs/architecture.md and enforced here. This test
// reads the doc rather than copying it, so dropping "viewer" or "partner" from
// absentRoleWords — which would narrow the rule to nothing for the very roles the
// product documents as absent — fails instead of passing quietly.
func TestTheAbsentRoleWordsKeepTheRolesTheArchitectureDeclaresAbsent(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "docs", "architecture.md"))
	if err != nil {
		t.Fatalf("reading docs/architecture.md: %v", err)
	}

	sentence := regexp.MustCompile(`There is no ([a-z]+) or ([a-z]+)\s+role`)
	match := sentence.FindStringSubmatch(strings.ReplaceAll(string(body), "\n", " "))
	if match == nil {
		t.Fatal("docs/architecture.md no longer states the role model as \"There is no <x> or <y> role\" — the lower bound on absentRoleWords was derived from that sentence and must be re-derived, not dropped")
	}

	listed := make(map[string]bool, len(absentRoleWords))
	for _, word := range absentRoleWords {
		listed[word] = true
	}
	for _, declaredAbsent := range match[1:] {
		if !listed[declaredAbsent] {
			t.Fatalf("docs/architecture.md declares %q a role the product does not have, but absentRoleWords no longer forbids it — restore %q to the list; the doc is the source, this list is the enforcement", declaredAbsent, declaredAbsent)
		}
	}
}

// The mirror of the rule above: a role the product DOES declare must never be
// forbidden. internal/models is the source, so the day a second role is
// introduced this fails and names the word to remove, rather than refusing every
// identifier that uses the new role's name.
func TestTheAbsentRoleWordsDoNotForbidADeclaredRole(t *testing.T) {
	declared := declaredRoleValues(t, filepath.Join("..", "..", "internal", "models"))

	// Positive anchor on the product's own constants: if internal/models stops
	// declaring any role, the check below is trivially true and would report
	// success about a role model nobody read.
	if !declared["owner"] {
		t.Fatalf("internal/models declares no Role* constant with the value \"owner\" — the role model this rule is derived from is gone, got %v", declared)
	}

	for _, word := range absentRoleWords {
		if declared[word] {
			t.Fatalf("absentRoleWords forbids %q while internal/models declares it as a role — the product's role model changed, so remove %q from absentRoleWords and update the role line in docs/architecture.md", word, word)
		}
	}
}

// declaredRoleValues reads the role VALUES internal/models declares — every
// `Role…` constant bound to a string literal — so the rule's notion of "a role
// the product has" is the product's own rather than a copy of it.
func declaredRoleValues(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	roles := make(map[string]bool)
	read := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		read++
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, constName := range value.Names {
					if !strings.HasPrefix(constName.Name, "Role") || i >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					text, unquoteErr := strconv.Unquote(literal.Value)
					if unquoteErr != nil {
						continue
					}
					roles[strings.ToLower(text)] = true
				}
			}
		}
	}
	if read == 0 {
		t.Fatalf("%s yielded no production Go file — the declared role set was read from nothing", dir)
	}
	return roles
}
