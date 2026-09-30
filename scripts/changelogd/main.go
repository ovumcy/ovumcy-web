// Command changelogd keeps changelog entries out of CHANGELOG.md until a
// release is cut. Every pull request adds one fragment file under changelog.d/
// instead of editing the "## [Unreleased]" section, which is the only spot in
// the tree several branches reliably contend for: two pull requests inserting
// an entry at the same anchor conflict in the merge queue, and rebasing replays
// the earlier commit straight back into the contested spot.
//
// Two subcommands:
//
//	changelogd check
//	    CI gate for pull requests. Env BASE_REF (default origin/main) names the
//	    base to diff against. Passes when the branch adds at least one fragment
//	    under changelog.d/ — one of them named changelog.d/<slug>.md, the slug
//	    being the branch (GITHUB_HEAD_REF in CI, else the current branch)
//	    without its type/ prefix — or when it edits CHANGELOG.md itself in a way only
//	    release assembly does (adding the "## [x.y.z]" heading of a new release,
//	    not re-typing an existing one); every added or modified
//	    fragment must be valid. Independently of either, a CHANGELOG.md edit
//	    that is neither assembly nor confined to an already-released section
//	    fails. Exit 0 when the gate passes, 1 when it fails.
//
//	changelogd assemble -version X.Y.Z [-date YYYY-MM-DD]
//	    Run by hand at release time. Folds every accumulated fragment, plus
//	    whatever is still frozen under "## [Unreleased]", into a new released
//	    section and deletes the consumed fragments.
//
// Exit codes follow scripts/patchcov: 0 pass, 1 the gate itself failing, 2 for
// tool breakage (git unavailable, unreadable files, bad flags).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// pointerLine replaces the entries that used to accumulate under
// "## [Unreleased]". Assembly rewrites the Unreleased body down to this single
// line, and strips it from the frozen backlog before merging.
const pointerLine = "*New entries accumulate as per-pull-request fragments in [changelog.d/](changelog.d/) and are assembled here at release.*"

// fragmentDir holds one markdown fragment per pull request.
const fragmentDir = "changelog.d"

// changelogFile is written by release assembly and by corrections to text that
// has already been released — never by an ordinary pull request.
const changelogFile = "CHANGELOG.md"

// noneMarker is the whole content a pull request with no user-visible change
// puts in its fragment (the rest of such a file is ignored).
const noneMarker = "none"

// sectionOrder is the order every assembled release follows: the Keep a
// Changelog sections, then the two this changelog has always carried after them
// ("### Internal" since 1.4.0, "### Dependencies" since 1.8.0). The list is
// closed on purpose — a typo'd or invented header is what the check rejects.
var sectionOrder = []string{
	"### Added",
	"### Changed",
	"### Deprecated",
	"### Removed",
	"### Fixed",
	"### Security",
	"### Internal",
	"### Dependencies",
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// unreleasedLinkPattern matches the Keep a Changelog reference-style link line
// for "[Unreleased]", e.g.
// "[Unreleased]: https://github.com/ovumcy/ovumcy-web/compare/v1.9.2...HEAD".
// Group 1 is everything through "compare/", group 2 the previous release tag.
var unreleasedLinkPattern = regexp.MustCompile(`^\[Unreleased\]: (.+/compare/)(v\d+\.\d+\.\d+)\.\.\.HEAD$`)

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: changelogd <check|assemble> [flags]")
	}
	switch os.Args[1] {
	case "check":
		headRef, err := resolveHeadRef(".", os.Getenv("GITHUB_HEAD_REF"), gitOutput)
		if err != nil {
			fatalf("changelog fragment check: %v", err)
		}
		failure, err := check(".", envOr("BASE_REF", "origin/main"), headRef, gitOutput)
		if err != nil {
			fatalf("changelog fragment check: %v", err)
		}
		if failure != "" {
			fmt.Fprintln(os.Stderr, failure)
			os.Exit(1)
		}
		fmt.Println("changelog fragment check OK.")
	case "assemble":
		summary, err := assembleCommand(".", os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "changelog assembly FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(summary)
	default:
		fatalf("unknown subcommand %q (expected check or assemble)", os.Args[1])
	}
}

