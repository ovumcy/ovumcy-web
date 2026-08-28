// Package backuprestoredoc EXECUTES the backup and restore runbook in
// docs/self-hosted.md instead of trusting its prose.
//
// The runbook documents two procedures, one per storage engine, and makes a
// specific claim about each: the Postgres restore, with the schema dropped
// first and `psql` run under `-v ON_ERROR_STOP=1`, brings back every table,
// row and sequence the dump carried; the SQLite named-volume archive captures
// `ovumcy.db`, `ovumcy.db-wal` and `ovumcy.db-shm` together, and the restore
// puts them back. Until this package, both claims and all four destructive
// commands existed only as prose — a repository-wide search for `pg_dump`,
// `ON_ERROR_STOP`, the schema drop, `docker volume rm` or `tar xzf` matched
// documentation and the changelog, never a test, a script or a workflow — so a
// real incident would have been the first end-to-end run of either.
//
// The commands are therefore READ OUT OF THE DOCUMENT and executed verbatim
// against ephemeral resources. Nothing here restates a command as a Go string
// literal. What each half substitutes, and why the substitution is the thing
// that keeps a `go test ./...` on a self-hoster’s own machine away from a real
// deployment, is stated where it happens: composeExecPrefix below for Postgres,
// the volume name and the compose lifecycle bracket in volume_test.go, and
// refusedInAnExecutedCommand in shell_test.go, which refuses to run whatever a
// substitution missed. A drift between the prose an operator follows and a
// procedure that works is what turns this red.
//
// What the two halves cover:
//
//   - the documented `pg_dump` backup command, and the documented restore in
//     both its steps — dropping and recreating the `public` schema, then the
//     `-v ON_ERROR_STOP=1` replay;
//   - the claim about what that restore leaves behind, over the application’s
//     own migrated schema and with a deliberately drifted intermediate
//     generation, so a restore that moved nothing cannot pass. What "the same
//     export" means, and the two differences that survive any restore, are
//     export_compare_test.go’s subject;
//   - the runbook’s own "data intact, or drifted since the backup" table row —
//     the restore that exits 0 having restored no row — as the counterfactual
//     that makes the drop step and ON_ERROR_STOP load-bearing rather than
//     decorative, down to the sequences it rewinds and the collision that
//     follows;
//   - the documented named-volume archive and restore (`tar czf` / `docker
//     volume rm` + `create` / `tar xzf`), against an ephemeral volume holding
//     the application’s own SQLite database, drifted the same three ways and
//     read back through the repositories;
//   - the whole-WAL-set claim: the fixture is written with the connection held
//     OPEN, so the rows are in `ovumcy.db-wal` rather than in the database
//     file, and an archive that missed the sidecars restores the clean, empty
//     instance the runbook’s Post-Restore Verification warns about.
//
// What stays outside, deliberately: `docker compose down` / `up -d` are
// asserted present in the restore block and then removed rather than run —
// this package never starts the application, it writes the volume and reads it
// back itself — and the operator-side checklist in Post-Restore Verification,
// which is performed against a running instance and cannot be seen from here.
// Its ONE property that can be seen from here is checked:
// TestPostRestoreVerificationPointsAtTheCalendarFeedNote holds the checklist to
// the cross-reference an operator needs and nothing else enforces.
package backuprestoredoc

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// runbookPath is the operator document under test, and postgresSection the
	// heading whose fenced blocks are the Postgres procedure — the named-volume
	// sections are volume_test.go's. Reading a named section rather than the
	// whole file keeps every extraction honest: a renamed or removed heading
	// fails there instead of quietly matching some other `pg_dump` further down
	// the page.
	runbookPath     = "docs/self-hosted.md"
	postgresSection = "## Backup and Restore Contract"

	// postRestoreSection is the checklist an operator works through after a
	// restore, and the two constants under it are the ends of the one
	// cross-reference it has to carry. The link itself is DERIVED from the
	// heading rather than written out a second time, so a heading that is
	// reworded takes the expected anchor with it instead of leaving a link that
	// still matches a section that no longer exists.
	postRestoreSection      = "## Post-Restore Verification"
	calendarFeedNoteDoc     = "docs/gdpr.md"
	calendarFeedNoteHeading = "## Backup Restore and the Calendar Feed"

	// composeExecPrefix is how the runbook reaches the database: the bundled
	// compose stack's postgres service, without a TTY. It is the ONE part of a
	// documented command this guard rewrites — `docker exec -i <container>` is
	// the same call against a container named directly instead of resolved by
	// service name — and everything to its right, redirections and pipes
	// included, runs exactly as written. A runbook that switches transport
	// (podman, a remote psql, a different service name) stops containing this
	// prefix and fails the extraction rather than silently running something
	// else.
	composeExecPrefix = "docker compose exec -T postgres"

	// The two additions the runbook declares load-bearing. They are asserted
	// present, not supplied: the guard would otherwise pass on a document that
	// had dropped them.
	dropSchemaStatement = "DROP SCHEMA public CASCADE"
	createSchemaClause  = "CREATE SCHEMA public"
	onErrorStopFlag     = "-v ON_ERROR_STOP=1"
)

