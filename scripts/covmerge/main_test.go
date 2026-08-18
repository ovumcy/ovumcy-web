package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// merge is the whole command minus its file handling: it folds the given
// profiles in the order they are written and renders the result.
func merge(t *testing.T, profiles ...string) string {
	t.Helper()

	blocks := map[blockKey]blockValue{}
	mode := ""
	for index, profile := range profiles {
		profileMode, err := Accumulate(blocks, profile, sourceName(index))
		if err != nil {
			t.Fatalf("accumulate profile %d: %v", index, err)
		}
		mode = profileMode
	}
	return Render(mode, blocks)
}

func sourceName(index int) string {
	return "shard-" + string(rune('1'+index)) + "/coverage.out"
}

func mergeError(t *testing.T, profiles ...string) string {
	t.Helper()

	blocks := map[blockKey]blockValue{}
	for index, profile := range profiles {
		if _, err := Accumulate(blocks, profile, sourceName(index)); err != nil {
			return err.Error()
		}
	}
	t.Fatalf("merging %d profile(s) succeeded, want an error", len(profiles))
	return ""
}

// The counting property the consumers rest on: a line executed by two shards
// is credited with what both of them did, not with the last writer's number.
func TestCountsForTheSameBlockAreSummed(t *testing.T) {
	first := "mode: atomic\ngithub.com/o/w/internal/api/a.go:10.2,12.3 2 4\n"
	second := "mode: atomic\ngithub.com/o/w/internal/api/a.go:10.2,12.3 2 3\n"

	got := merge(t, first, second)

	want := "mode: atomic\ngithub.com/o/w/internal/api/a.go:10.2,12.3 2 7\n"
	if got != want {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", got, want)
	}
}

