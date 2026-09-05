package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testenv"
)

const fixtureChangelog = `# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

` + pointerLine + `

### Fixed

- **A frozen entry.** Written before fragments existed, and still waiting for a
  release to carry it out.

## [1.0.0] - 2026-01-01

### Added

- **The first release.**
`

// --- check mode -------------------------------------------------------------

func TestCheckFailsWhenBranchAddsNoFragment(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "internal/app.go", "package app\n")
	commitAll(t, dir, "feat: something user-visible")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure == "" {
		t.Fatal("expected the gate to fail on a branch with no changelog.d/ fragment")
	}
	for _, want := range []string{"adds no fragment", "changelog.d/<branch-name>.md", "### Fixed", "none"} {
		if !strings.Contains(failure, want) {
			t.Errorf("failure message does not mention %q:\n%s", want, failure)
		}
	}
}

func TestCheckPassesOnAddedFragment(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/some-branch.md", "### Added\n\n- **A new thing.**\n")
	commitAll(t, dir, "feat: a new thing")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected the gate to pass on a valid fragment, got:\n%s", failure)
	}
}

func TestCheckPassesOnNoneMarkerFragment(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/chore-branch.md", "none\n\nOnly the CI workflow moved; nothing an operator can observe.\n")
	commitAll(t, dir, "chore: nothing user-visible")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected the gate to pass on a none-marker fragment, got:\n%s", failure)
	}
}

func TestCheckFailsOnUnknownSectionHeaderAndNamesTheFile(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/bad-branch.md", "### Improved\n\n- **Not a Keep a Changelog section.**\n")
	commitAll(t, dir, "feat: wrong header")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(failure, "changelog.d/bad-branch.md") || !strings.Contains(failure, "### Improved") {
		t.Fatalf("failure must name the file and the offending header, got:\n%s", failure)
	}
}

func TestCheckFailsOnFragmentWithoutEntryText(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/empty-branch.md", "### Added\n\n")
	commitAll(t, dir, "feat: header only")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(failure, "no changelog entry found") {
		t.Fatalf("expected a header-only fragment to fail, got:\n%s", failure)
	}
}

func TestCheckFailsOnFragmentTextBeforeTheFirstHeader(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/loose-branch.md", "- **An entry with no section.**\n")
	commitAll(t, dir, "feat: no header")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(failure, "text before the first section header") {
		t.Fatalf("expected an unheaded fragment to fail, got:\n%s", failure)
	}
}

func TestCheckPassesWhenChangelogGainsAReleaseHeading(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "CHANGELOG.md", strings.Replace(fixtureChangelog,
		"## [1.0.0] - 2026-01-01",
		"## [1.1.0] - 2026-02-02\n\n### Added\n\n- **An assembled release.**\n\n## [1.0.0] - 2026-01-01", 1))
	commitAll(t, dir, "chore: cut 1.1.0")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected release assembly to pass without a fragment, got:\n%s", failure)
	}
}

func TestCheckFailsWhenChangelogGainsAnEntryButNoReleaseHeading(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "CHANGELOG.md", strings.Replace(fixtureChangelog,
		"### Fixed\n",
		"### Fixed\n\n- **An entry written straight into Unreleased.**\n", 1))
	commitAll(t, dir, "fix: edit the changelog by hand")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure == "" {
		t.Fatal("editing the [Unreleased] body by hand must not satisfy the gate")
	}
}

