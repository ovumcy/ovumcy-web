package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// calendarFeedAccessColumns are the three columns that decide whether a
// subscribe URL resolves. calendar_feed_revealed_at is deliberately absent: it
// records that a URL was shown once, not that one works, so a function that
// only touches it changes no access.
var calendarFeedAccessColumns = []string{
	`"calendar_feed_selector"`,
	`"calendar_feed_verifier_hash"`,
	`"calendar_feed_verifier_mac"`,
}

// exemptCalendarFeedWriters are the functions that write one of those columns
// and deliberately do NOT advance the fence. Each carries the reason, because an
// exemption without one is how this guard would quietly become an allowlist for
// whatever was easiest to skip.
var exemptCalendarFeedWriters = map[string]string{
	"DisarmAllCalendarFeedTokens":        "the restore fence's own bulk disarm: the fence records a fresh token in both halves immediately after it, so advancing here would mint two tokens for one event",
	"DisarmCalendarFeedTokensWithoutMAC": "the key-rotation sentinel's bulk disarm: its trigger is a changed SECRET_KEY, and the epoch that detects that is derived from the key rather than from stored progress, so a restore cannot roll it back into agreement",
	"BackfillCalendarFeedVerifierMAC":    "neither grants nor removes access: it fills a derived column for a token that already verified, on the read path every first poll takes",
}

// alwaysAdvancingWriters change which feeds are armed WITHOUT naming a feed
// column, so the scan cannot find them. Naming them by hand is the compromise
// this one case needs; each says why.
var alwaysAdvancingWriters = map[string]string{
	"DeleteAccountAndRelatedData": "the erased account's feed leaves with its row, and a restore brings both back together",
}

// TestEveryCalendarFeedWriterAdvancesTheRestoreFence is the completeness guard
// the fence depends on. The fence can only see a restore if every change to the
// armed-feed set was recorded outside the database; one writer that skips the
// advance leaves exactly one route by which a revoked subscribe URL returns from
// a backup, and nothing else in the suite would notice.
//
// The set is derived from the file rather than re-listed: a hand-written mirror
// of the writers would agree with itself while a new writer went unguarded.
func TestEveryCalendarFeedWriterAdvancesTheRestoreFence(t *testing.T) {
	const path = "user_repository.go"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	writers := map[string]bool{}
	advances := map[string]bool{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		body := string(source[fileSet.Position(function.Body.Pos()).Offset:fileSet.Position(function.Body.End()).Offset])
		if strings.Contains(body, "advanceCalendarFeedFence(ctx)") {
			advances[function.Name.Name] = true
		}
		for _, column := range calendarFeedAccessColumns {
			if strings.Contains(body, column) {
				writers[function.Name.Name] = true
				break
			}
		}
	}

	// Anti-vacuity, on two names this test owns: arming and revoking must both
	// be found, or the scan is looking at the wrong thing and would report
	// success over an empty set.
	for _, required := range []string{"SaveCalendarFeedToken", "ClearCalendarFeedToken"} {
		if !writers[required] {
			t.Fatalf("the scan did not find %s among the feed writers (%v): it is not measuring what it claims", required, sortedNamesOf(writers))
		}
	}

	var missing []string
	for name := range writers {
		if _, exempt := exemptCalendarFeedWriters[name]; exempt {
			continue
		}
		if !advances[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these writes change which calendar feeds are armed but never record it outside the database, so a backup restore would undo them silently: %v", missing)
	}

	for name, reason := range alwaysAdvancingWriters {
		if !advances[name] {
			t.Fatalf("%s must advance the restore fence (%s)", name, reason)
		}
	}

	// A stale exemption is an exemption nobody can see is stale.
	for name := range exemptCalendarFeedWriters {
		if !writers[name] {
			t.Fatalf("exempt writer %s no longer writes a feed access column: drop the exemption rather than leaving it standing", name)
		}
	}
}

func sortedNamesOf(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