// The `-coverpkg` property, and the reason the merge is a UNION rather than a
// concatenation of what each shard executed: every shard's profile lists every
// coverable block in the module, and a package only one shard ran is present
// at count 0 in all the others. A merge that let a 0 overwrite a real count —
// or that dropped the blocks nobody executed — would report a coverage drop
// that no code change caused.
func TestABlockOnlyOneShardExecutedKeepsItsCount(t *testing.T) {
	ran := strings.Join([]string{
		"mode: atomic",
		"github.com/o/w/internal/api/a.go:10.2,12.3 2 5",
		"github.com/o/w/internal/services/s.go:4.1,6.2 1 0",
		"",
	}, "\n")
	didNotRun := strings.Join([]string{
		"mode: atomic",
		"github.com/o/w/internal/api/a.go:10.2,12.3 2 0",
		"github.com/o/w/internal/services/s.go:4.1,6.2 1 9",
		"",
	}, "\n")

	got := merge(t, ran, didNotRun)

	want := strings.Join([]string{
		"mode: atomic",
		"github.com/o/w/internal/api/a.go:10.2,12.3 2 5",
		"github.com/o/w/internal/services/s.go:4.1,6.2 1 9",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", got, want)
	}
}

// A block no shard executed must survive the merge at 0. Dropping it would
// silently shrink the denominator: patchcov treats a line absent from the
// profile as NON-COVERABLE and ignores it, so a dropped zero block turns an
// uncovered modified line into a line the required gate never judges.
func TestABlockNoShardExecutedSurvivesAtZero(t *testing.T) {
	profile := "mode: atomic\ngithub.com/o/w/internal/db/d.go:1.1,2.2 1 0\n"

	got := merge(t, profile, profile)

	if got != profile {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", got, profile)
	}
}

// `set` mode records a boolean, so summing would produce a 2 that the profile's
// own mode line says cannot exist. The lane pins `atomic`; this keeps the
// command honest for anyone who points it at a `set` profile instead of
// producing a value its consumers would misread.
func TestSetModeCombinesWithOrRatherThanSum(t *testing.T) {
	profile := "mode: set\ngithub.com/o/w/internal/api/a.go:10.2,12.3 2 1\n"

	got := merge(t, profile, profile)

	if got != profile {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", got, profile)
	}
}

// Two profiles that disagree about how many statements a span holds were built
// from different source trees. Merging them would hand the consumers a profile
// matching neither, and the patch gate reads it against the diff of one of them.
func TestABlockWithADifferentStatementCountIsRefused(t *testing.T) {
	first := "mode: atomic\ngithub.com/o/w/internal/api/a.go:10.2,12.3 2 1\n"
	second := "mode: atomic\ngithub.com/o/w/internal/api/a.go:10.2,12.3 3 1\n"

	got := mergeError(t, first, second)

	if !strings.Contains(got, "different trees") {
		t.Fatalf("error %q does not name the cause", got)
	}
}

func TestAModeMismatchIsRefused(t *testing.T) {
	paths := []string{
		writeProfile(t, "atomic", "mode: atomic\ngithub.com/o/w/a.go:1.1,2.2 1 1\n"),
		writeProfile(t, "set", "mode: set\ngithub.com/o/w/a.go:1.1,2.2 1 1\n"),
	}

	_, _, err := mergeProfiles(paths)

	if err == nil || !strings.Contains(err.Error(), "mode mismatch") {
		t.Fatalf("merging atomic with set gave %v, want a mode mismatch", err)
	}
}

// A line the command cannot fully account for is an error rather than
// something to skip past: a silently dropped block understates coverage in a
// required gate, which reads as the pull request's own fault.
func TestAnUnaccountableLineIsRefused(t *testing.T) {
	for name, line := range map[string]string{
		"missing count":      "github.com/o/w/a.go:1.1,2.2 1",
		"extra field":        "github.com/o/w/a.go:1.1,2.2 1 1 1",
		"no span":            "github.com/o/w/a.go 1 1",
		"no column":          "github.com/o/w/a.go:1,2 1 1",
		"negative count":     "github.com/o/w/a.go:1.1,2.2 1 -1",
		"non-numeric count":  "github.com/o/w/a.go:1.1,2.2 1 many",
		"zero line number":   "github.com/o/w/a.go:0.1,2.2 1 1",
		"a go test trailer":  "ok  \tgithub.com/o/w\t5.5s",
		"a coverage summary": "coverage: 65.8% of statements",
	} {
		t.Run(name, func(t *testing.T) {
			blocks := map[blockKey]blockValue{}
			if _, err := Accumulate(blocks, "mode: atomic\n"+line+"\n", "shard/coverage.out"); err == nil {
				t.Fatalf("line %q was accepted", line)
			}
		})
	}
}

func TestAProfileWithoutAModeLineIsRefused(t *testing.T) {
	blocks := map[blockKey]blockValue{}

	if _, err := Accumulate(blocks, "github.com/o/w/a.go:1.1,2.2 1 1\n", "shard/coverage.out"); err == nil {
		t.Fatal("a profile whose first line is a block was accepted")
	}
	if _, err := Accumulate(blocks, "\n", "shard/coverage.out"); err == nil {
		t.Fatal("an empty profile was accepted")
	}
}

// Determinism is for the humans: a merged profile that reorders itself between
// runs cannot be diffed when a coverage number moves unexpectedly.
func TestRenderSortsByFileThenPosition(t *testing.T) {
	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/o/w/b.go:1.1,2.2 1 1",
		"github.com/o/w/a.go:10.1,11.2 1 1",
		"github.com/o/w/a.go:2.5,3.2 1 1",
		"github.com/o/w/a.go:2.1,3.2 1 1",
		"",
	}, "\n")

	got := merge(t, profile)

	want := strings.Join([]string{
		"mode: atomic",
		"github.com/o/w/a.go:2.1,3.2 1 1",
		"github.com/o/w/a.go:2.5,3.2 1 1",
		"github.com/o/w/a.go:10.1,11.2 1 1",
		"github.com/o/w/b.go:1.1,2.2 1 1",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("rendered profile:\n%s\nwant:\n%s", got, want)
	}
}

// actions/download-artifact lands each artifact in its own subdirectory, and
// every shard's file is called coverage.out — so the search is a walk matched
// on the BASENAME, never a flat listing of the download directory.
func TestProfilesAreFoundInPerArtifactSubdirectories(t *testing.T) {
	root := t.TempDir()
	for _, shard := range []string{"coverage-shard-1", "coverage-shard-2"} {
		dir := filepath.Join(root, shard)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "coverage.out"), []byte("mode: atomic\n"), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	matches, err := findProfiles(root, "coverage.out")
	if err != nil {
		t.Fatalf("find profiles: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("found %v, want the two shard profiles", matches)
	}
}

// The failure this guard exists for leaves no other trace: an artifact that
// never arrived merges into a profile that parses cleanly and covers less.
func TestACountOtherThanTheExpectedOneIsRefused(t *testing.T) {
	two := []string{"a/coverage.out", "b/coverage.out"}

	if err := RequireCount(two, 3); err == nil {
		t.Fatal("two profiles passed a check expecting three")
	}
	if err := RequireCount(two, 2); err != nil {
		t.Fatalf("two profiles failed a check expecting two: %v", err)
	}
	if err := RequireCount(two, 0); err != nil {
		t.Fatalf("an expectation of 0 must disable the check, got %v", err)
	}
}

func writeProfile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name+".out")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