// bashBlock matches one fenced shell block of the runbook.
var bashBlock = regexp.MustCompile("(?s)```bash\n(.*?)```")

// dumpRedirect and replaySource read the dump file path off the two commands
// that name it. They exist to cross-check the pair: a runbook whose backup
// writes one path and whose restore replays another documents a restore that
// cannot work, and no assertion about either command alone would see it.
var (
	dumpRedirect = regexp.MustCompile(`>\s*(\S+\.sql)\s*$`)
	replaySource = regexp.MustCompile(`^cat\s+(\S+\.sql)\s*\|`)
)

// runbookCommands is the Postgres procedure as the document spells it.
type runbookCommands struct {
	// backup is the whole first fenced block — `mkdir -p backups` and the
	// pg_dump redirect — kept together because the redirect depends on the
	// mkdir.
	backup string
	// drop and replay are the two commands of the restore block, in order.
	drop   string
	replay string
	// dumpFile is the path both of them agree on, relative to the working
	// directory the operator runs in.
	dumpFile string
}

// documentedPostgresCommands reads the Postgres backup and restore commands out
// of the runbook. Every shape assertion here fails CLOSED: an extraction that
// found nothing, or found something it did not recognise, is this guard's own
// failure rather than a reason to test less.
func documentedPostgresCommands(t *testing.T) runbookCommands {
	t.Helper()

	section := runbookSectionText(t, postgresSection)
	blocks := bashBlock.FindAllStringSubmatch(section, -1)
	if len(blocks) != 2 {
		t.Fatalf("%s: expected exactly 2 shell blocks under %q (backup, then restore), found %d — the extraction no longer matches the document", runbookPath, postgresSection, len(blocks))
	}

	backup := strings.TrimSpace(blocks[0][1])
	restore := strings.TrimSpace(blocks[1][1])

	if !strings.Contains(backup, "pg_dump") {
		t.Fatalf("%s: the first shell block under %q does not run pg_dump:\n%s", runbookPath, postgresSection, backup)
	}

	// The restore block is two commands separated by a blank line, and an
	// operator runs them one after the other. Splitting keeps the failure
	// legible — which of the two steps died — and gives the counterfactual
	// below a replay command it can run on its own.
	steps := splitBlankLineSeparated(restore)
	if len(steps) != 2 {
		t.Fatalf("%s: expected the restore block to be exactly 2 commands (drop the schema, then replay the dump), found %d:\n%s", runbookPath, len(steps), restore)
	}
	drop, replay := steps[0], steps[1]

	for _, required := range []struct {
		command string
		name    string
		substr  string
	}{
		{drop, "the schema-drop step", dropSchemaStatement},
		{drop, "the schema-drop step", createSchemaClause},
		{drop, "the schema-drop step", onErrorStopFlag},
		{replay, "the dump replay", onErrorStopFlag},
	} {
		if !strings.Contains(required.command, required.substr) {
			t.Fatalf("%s: %s no longer contains %q — the runbook calls it load-bearing, so this guard refuses to run a procedure without it:\n%s", runbookPath, required.name, required.substr, required.command)
		}
	}

	commands := runbookCommands{backup: backup, drop: drop, replay: replay}
	for _, command := range []string{commands.backup, commands.drop, commands.replay} {
		if !strings.Contains(command, composeExecPrefix) {
			t.Fatalf("%s: documented command does not reach the database through %q, so this guard cannot run it as written:\n%s", runbookPath, composeExecPrefix, command)
		}
	}

	commands.dumpFile = agreedDumpFile(t, commands)
	return commands
}