// gitRunner runs git in dir and returns its standard output.
type gitRunner func(dir string, args ...string) (string, error)

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// resolveHeadRef names the branch a fragment is owed for: the pull request's
// head branch in CI (GITHUB_HEAD_REF — the checkout there is a detached merge
// commit), otherwise the current branch. A detached HEAD outside CI names no
// branch and yields "", which skips the naming rule.
func resolveHeadRef(root, ciHeadRef string, git gitRunner) (string, error) {
	if ref := strings.TrimSpace(ciHeadRef); ref != "" {
		return ref, nil
	}
	out, err := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve the current branch: %w", err)
	}
	if ref := strings.TrimSpace(out); ref != "HEAD" {
		return ref, nil
	}
	return "", nil
}

// fragmentPathForBranch is the fragment a branch owes: changelog.d/<slug>.md,
// the slug being the branch name without its leading type/ segment (fix/,
// feat/, ci/, …), with any further "/" turned into "-" so the fragment stays
// directly under changelog.d/.
func fragmentPathForBranch(headRef string) string {
	slug := headRef
	if _, rest, found := strings.Cut(headRef, "/"); found {
		slug = rest
	}
	return fragmentDir + "/" + strings.ReplaceAll(slug, "/", "-") + ".md"
}

// check reports whether the branch satisfies the fragment rule. It returns an
// empty string when the gate passes, the failure text when it fails, and an
// error only when the check itself could not be carried out. headRef is the
// branch the pull request comes from; when it is known, one of the fragments
// the branch adds must carry its slug (fragmentPathForBranch), and "" skips
// that rule.
func check(root, baseRef, headRef string, git gitRunner) (string, error) {
	nameStatus, err := git(root, "diff", "--name-status", "--no-color", baseRef+"...HEAD")
	if err != nil {
		return "", fmt.Errorf("diff against %s: %w", baseRef, err)
	}

	// Every fragment the branch touches is judged, added or edited: an edited
	// fragment is not this branch's entry (see addedFragments) but its text
	// still reaches CHANGELOG.md at release assembly.
	var fragmentProblems []string
	for _, fragment := range changedFragments(nameStatus) {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(fragment)))
		if readErr != nil {
			return "", fmt.Errorf("read fragment %s: %w", fragment, readErr)
		}
		if err := validateFragment(fragment, string(data)); err != nil {
			fragmentProblems = append(fragmentProblems, "  "+err.Error())
		}
	}

	// CHANGELOG.md is judged whether or not a fragment was added: an added
	// fragment must not license a rewrite of the [Unreleased] body beside it.
	changelogDiff, err := git(root, "diff", "--unified=0", "--no-color", baseRef+"...HEAD", "--", changelogFile)
	if err != nil {
		return "", fmt.Errorf("diff %s against %s: %w", changelogFile, baseRef, err)
	}
	var offending []string
	assembly := false
	if strings.TrimSpace(changelogDiff) != "" {
		current, showErr := git(root, "show", "HEAD:"+changelogFile)
		if showErr != nil {
			return "", fmt.Errorf("read %s at HEAD: %w", changelogFile, showErr)
		}
		assembly = addsReleaseHeading(changelogDiff, current)
		if !assembly {
			// The diff's old-side positions belong to the merge base, so the released
			// boundary is read there: read at HEAD, a heading this very diff adds above
			// the [Unreleased] body would move the boundary up and license its rewrite.
			mergeBase, mbErr := git(root, "merge-base", baseRef, "HEAD")
			if mbErr != nil {
				return "", fmt.Errorf("merge base of %s and HEAD: %w", baseRef, mbErr)
			}
			base, baseErr := git(root, "show", strings.TrimSpace(mergeBase)+":"+changelogFile)
			if baseErr != nil {
				return "", fmt.Errorf("read %s at the merge base: %w", changelogFile, baseErr)
			}
			offending = editsOutsideReleasedText(changelogDiff, base)
		}
	}

	if len(fragmentProblems) > 0 || len(offending) > 0 {
		var report []string
		if len(fragmentProblems) > 0 {
			report = append(report, strings.Join(fragmentProblems, "\n")+"\n\n"+fragmentFormatHelp())
		}
		if len(offending) > 0 {
			report = append(report, changelogEditHelp(offending, headRef))
		}
		return "changelog fragment check FAILED:\n" + strings.Join(report, "\n"), nil
	}

	if assembly {
		return "", nil
	}
	added := addedFragments(nameStatus)
	if len(added) == 0 {
		return missingFragmentHelp(headRef), nil
	}
	if headRef == "" {
		return "", nil
	}
	want := fragmentPathForBranch(headRef)
	for _, fragment := range added {
		if fragment == want {
			return "", nil
		}
	}
	return misnamedFragmentHelp(headRef, want, added), nil
}