func TestCheckIgnoresAnEditToAnExistingFragment(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/landed-earlier.md", "### Added\n\n- **Landed on main already.**\n")
	commitAll(t, dir, "chore: a fragment already on main")
	run(t, dir, "checkout", "-q", "main")
	run(t, dir, "merge", "-q", "--ff-only", "feature")
	run(t, dir, "checkout", "-q", "-b", "later")
	writeFile(t, dir, "changelog.d/landed-earlier.md", "### Added\n\n- **Reworded by another branch.**\n")
	commitAll(t, dir, "docs: reword someone else's fragment")

	failure, err := check(dir, "main", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure == "" {
		t.Fatal("modifying a fragment that is already on main is not this branch's entry")
	}
}

func TestCheckReportsAnUnusableBaseRef(t *testing.T) {
	dir := initRepo(t)
	if _, err := check(dir, "origin/does-not-exist", gitOutput); err == nil {
		t.Fatal("expected an error, not a verdict, when the base ref cannot be resolved")
	}
}

func TestAddedFragmentsCollectsOnlyAddedMarkdownUnderChangelogD(t *testing.T) {
	nameStatus := strings.Join([]string{
		"A\tchangelog.d/second.md",
		"A\tchangelog.d/first.md",
		"M\tchangelog.d/existing.md",
		"D\tchangelog.d/gone.md",
		"A\tchangelog.d/.gitkeep",
		"A\tdocs/changelog.d/elsewhere.md",
		"A\tinternal/api/handler.go",
		"R100\tchangelog.d/old.md\tchangelog.d/renamed.md",
		"",
	}, "\n")

	got := addedFragments(nameStatus)
	want := []string{"changelog.d/first.md", "changelog.d/second.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("addedFragments = %v, want %v", got, want)
	}
}

func TestAddsReleaseHeadingReadsAddedLinesOnly(t *testing.T) {
	cases := map[string]bool{
		"+++ b/CHANGELOG.md\n+## [1.2.3] - 2026-02-02\n":  true,
		"+++ b/CHANGELOG.md\n+- **An ordinary entry.**\n": false,
		"+++ b/CHANGELOG.md\n-## [1.2.3] - 2026-02-02\n":  false,
		"": false,
	}
	for diff, want := range cases {
		if got := addsReleaseHeading(diff); got != want {
			t.Errorf("addsReleaseHeading(%q) = %v, want %v", diff, got, want)
		}
	}
}

// --- assemble mode ----------------------------------------------------------

func TestAssembleFoldsFragmentsIntoAReleaseSection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)
	writeFile(t, dir, "changelog.d/.gitkeep", "")
	writeFile(t, dir, "changelog.d/a-first.md", "### Added\n\n- **A new thing.** It spans\n  two lines.\n")
	writeFile(t, dir, "changelog.d/b-second.md", "### Fixed\n\n- **A fixed thing.**\n\n### Security\n\n- **A hardened thing.**\n")
	writeFile(t, dir, "changelog.d/c-quiet.md", "none\n")

	summary, err := assembleCommand(dir, []string{"-version", "1.1.0", "-date", "2026-02-02"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !strings.Contains(summary, "1.1.0") || !strings.Contains(summary, "3 fragment(s)") {
		t.Errorf("summary = %q, want the version and the consumed count", summary)
	}

	want := `# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

` + pointerLine + `

## [1.1.0] - 2026-02-02

### Added

- **A new thing.** It spans
  two lines.

### Fixed

- **A frozen entry.** Written before fragments existed, and still waiting for a
  release to carry it out.

- **A fixed thing.**

### Security

- **A hardened thing.**

## [1.0.0] - 2026-01-01

### Added

- **The first release.**
`
	got := readFile(t, dir, "CHANGELOG.md")
	if got != want {
		t.Fatalf("assembled CHANGELOG.md mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	for _, consumed := range []string{"changelog.d/a-first.md", "changelog.d/b-second.md", "changelog.d/c-quiet.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(consumed))); !os.IsNotExist(err) {
			t.Errorf("%s must be deleted after assembly (stat err = %v)", consumed, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "changelog.d", ".gitkeep")); err != nil {
		t.Errorf("changelog.d/.gitkeep must survive assembly: %v", err)
	}
}

func TestAssembleWithoutFragmentsStillReleasesTheFrozenBacklog(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)

	if _, err := assemble(dir, "1.1.0", "2026-02-02"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	got := readFile(t, dir, "CHANGELOG.md")
	if !strings.Contains(got, "## [1.1.0] - 2026-02-02\n\n### Fixed\n\n- **A frozen entry.**") {
		t.Fatalf("frozen backlog was not carried into the release:\n%s", got)
	}
	if strings.Count(got, "- **A frozen entry.**") != 1 {
		t.Fatalf("the frozen entry must move, not duplicate:\n%s", got)
	}
	if !strings.Contains(got, "## [Unreleased]\n\n"+pointerLine+"\n\n## [1.1.0]") {
		t.Fatalf("the Unreleased body must shrink to the pointer line:\n%s", got)
	}
}

func TestAssembleDefaultsTheDateToTodayInUTC(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)

	summary, err := assembleCommand(dir, []string{"-version", "2.0.0"})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !strings.Contains(readFile(t, dir, "CHANGELOG.md"), "## [2.0.0] - ") {
		t.Fatalf("expected a dated release heading, summary was %q", summary)
	}
}