// runbookSectionText returns the text of one section of the runbook, from its
// heading to the next one.
func runbookSectionText(t *testing.T, heading string) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), filepath.FromSlash(runbookPath))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", runbookPath, err)
	}

	document := strings.ReplaceAll(string(content), "\r\n", "\n")
	start := strings.Index(document, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("%s: section %q not found — a renamed heading leaves this guard reading nothing, so it fails here instead", runbookPath, heading)
	}
	rest := document[start+1:]
	end := strings.Index(rest[len(heading):], "\n## ")
	if end < 0 {
		return rest
	}
	return rest[:len(heading)+end]
}

// agreedDumpFile returns the dump path named by both the backup and the replay,
// failing when they disagree.
func agreedDumpFile(t *testing.T, commands runbookCommands) string {
	t.Helper()

	written := ""
	for _, line := range strings.Split(commands.backup, "\n") {
		if match := dumpRedirect.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			written = match[1]
		}
	}
	if written == "" {
		t.Fatalf("%s: the backup block redirects to no .sql file:\n%s", runbookPath, commands.backup)
	}

	match := replaySource.FindStringSubmatch(strings.TrimSpace(commands.replay))
	if match == nil {
		t.Fatalf("%s: the dump replay reads from no .sql file:\n%s", runbookPath, commands.replay)
	}
	if match[1] != written {
		t.Fatalf("%s: the backup writes %q but the restore replays %q — following the runbook as written would restore the wrong file, or none", runbookPath, written, match[1])
	}
	return written
}

// splitBlankLineSeparated splits a shell block into the commands an operator
// runs one at a time.
func splitBlankLineSeparated(block string) []string {
	var commands []string
	for _, chunk := range strings.Split(block, "\n\n") {
		if trimmed := strings.TrimSpace(chunk); trimmed != "" {
			commands = append(commands, trimmed)
		}
	}
	return commands
}

// TestDocumentedPostgresCommandsAreTheOnesThisGuardRuns pins the extraction
// itself, so a reader can see which strings the executable guard below actually
// takes out of the runbook — and so the fail-closed branches are exercised
// against fixtures rather than only in the day they fire for real.
func TestDocumentedPostgresCommandsAreTheOnesThisGuardRuns(t *testing.T) {
	commands := documentedPostgresCommands(t)

	if !strings.Contains(commands.backup, composeExecPrefix+" sh -lc 'pg_dump") {
		t.Errorf("backup command is not the documented pg_dump call: %q", commands.backup)
	}
	if !strings.HasPrefix(commands.drop, composeExecPrefix) {
		t.Errorf("schema-drop step does not start with the documented transport: %q", commands.drop)
	}
	if !strings.HasPrefix(commands.replay, "cat ") {
		t.Errorf("dump replay does not pipe the dump file into psql: %q", commands.replay)
	}
	if commands.dumpFile == "" {
		t.Error("no dump file path agreed between the backup and the restore")
	}
}