// changedFragments returns every changelog.d/*.md path whose content this
// branch introduces or rewrites: added (A), modified (M) and the destination of
// a rename (R). A deletion carries no content to judge. Unlike addedFragments it
// says nothing about whether the branch owes a fragment of its own.
func changedFragments(nameStatus string) []string {
	var changed []string
	for _, line := range strings.Split(nameStatus, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		switch fields[0][0] {
		case 'A', 'M', 'R':
		default:
			continue
		}
		path := fields[len(fields)-1]
		if strings.HasPrefix(path, fragmentDir+"/") && strings.HasSuffix(path, ".md") {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// hunkHeader matches a unified-diff hunk header; group 1 is the first line of
// the hunk in the old file (for a pure insertion, the line it follows), group 2
// its length there (absent means 1) — unread, since a deletion and an insertion
// are judged by the same first line.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,\d+)? @@`)

// editsOutsideReleasedText returns the hunk headers of a `--unified=0`
// CHANGELOG.md diff that touch anything but an already-released version
// section: the title, the "[Unreleased]" body, or a hunk that reaches up to the
// first release heading. base is the file the diff starts from, whose line
// numbers the hunks' old side carries; a boundary read from the changed file
// would move with a heading the diff itself adds. The first released heading is
// the first line matching releaseHeadingPattern, so an "## [Unreleased]" with a
// suffix is never mistaken for it. An edit from that heading line onwards — the
// heading belongs to its section, so a date correction is one — is a correction
// to released text and is not reported; a file with no released heading has no
// such text, so every hunk is.
func editsOutsideReleasedText(diff, base string) []string {
	firstReleased := 0
	for i, line := range strings.Split(base, "\n") {
		if releaseHeadingPattern.MatchString(strings.TrimRight(line, "\r")) {
			firstReleased = i + 1
			break
		}
	}
	var offending []string
	for _, line := range strings.Split(diff, "\n") {
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		// A hunk that only inserts lines reports the line it follows, and one that
		// removes or rewrites lines its first removed line; either way the hunk is
		// inside a released section when that line is the heading line or below it.
		inside := firstReleased > 0 && start >= firstReleased
		if !inside {
			offending = append(offending, strings.TrimSpace(line))
		}
	}
	return offending
}

func changelogEditHelp(offending []string, headRef string) string {
	return "\n" + changelogFile + " is edited outside release assembly and outside an already-released section:\n" +
		"  " + strings.Join(offending, "\n  ") + "\n\n" +
		"Put the entry in " + fragmentNameHelp(headRef) + " instead. " + changelogFile + " is edited by hand only below\n" +
		"the first released \"## [x.y.z]\" heading, that heading included (a correction to released text), or by\n" +
		"release assembly, which adds the heading of a new release.\n"
}

// addedFragments returns the changelog.d/*.md paths this branch adds, taken
// from `git diff --name-status` output. Status A counts, and so does the
// DESTINATION of a rename (R<score>): when the branch deletes one fragment and
// adds a near-identical one, rename detection reports a single R line, and the
// new path is this branch's entry. A rename's source, a rename out of
// changelog.d/, an edit (M) to a fragment another branch already landed and a
// deletion are not entries. A missing changelog.d/ directory simply yields
// nothing.
func addedFragments(nameStatus string) []string {
	var added []string
	for _, line := range strings.Split(nameStatus, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 || fields[0] == "" || (fields[0] != "A" && fields[0][0] != 'R') {
			continue
		}
		path := fields[len(fields)-1]
		if strings.HasPrefix(path, fragmentDir+"/") && strings.HasSuffix(path, ".md") {
			added = append(added, path)
		}
	}
	sort.Strings(added)
	return added
}

// releaseHeadingPattern matches a released version's heading as CHANGELOG.md
// and `assemble` write it — "## [1.9.2] - 2026-07-24" — with the date optional
// and trailing whitespace tolerated. Group 1 is the version. "## [Unreleased]"
// does not match, nor does any heading carrying another suffix, so neither can
// be taken for a released section.
var releaseHeadingPattern = regexp.MustCompile(`^## \[(\d+\.\d+\.\d+)\](?: - \d{4}-\d{2}-\d{2})?\s*$`)

// addsReleaseHeading reports whether a CHANGELOG.md diff adds a NEW release,
// which is what release assembly looks like and what an ordinary entry never
// does. Two things must hold. The diff adds more release-heading lines than it
// removes, so a heading re-typed in place — a date correction, a whitespace
// touch, a changed version — nets to zero and is not a release. And every
// added version is, in current (the file as of HEAD), the heading of exactly
// one section, so a heading that repeats a version the file already carries is
// not a release either. "## [Unreleased]" never counts.
func addsReleaseHeading(diff, current string) bool {
	removed := 0
	var added []string
	for _, line := range strings.Split(diff, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "-"):
			if releaseHeadingPattern.MatchString(line[1:]) {
				removed++
			}
		case strings.HasPrefix(line, "+"):
			if m := releaseHeadingPattern.FindStringSubmatch(line[1:]); m != nil {
				added = append(added, m[1])
			}
		}
	}
	if len(added) <= removed {
		return false
	}
	headings := map[string]int{}
	for _, line := range strings.Split(current, "\n") {
		if m := releaseHeadingPattern.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			headings[m[1]]++
		}
	}
	for _, version := range added {
		if headings[version] != 1 {
			return false
		}
	}
	return true
}