func TestAssembleRefusesBadInput(t *testing.T) {
	cases := []struct {
		name    string
		version string
		date    string
		want    string
	}{
		{name: "no version", version: "", date: "2026-02-02", want: "invalid -version"},
		{name: "tag-shaped version", version: "v1.1.0", date: "2026-02-02", want: "invalid -version"},
		{name: "bad date", version: "1.1.0", date: "02/02/2026", want: "invalid -date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)
			_, err := assemble(dir, c.version, c.date)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("assemble error = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestAssembleRefusesAnEmptyRelease(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n"+pointerLine+"\n\n## [1.0.0] - 2026-01-01\n\n### Added\n\n- **The first release.**\n")

	_, err := assemble(dir, "1.1.0", "2026-02-02")
	if err == nil || !strings.Contains(err.Error(), "nothing to release") {
		t.Fatalf("assemble error = %v, want one mentioning \"nothing to release\"", err)
	}
}

func TestAssembleRefusesAnUnknownSectionInAFragment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)
	writeFile(t, dir, "changelog.d/bad.md", "### Improved\n\n- **Not a Keep a Changelog section.**\n")

	_, err := assemble(dir, "1.1.0", "2026-02-02")
	if err == nil || !strings.Contains(err.Error(), "bad.md") || !strings.Contains(err.Error(), "### Improved") {
		t.Fatalf("assemble error = %v, want one naming the fragment and the header", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "changelog.d", "bad.md")); statErr != nil {
		t.Errorf("a rejected fragment must not be deleted: %v", statErr)
	}
}

func TestAssembleRefusesAChangelogWithoutAnUnreleasedSection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [1.0.0] - 2026-01-01\n\n### Added\n\n- **The first release.**\n")
	writeFile(t, dir, "changelog.d/x.md", "### Added\n\n- **A new thing.**\n")

	_, err := assemble(dir, "1.1.0", "2026-02-02")
	if err == nil || !strings.Contains(err.Error(), "no \"## [Unreleased]\" section") {
		t.Fatalf("assemble error = %v, want one about the missing Unreleased section", err)
	}
}

func TestAssembleReportsAnUnreadableChangelog(t *testing.T) {
	dir := t.TempDir()
	if _, err := assemble(dir, "1.1.0", "2026-02-02"); err == nil || !strings.Contains(err.Error(), "read CHANGELOG.md") {
		t.Fatalf("assemble error = %v, want one about reading CHANGELOG.md", err)
	}
}

func TestAssembleCommandRejectsUnknownFlags(t *testing.T) {
	if _, err := assembleCommand(t.TempDir(), []string{"-nope"}); err == nil {
		t.Fatal("expected an error on an unknown flag")
	}
}