// TestChangeDetectionRunsTheGoLanesForARunbookOnlyDiff asserts that CI still
// detects a change to the document this package executes, and still lets that
// detection reach the lanes that run it.
//
// Both halves matter and neither implies the other. The Go lanes are skipped
// for a diff CI judges to be documentation only, and this runbook is
// documentation by every spelling the filter knows — the `docs/` prefix and
// the `.md` suffix both — so it is detected on its own and then FORCES
// `run_core`, the ground the Go lanes derive from. It deliberately does not
// force `run_e2e`: the guard rides the Go lanes alone, and a runbook typo
// must not buy a full browser battery that cannot look at it.
//
// Measured 2026-08-20: 10 of the 22 commits touching the file since June
// carried nothing else, so half of all runbook edits ride on this. Every
// piece of it is a rule keyed on a SPELLING — a rename here, or a tidy-up
// there, would silently restore the hole. This is the check that makes either
// fail instead.
func TestChangeDetectionRunsTheGoLanesForARunbookOnlyDiff(t *testing.T) {
	path := filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml")
	workflow, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the CI workflow: %v", err)
	}

	for _, required := range []struct {
		fragment string
		why      string
	}{
		{
			fragment: "grep -E '^" + strings.ReplaceAll(runbookPath, ".", `\.`) + "$'",
			why:      "the runbook is no longer detected at all, so a diff touching only it reads as documentation",
		},
		{
			fragment: "if [ -n \"${runbook_changes:-}\" ]; then\n            run_core=true",
			why:      "the detection no longer forces the core suite, so the Go lanes skip the diff they exist to judge here",
		},
		{
			fragment: "[ -z \"$beyond_specs\" ] && [ -z \"${runbook_changes:-}\" ]",
			why:      "the browser-spec-only slice takes the forced core suite straight back, since it excuses every docs/ path",
		},
	} {
		if !strings.Contains(string(workflow), required.fragment) {
			t.Errorf(".github/workflows/ci.yml no longer carries %q: %s, and this guard would not run on a change to %s", required.fragment, required.why, runbookPath)
		}
	}
}

// TestPostRestoreVerificationPointsAtTheCalendarFeedNote holds the restore
// checklist to the one consequence of a restore that is invisible from inside
// it: the calendar-feed columns come back with everything else, so a
// subscription an owner revoked or rotated AFTER the backup was taken is armed
// again at its old subscribe URL. docs/gdpr.md records that consequence, and an
// operator who follows this runbook alone never opens that page, which is why
// the checklist has to carry the pointer itself.
//
// Both ends of the link are checked, because either one rotting leaves the same
// operator uninformed: the pointer has to be in the section being read, and the
// section it points at has to still exist under the anchor it resolves to.
func TestPostRestoreVerificationPointsAtTheCalendarFeedNote(t *testing.T) {
	anchor := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(calendarFeedNoteHeading, "## "), " ", "-"))
	link := strings.TrimPrefix(calendarFeedNoteDoc, "docs/") + "#" + anchor

	checklist := runbookSectionText(t, postRestoreSection)
	if !strings.Contains(checklist, link) {
		t.Errorf("%s: section %q does not link %q — an operator following only the runbook is never told that a revoked or rotated calendar feed comes back with the restored data", runbookPath, postRestoreSection, link)
	}

	content, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(calendarFeedNoteDoc)))
	if err != nil {
		t.Fatalf("read %s: %v", calendarFeedNoteDoc, err)
	}
	if !strings.Contains(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n"+calendarFeedNoteHeading+"\n") {
		t.Errorf("%s: heading %q is gone, so the runbook's cross-reference resolves to nothing", calendarFeedNoteDoc, calendarFeedNoteHeading)
	}
}

// TestSplitBlankLineSeparatedKeepsOperatorSteps proves the restore block is cut
// where an operator would cut it, using fixtures rather than the real document.
func TestSplitBlankLineSeparatedKeepsOperatorSteps(t *testing.T) {
	cases := []struct {
		name  string
		block string
		want  []string
	}{
		{"two steps", "first --flag\n\nsecond --flag\n", []string{"first --flag", "second --flag"}},
		{"one step", "only --flag\n", []string{"only --flag"}},
		{"trailing blank lines are not a step", "first\n\n\nsecond\n\n", []string{"first", "second"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := splitBlankLineSeparated(testCase.block)
			if len(got) != len(testCase.want) {
				t.Fatalf("got %d commands %q, want %d", len(got), got, len(testCase.want))
			}
			for i := range got {
				if got[i] != testCase.want[i] {
					t.Fatalf("command %d: got %q, want %q", i, got[i], testCase.want[i])
				}
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