// validateFragment accepts either the "none" marker or a fragment whose first
// non-empty line is a known section header, whose every "### " line is a known
// section header, and which carries at least one line of entry text.
func validateFragment(name, content string) error {
	if isNoneMarker(content) {
		return nil
	}
	sections, err := parseSections(name, content)
	if err != nil {
		return err
	}
	if len(sections) == 0 {
		return fmt.Errorf("%s: no changelog entry found (expected a %q header followed by entry text, or the single line %q)", name, sectionOrder[0], noneMarker)
	}
	return nil
}

// isNoneMarker reports whether the file's first non-empty line is exactly the
// no-user-visible-change marker. The rest of such a file is ignored, so an
// author may explain the "none" underneath it.
func isNoneMarker(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed == noneMarker
		}
	}
	return false
}

// parseSections splits changelog text into one body per section header,
// preserving each body verbatim (multi-paragraph entries and their indentation
// survive a round trip). Repeated headers in one source are concatenated.
func parseSections(name, content string) (map[string]string, error) {
	sections := map[string]string{}
	current := ""
	var buf []string

	flush := func() {
		body := strings.Trim(strings.Join(buf, "\n"), "\n")
		buf = nil
		if current == "" || body == "" {
			return
		}
		if existing := sections[current]; existing != "" {
			body = existing + "\n\n" + body
		}
		sections[current] = body
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.HasPrefix(line, "### ") {
			if !isKnownSection(line) {
				return nil, fmt.Errorf("%s: unknown section header %q (expected one of: %s)", name, line, strings.Join(sectionOrder, ", "))
			}
			flush()
			current = line
			continue
		}
		if current == "" {
			if strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("%s: text before the first section header: %q (a fragment starts with a %q header, or with the single line %q)", name, strings.TrimSpace(line), sectionOrder[0], noneMarker)
			}
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return sections, nil
}

func isKnownSection(line string) bool {
	for _, section := range sectionOrder {
		if line == section {
			return true
		}
	}
	return false
}

func fragmentFormatHelp() string {
	return "A fragment holds the entry text that would previously have gone under \"## [Unreleased]\":\n" +
		"\n" +
		"    ### Fixed\n" +
		"\n" +
		"    - **Short summary.** What changed, and what an operator or a user notices.\n" +
		"\n" +
		"Known section headers: " + strings.Join(sectionOrder, ", ") + ".\n" +
		"Several sections may appear in one fragment.\n" +
		"\n" +
		"A pull request with no user-visible change writes a fragment whose first line is:\n" +
		"\n" +
		"    none\n"
}

// fragmentNameHelp says which file to add: the exact path when the branch is
// known, the rule otherwise.
func fragmentNameHelp(headRef string) string {
	if headRef != "" {
		return fragmentPathForBranch(headRef)
	}
	return fragmentDir + "/<slug>.md (the branch name without its type/ prefix)"
}

func misnamedFragmentHelp(headRef, want string, added []string) string {
	return "changelog fragment check FAILED: branch " + headRef + " adds no fragment named " + want + ".\n" +
		"\n" +
		"It adds: " + strings.Join(added, ", ") + "\n" +
		"Rename the entry to " + want + ": the fragment is named after the branch without its\n" +
		"type/ prefix, so each pull request's entry has a file no other branch writes. Rename it before\n" +
		"the pull request is queued — a queued branch refuses pushes.\n"
}

func missingFragmentHelp(headRef string) string {
	return "changelog fragment check FAILED: this branch adds no fragment under " + fragmentDir + "/.\n" +
		"\n" +
		"Add " + fragmentNameHelp(headRef) + " and put the changelog entry there instead of in\n" +
		changelogFile + ", which is rewritten only by release assembly\n" +
		"(go run ./scripts/changelogd assemble -version X.Y.Z) and by corrections to already-released text.\n" +
		"\n" + fragmentFormatHelp()
}

// assembleCommand parses the assemble flags and folds the accumulated
// fragments into CHANGELOG.md.
func assembleCommand(root string, args []string) (string, error) {
	flags := flag.NewFlagSet("assemble", flag.ContinueOnError)
	version := flags.String("version", "", "release version to assemble, as X.Y.Z")
	date := flags.String("date", "", "release date as YYYY-MM-DD (default: today, UTC)")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	return assemble(root, *version, *date)
}

// errNothingToRelease is what assemble returns when neither the frozen
// "[Unreleased]" body nor any fragment contributes an entry — the state the
// repository is legitimately in for as long as it takes the next pull request
// to land a fragment. It is a value rather than a message so a caller can
// recognise that state without matching prose: a fragment carrying only the
// `none` marker counts as a fragment and contributes nothing, so counting
// files is not the same question and does not answer it.
var errNothingToRelease = errors.New("nothing to release")

func assemble(root, version, date string) (string, error) {
	if !versionPattern.MatchString(version) {
		return "", fmt.Errorf("invalid -version %q (expected X.Y.Z)", version)
	}
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("invalid -date %q (expected YYYY-MM-DD)", date)
	}

	fragments, err := filepath.Glob(filepath.Join(root, fragmentDir, "*.md"))
	if err != nil {
		return "", fmt.Errorf("list fragments: %w", err)
	}
	sort.Strings(fragments)

	changelogPath := filepath.Join(root, changelogFile)
	content, err := os.ReadFile(changelogPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", changelogFile, err)
	}
	head, unreleased, tail, err := splitChangelog(string(content))
	if err != nil {
		return "", err
	}
	tail, linkWarning := updateReleaseLinks(tail, version)
	if linkWarning != "" {
		fmt.Fprintln(os.Stderr, "changelogd assemble: WARNING: "+linkWarning)
	}

	merged := map[string][]string{}
	frozen, err := parseSections(changelogFile+" [Unreleased]", strings.Join(stripPointerLine(unreleased), "\n"))
	if err != nil {
		return "", err
	}
	for section, body := range frozen {
		merged[section] = append(merged[section], body)
	}

	consumed := 0
	for _, fragment := range fragments {
		name := filepath.ToSlash(fragment)
		data, err := os.ReadFile(fragment)
		if err != nil {
			return "", fmt.Errorf("read fragment %s: %w", name, err)
		}
		consumed++
		if isNoneMarker(string(data)) {
			continue
		}
		sections, err := parseSections(name, string(data))
		if err != nil {
			return "", err
		}
		for _, section := range sectionOrder {
			if body := sections[section]; body != "" {
				merged[section] = append(merged[section], body)
			}
		}
	}

	if len(merged) == 0 {
		return "", fmt.Errorf("%w: no fragments in %s/ and no entries frozen under \"## [Unreleased]\" in %s", errNothingToRelease, fragmentDir, changelogFile)
	}

	out := renderChangelog(head, tail, merged, version, date)
	if err := os.WriteFile(changelogPath, []byte(out), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", changelogFile, err)
	}
	for _, fragment := range fragments {
		if err := os.Remove(fragment); err != nil {
			return "", fmt.Errorf("remove consumed fragment %s: %w", filepath.ToSlash(fragment), err)
		}
	}

	return fmt.Sprintf("assembled ## [%s] - %s into %s (%d fragment(s) consumed)", version, date, changelogFile, consumed), nil
}