func TestAssembleReleasesTheLastSectionOfTheFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n"+pointerLine+"\n")
	writeFile(t, dir, "changelog.d/only.md", "### Added\n\n- **The very first entry.**\n")

	if _, err := assemble(dir, "0.1.0", "2026-02-02"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	want := "# Changelog\n\n## [Unreleased]\n\n" + pointerLine + "\n\n## [0.1.0] - 2026-02-02\n\n### Added\n\n- **The very first entry.**\n"
	if got := readFile(t, dir, "CHANGELOG.md"); got != want {
		t.Fatalf("assembled CHANGELOG.md mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestAssembleOrdersSectionsIncludingTheTwoBeyondKeepAChangelog pins the order
// released sections have always been written in here: the Keep a Changelog six,
// then "### Internal", then "### Dependencies".
func TestAssembleOrdersSectionsIncludingTheTwoBeyondKeepAChangelog(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n"+pointerLine+"\n")
	writeFile(t, dir, "changelog.d/one.md", "### Dependencies\n\n- **Bumped a pin.**\n\n### Internal\n\n- **Moved a helper.**\n\n### Changed\n\n- **Changed a behaviour.**\n")

	if _, err := assemble(dir, "1.1.0", "2026-02-02"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	want := "# Changelog\n\n## [Unreleased]\n\n" + pointerLine + "\n\n## [1.1.0] - 2026-02-02\n\n" +
		"### Changed\n\n- **Changed a behaviour.**\n\n" +
		"### Internal\n\n- **Moved a helper.**\n\n" +
		"### Dependencies\n\n- **Bumped a pin.**\n"
	if got := readFile(t, dir, "CHANGELOG.md"); got != want {
		t.Fatalf("assembled CHANGELOG.md mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestAssembleCarriesTheRepositoryBacklogVerbatim runs assembly over a copy of
// this repository's own CHANGELOG.md and fragments. The fixture above is small
// and tidy; the real file carries multi-paragraph entries, nested bullets and
// indented continuation lines, and those must survive the move into a released
// section byte for byte.
func TestAssembleCarriesTheRepositoryBacklogVerbatim(t *testing.T) {
	root := repoRoot(t)
	original, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read the repository CHANGELOG.md: %v", err)
	}
	head, unreleased, tail, err := splitChangelog(string(original))
	if err != nil {
		t.Fatalf("split the repository CHANGELOG.md: %v", err)
	}
	frozen, err := parseSections("CHANGELOG.md [Unreleased]", strings.Join(stripPointerLine(unreleased), "\n"))
	if err != nil {
		t.Fatalf("parse the repository backlog: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", string(original))

	// The fragment directory has to EXIST before its listing means anything:
	// filepath.Glob reports no error for a directory that is not there, so a
	// renamed, missing or unreadable changelog.d yields the same empty slice a
	// freshly assembled repository does. Separating the two here is what lets
	// the skip below trust assemble's verdict — a fixture that could not look
	// must fail, never skip.
	// Two outcomes, and the message says which: a path that cannot be looked at
	// is not a path that was looked at and turned out to be a file, and one
	// report carrying both appends a `<nil>` error to the second.
	fragmentsDir := filepath.Join(root, "changelog.d")
	switch info, statErr := os.Stat(fragmentsDir); {
	case statErr != nil:
		t.Fatalf("cannot stat the repository fragment directory %s: %v — an empty listing here would be a broken checkout, not an empty backlog", fragmentsDir, statErr)
	case !info.IsDir():
		t.Fatalf("the repository fragment path %s exists but is not a directory — an empty listing here would be a broken checkout, not an empty backlog", fragmentsDir)
	}
	fragments, err := filepath.Glob(filepath.Join(fragmentsDir, "*.md"))
	if err != nil {
		t.Fatalf("list the repository fragments: %v", err)
	}
	for _, fragment := range fragments {
		data, readErr := os.ReadFile(fragment)
		if readErr != nil {
			t.Fatalf("read %s: %v", fragment, readErr)
		}
		writeFile(t, dir, "changelog.d/"+filepath.Base(fragment), string(data))
	}

	if _, err := assemble(dir, "99.0.0", "2026-12-31"); err != nil {
		// Immediately after a real release assembly the repository legitimately
		// carries no backlog — every fragment was just consumed and the
		// "[Unreleased]" body reduced to the pointer line, which is exactly the
		// state TestAssembleRefusesAnEmptyRelease asserts assemble must refuse.
		// This test has nothing to verify verbatim survival of until the next
		// pull request lands a fragment, so it has nothing to fail on either.
		//
		// assemble is the authority on that state, and it is asked by value:
		// counting the files collected above would disagree with it on a
		// backlog made only of `none` markers, which are fragments that
		// contribute no entry. The directory check above is what separates a
		// repository that is empty from a fixture that could not look.
		if errors.Is(err, errNothingToRelease) {
			t.Skip("repository backlog is empty right after a release assembly; nothing to verify verbatim")
		}
		t.Fatalf("assemble: %v", err)
	}

	got := readFile(t, dir, "CHANGELOG.md")
	if !strings.HasPrefix(got, strings.Join(head, "\n")+"\n\n"+pointerLine+"\n\n## [99.0.0] - 2026-12-31\n") {
		t.Fatalf("assembled head is wrong:\n%s", firstLines(got, 12))
	}
	wantTail, warning := updateReleaseLinks(tail, "99.0.0")
	if warning != "" {
		t.Fatalf("the repository's own link block should match the expected format, got warning %q", warning)
	}
	if !strings.HasSuffix(got, strings.Join(wantTail, "\n")) {
		t.Fatal("everything below the released sections, other than the link block's own update, must be untouched")
	}
	for section, body := range frozen {
		if !strings.Contains(got, body) {
			t.Errorf("the frozen %s body did not survive assembly verbatim", section)
		}
	}
}

func TestUpdateReleaseLinksInsertsTheNewVersionAndMovesUnreleased(t *testing.T) {
	tail := []string{
		"## [1.0.0] - 2026-01-01",
		"",
		"### Added",
		"",
		"- **The first release.**",
		"",
		"[Unreleased]: https://example.com/o/r/compare/v1.0.0...HEAD",
		"[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0",
	}
	got, warning := updateReleaseLinks(tail, "1.1.0")
	want := []string{
		"## [1.0.0] - 2026-01-01",
		"",
		"### Added",
		"",
		"- **The first release.**",
		"",
		"[Unreleased]: https://example.com/o/r/compare/v1.1.0...HEAD",
		"[1.1.0]: https://example.com/o/r/compare/v1.0.0...v1.1.0",
		"[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("updateReleaseLinks =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if warning != "" {
		t.Fatalf("expected no warning on a well-formed link block, got %q", warning)
	}
}

func TestUpdateReleaseLinksIsANoOpWhenTheVersionAlreadyHasALink(t *testing.T) {
	tail := []string{
		"[Unreleased]: https://example.com/o/r/compare/v1.1.0...HEAD",
		"[1.1.0]: https://example.com/o/r/compare/v1.0.0...v1.1.0",
		"[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0",
	}
	got, warning := updateReleaseLinks(tail, "1.1.0")
	if strings.Join(got, "\n") != strings.Join(tail, "\n") {
		t.Fatalf("a rerun for a version that already has a link must not change the block:\n%s", strings.Join(got, "\n"))
	}
	if warning != "" {
		t.Fatalf("expected no warning when the version already has a link, got %q", warning)
	}
}

func TestUpdateReleaseLinksIsANoOpWithoutALinkBlock(t *testing.T) {
	tail := []string{"## [1.0.0] - 2026-01-01", "", "### Added", "", "- **The first release.**"}
	got, warning := updateReleaseLinks(tail, "1.1.0")
	if strings.Join(got, "\n") != strings.Join(tail, "\n") {
		t.Fatalf("a changelog with no link block must be left untouched:\n%s", strings.Join(got, "\n"))
	}
	if warning != "" {
		t.Fatalf("expected no warning when there is no link block at all, got %q", warning)
	}
}

func TestUpdateReleaseLinksWarnsInsteadOfFailingSilentlyOnADriftedFormat(t *testing.T) {
	tail := []string{
		"[Unreleased]: https://example.com/o/r/compare/v1.0...HEAD",
		"[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0",
	}
	got, warning := updateReleaseLinks(tail, "1.1.0")
	if strings.Join(got, "\n") != strings.Join(tail, "\n") {
		t.Fatalf("a block the pattern cannot parse must be left untouched:\n%s", strings.Join(got, "\n"))
	}
	if warning == "" {
		t.Fatal("expected a warning when the [Unreleased] line does not match the expected format")
	}
}

func TestAssembleUpdatesTheReleaseLinkBlock(t *testing.T) {
	dir := t.TempDir()
	content := fixtureChangelog + "\n[Unreleased]: https://example.com/o/r/compare/v1.0.0...HEAD\n[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0\n"
	writeFile(t, dir, "CHANGELOG.md", content)
	writeFile(t, dir, "changelog.d/a.md", "### Added\n\n- **A new thing.**\n")

	if _, err := assemble(dir, "1.1.0", "2026-02-02"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	got := readFile(t, dir, "CHANGELOG.md")
	if !strings.Contains(got, "[Unreleased]: https://example.com/o/r/compare/v1.1.0...HEAD\n") {
		t.Fatalf("the Unreleased link was not moved to compare against the new release:\n%s", got)
	}
	if !strings.Contains(got, "[1.1.0]: https://example.com/o/r/compare/v1.0.0...v1.1.0\n") {
		t.Fatalf("the new release did not get its own compare link:\n%s", got)
	}
	if !strings.Contains(got, "[1.0.0]: https://example.com/o/r/releases/tag/v1.0.0\n") {
		t.Fatalf("the previous release's own link must survive untouched:\n%s", got)
	}
}

func TestParseSectionsConcatenatesRepeatedHeaders(t *testing.T) {
	sections, err := parseSections("x.md", "### Added\n\n- **One.**\n\n### Added\n\n- **Two.**\n")
	if err != nil {
		t.Fatalf("parseSections: %v", err)
	}
	if got := sections["### Added"]; got != "- **One.**\n\n- **Two.**" {
		t.Fatalf("sections[\"### Added\"] = %q", got)
	}
}

func TestValidateFragmentAcceptsTheNoneMarkerAndRejectsAnEmptyFile(t *testing.T) {
	if err := validateFragment("quiet.md", "  none  \n"); err != nil {
		t.Errorf("a padded none marker must be accepted: %v", err)
	}
	if err := validateFragment("empty.md", "\n\n"); err == nil {
		t.Error("an empty fragment must be rejected")
	}
	if err := validateFragment("notquite.md", "nonelike\n"); err == nil {
		t.Error("only an exact none marker may skip the section rules")
	}
}

// --- helpers ----------------------------------------------------------------

// initRepo builds a real one-commit git repository with the fixture changelog
// on main and leaves a branch checked out, so the check-mode tests exercise the
// same `git diff` invocations CI runs rather than a canned string.
func initRepo(t *testing.T) string {
	t.Helper()
	testenv.RequireLookPath(t, "git", "git")
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "changelogd@example.test")
	run(t, dir, "config", "user.name", "changelogd test")
	run(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "CHANGELOG.md", fixtureChangelog)
	commitAll(t, dir, "chore: base")
	run(t, dir, "checkout", "-q", "-b", "feature")
	return dir
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "--no-verify", "-m", message)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitOutput(dir, args...); err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// repoRoot walks up from the test's working directory to the module root.
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
			t.Fatal("module root (go.mod) not found above the test directory")
		}
		dir = parent
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
