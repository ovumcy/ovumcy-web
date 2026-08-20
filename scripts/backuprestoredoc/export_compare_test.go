package backuprestoredoc

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The runbook's claim is about what an export looks like after a restore, so
// this file is where "the same export" is defined — and the definition is not
// `bytes.Equal`, because two dumps of one unchanged database are not byte-equal
// on any currently shipping pg_dump:
//
//   - `pg_dump` 17.6+ opens and closes every dump with `\restrict <token>` /
//     `\unrestrict <token>`, and the token is fresh on every run. It is a psql
//     meta-command guard, not data.
//   - The documented restore drops and recreates `public`, so the schema is no
//     longer the one initdb made. Every LATER dump therefore carries an
//     explicit ownership/comment/ACL block for `public` that the dump taken
//     before the restore did not — restoreResidue below, three statements, all
//     about the schema object itself and none about a row.
//
// Both were measured, not assumed: they are the whole difference between a dump
// taken before the documented restore and one taken after it, over this
// application's schema. So a comparison drops the first class and accounts for
// the second EXPLICITLY, and anything beyond them is a failure — a restore that
// lost a row, a column or a sequence has nowhere to hide.

// copyBlockStart and copyBlockEnd bracket the regions of a dump that are data
// rather than SQL. Nothing inside them is ever normalised away: a day-log note
// is free text and may begin with `--`, and stripping it as a comment would
// make this guard blind to exactly the loss it exists to catch.
var (
	copyBlockStart = regexp.MustCompile(`^COPY .* FROM stdin;$`)
	copyBlockEnd   = `\.`

	// restrictGuard is pg_dump 17.6+'s per-dump random token.
	restrictGuard = regexp.MustCompile(`^\\(un)?restrict \S+$`)

	// restoreResidue is what the documented restore leaves behind in every
	// later dump, because `CREATE SCHEMA public` produces a schema owned by
	// the restoring role instead of the pinned one initdb created. Each
	// pattern must actually appear (see assertExportMatchesApartFromRestoreResidue):
	// if a pg_dump release stops emitting one, the runbook's wording about it
	// is stale and this guard says so instead of quietly passing.
	restoreResidue = []*regexp.Regexp{
		regexp.MustCompile(`^ALTER SCHEMA public OWNER TO \S+;$`),
		regexp.MustCompile(`^COMMENT ON SCHEMA public IS '.*';$`),
		regexp.MustCompile(`^REVOKE USAGE ON SCHEMA public FROM PUBLIC;$`),
	}
)

// reportedDifferenceLimit bounds how many differing lines a failed comparison
// prints, so a wholesale mismatch cannot bury the log.
const reportedDifferenceLimit = 8

// canonicalExport reduces a dump to the lines that carry its schema and its
// data: comments, blank lines and the per-dump restrict tokens go, everything
// inside a COPY block stays exactly as it is.
func canonicalExport(export []byte) []string {
	var (
		canonical []string
		inData    bool
	)
	for _, line := range strings.Split(string(export), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if inData {
			canonical = append(canonical, line)
			if line == copyBlockEnd {
				inData = false
			}
			continue
		}
		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "--"):
		case restrictGuard.MatchString(line):
		default:
			canonical = append(canonical, line)
			if copyBlockStart.MatchString(line) {
				inData = true
			}
		}
	}
	return canonical
}

// sequenceSetting matches the `setval` calls a dump ends with. They are the
// part of a dump that a failed replay still lands: `CREATE TABLE` and `COPY`
// fail against a populated schema, `setval` does not, which is why the two
// halves of an export are compared separately below.
var sequenceSetting = regexp.MustCompile(`^SELECT pg_catalog\.setval\(.*\);$`)

// copyData returns the rows of a dump: every line of every COPY block,
// terminators included, and nothing else.
func copyData(export []byte) []string {
	var (
		rows   []string
		inData bool
	)
	for _, line := range canonicalExport(export) {
		switch {
		case inData:
			rows = append(rows, line)
			if line == copyBlockEnd {
				inData = false
			}
		case copyBlockStart.MatchString(line):
			rows = append(rows, line)
			inData = true
		}
	}
	return rows
}

// sequenceSettings returns the `setval` calls of a dump, in order.
func sequenceSettings(export []byte) []string {
	var settings []string
	for _, line := range canonicalExport(export) {
		if sequenceSetting.MatchString(line) {
			settings = append(settings, line)
		}
	}
	return settings
}

func sameLines(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for i := range first {
		if first[i] != second[i] {
			return false
		}
	}
	return true
}

// exportsMatch reports whether two dumps carry the same schema and the same
// data.
func exportsMatch(before, after []byte) bool {
	return sameLines(canonicalExport(before), canonicalExport(after))
}