// splitChangelog cuts CHANGELOG.md into everything through the
// "## [Unreleased]" heading, the body of that section, and everything from the
// next "## " heading onwards.
func splitChangelog(content string) (head, unreleased, tail []string, err error) {
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## [Unreleased]" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, nil, nil, fmt.Errorf("%s has no \"## [Unreleased]\" section", changelogFile)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return lines[:start+1], lines[start+1 : end], lines[end:], nil
}

// updateReleaseLinks keeps the trailing Keep a Changelog reference-link block
// in sync with a newly cut release: the release gets its own compare link,
// inserted right above the previous top entry, and "[Unreleased]" moves to
// compare against it instead of the release being cut. It is a no-op when
// tail carries no "[Unreleased]:" line at all (a changelog that predates the
// convention) or already lists this version — a rerun for a version that was
// already assembled, or corrected by hand, leaves the block untouched. When a
// "[Unreleased]:" line exists but its format has drifted from
// unreleasedLinkPattern, it returns tail untouched plus a non-empty warning
// instead of failing silently — a caller drops the update on the floor with
// no signal otherwise, which is the exact defect this function exists to fix.
func updateReleaseLinks(tail []string, version string) ([]string, string) {
	newLabel := "[" + version + "]:"
	for _, line := range tail {
		if strings.HasPrefix(line, newLabel) {
			return tail, ""
		}
	}
	for i, line := range tail {
		if !strings.HasPrefix(line, "[Unreleased]:") {
			continue
		}
		m := unreleasedLinkPattern.FindStringSubmatch(line)
		if m == nil {
			return tail, fmt.Sprintf("release-link block not updated: %q does not match the expected format", line)
		}
		base, prevTag := m[1], m[2]
		newTag := "v" + version
		out := make([]string, 0, len(tail)+1)
		out = append(out, tail[:i]...)
		out = append(out, "[Unreleased]: "+base+newTag+"...HEAD")
		out = append(out, "["+version+"]: "+base+prevTag+"..."+newTag)
		out = append(out, tail[i+1:]...)
		return out, ""
	}
	return tail, ""
}

// stripPointerLine drops the pointer that stands in for the entries assembly
// moved out, leaving the frozen backlog.
func stripPointerLine(body []string) []string {
	kept := make([]string, 0, len(body))
	for _, line := range body {
		if strings.TrimSpace(line) == pointerLine {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// renderChangelog rebuilds the file: the Unreleased body shrinks to the pointer
// line, the new release section follows it, and everything below is untouched.
func renderChangelog(head, tail []string, merged map[string][]string, version, date string) string {
	out := make([]string, 0, len(head)+len(tail)+16)
	out = append(out, head...)
	out = append(out, "", pointerLine, "")
	out = append(out, fmt.Sprintf("## [%s] - %s", version, date), "")
	for _, section := range sectionOrder {
		bodies := merged[section]
		if len(bodies) == 0 {
			continue
		}
		out = append(out, section, "")
		out = append(out, strings.Split(strings.Join(bodies, "\n\n"), "\n")...)
		out = append(out, "")
	}
	out = append(out, tail...)
	return strings.Join(out, "\n")
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