// assertExportMatchesApartFromRestoreResidue is the runbook's claim, stated as
// precisely as the instrument allows: the dump taken after the restore carries
// every line the dump taken before it carried, in order, and the only lines it
// adds are the recreated-schema residue named above — each of which it must
// actually add.
func assertExportMatchesApartFromRestoreResidue(t *testing.T, before, after []byte) {
	t.Helper()

	beforeLines := canonicalExport(before)
	afterLines := canonicalExport(after)

	seen := make([]bool, len(restoreResidue))
	var (
		differences []string
		i, j        int
	)
	for i < len(beforeLines) && j < len(afterLines) {
		if beforeLines[i] == afterLines[j] {
			i++
			j++
			continue
		}
		if index := matchesRestoreResidue(afterLines[j]); index >= 0 {
			seen[index] = true
			j++
			continue
		}
		differences = append(differences, fmt.Sprintf("  before the dump:   %s\n  after the restore: %s", beforeLines[i], afterLines[j]))
		if len(differences) == reportedDifferenceLimit {
			differences = append(differences, "  …")
			break
		}
		i++
		j++
	}
	for ; j < len(afterLines) && len(differences) < reportedDifferenceLimit; j++ {
		if index := matchesRestoreResidue(afterLines[j]); index >= 0 {
			seen[index] = true
			continue
		}
		differences = append(differences, "  only after the restore: "+afterLines[j])
	}
	for ; i < len(beforeLines) && len(differences) < reportedDifferenceLimit; i++ {
		differences = append(differences, "  only before the dump: "+beforeLines[i])
	}

	if len(differences) > 0 {
		t.Fatalf("the documented restore did not reproduce the dumped database — %s claims it does:\n%s", runbookPath, strings.Join(differences, "\n"))
	}
	for index, found := range seen {
		if !found {
			t.Fatalf("the dump taken after the restore no longer carries %q, which %s tells the operator to expect: the runbook's account of the restore is stale", restoreResidue[index], runbookPath)
		}
	}
}

func matchesRestoreResidue(line string) int {
	for index, pattern := range restoreResidue {
		if pattern.MatchString(line) {
			return index
		}
	}
	return -1
}

// differenceReport describes where two dumps diverge, for the assertions that
// only need to say "these two are not the same".
func differenceReport(before, after []byte) string {
	beforeLines := canonicalExport(before)
	afterLines := canonicalExport(after)

	var report []string
	for i := 0; i < len(beforeLines) && i < len(afterLines); i++ {
		if beforeLines[i] == afterLines[i] {
			continue
		}
		if len(report) == reportedDifferenceLimit {
			report = append(report, "  …")
			break
		}
		report = append(report, fmt.Sprintf("line %d:\n  first:  %s\n  second: %s", i+1, beforeLines[i], afterLines[i]))
	}
	if len(beforeLines) != len(afterLines) {
		report = append(report, fmt.Sprintf("the dumps differ in length: %d lines against %d", len(beforeLines), len(afterLines)))
	}
	if len(report) == 0 {
		return "  (no difference found)"
	}
	return strings.Join(report, "\n")
}

// TestCanonicalExportKeepsDataAndDropsOnlyPerDumpNoise proves the normalisation
// above on fixtures: it must drop the random restrict token and the SQL
// comments around a dump, and must NOT touch a row whose free-text field looks
// like one.
func TestCanonicalExportKeepsDataAndDropsOnlyPerDumpNoise(t *testing.T) {
	dump := strings.Join([]string{
		`\restrict AbCdEf123`,
		"",
		"--",
		"-- Name: daily_logs; Type: TABLE; Schema: public; Owner: ovumcy",
		"--",
		"CREATE TABLE public.daily_logs (id bigint NOT NULL);",
		"COPY public.daily_logs (id, notes) FROM stdin;",
		"1\t-- not a comment, an owner's note",
		"2\t",
		`\.`,
		"",
		`\unrestrict AbCdEf123`,
		"",
	}, "\n")

	want := []string{
		"CREATE TABLE public.daily_logs (id bigint NOT NULL);",
		"COPY public.daily_logs (id, notes) FROM stdin;",
		"1\t-- not a comment, an owner's note",
		"2\t",
		`\.`,
	}

	got := canonicalExport([]byte(dump))
	if len(got) != len(want) {
		t.Fatalf("canonical dump has %d lines, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonical line %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestExportsMatchSeesALostRow is the positive control for the comparison: the
// normalisation must not be so eager that a missing row survives it.
func TestExportsMatchSeesALostRow(t *testing.T) {
	with := []byte("COPY public.users (id) FROM stdin;\n1\n2\n\\.\n")
	without := []byte("COPY public.users (id) FROM stdin;\n1\n\\.\n")

	if !exportsMatch(with, with) {
		t.Error("a dump does not compare equal to itself")
	}
	if exportsMatch(with, without) {
		t.Error("a dump missing a row compares equal to one carrying it")
	}
	if !exportsMatch(with, []byte("\\restrict xyz\n"+string(with))) {
		t.Error("the per-dump restrict token is not being normalised away")
	}
}
