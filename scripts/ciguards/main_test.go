// Package ciguards pins five CI/supply-chain fixes that shipped with no test
// of their own: patch-coverage's absence from the merge-queue suite, Trivy
// hiding an unfixed CVE from itself, an untrusted ref spliced into a shell
// command, and a checkout step holding onto a git credential it never needs.
// (The fifth, mutation-merge's `-expect` flag, is pinned in
// scripts/mutationmerge — the check already lived in that package, not this
// one.) Each of these is a workflow-file SHAPE a future edit could revert
// with nothing here to catch it, until now.
//
// Modelled on scripts/covmerge: every fixed property is a named function,
// proven first against a fixture this repository does not contain (so the
// rule is shown to refuse the shape it exists to refuse), then read against
// the real workflow.
package ciguards

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// repoRoot walks up from the test's working directory to the module root, the
// same way workflowfile's own (unexported) repoRoot does — needed here only
// to list the workflow directory, which workflowfile has no reason to expose.
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

// allWorkflowFiles lists every workflow under .github/workflows, module-root
// relative with forward slashes — the shape workflowfile.Read expects. A
// directory listing rather than a literal list, so a workflow added later is
// swept in without this file needing an edit. GitHub Actions loads `.yml` and
// `.yaml` identically, so both extensions are accepted — a filter on one
// spelling alone would make a workflow written with the other invisible to
// every guard below that relies on this list.
func allWorkflowFiles(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		files = append(files, ".github/workflows/"+entry.Name())
	}
	sort.Strings(files)
	return files
}

// ---------------------------------------------------------------------------
// REL-6: patch-coverage runs in the merge queue.
// ---------------------------------------------------------------------------

var jobIfLine = regexp.MustCompile(`(?m)^    if: (.*)$`)

// jobCondition returns a job's own single-line `if:` value, at the 4-space
// indentation a job key sits under its 2-space job header. Conditions in this
// repository are not folded across lines the way publishgate's is; a folded
// condition here is refused rather than mis-parsed.
func jobCondition(t *testing.T, block string) string {
	t.Helper()

	match := jobIfLine.FindStringSubmatch(block)
	if match == nil {
		t.Fatalf("job has no single-line `if:` key at 4-space indentation — it was reshaped, and this guard would judge nothing")
	}
	return match[1]
}

// PatchCoverageRunsInTheQueue refuses a patch-coverage `if:` that does not
// admit merge_group. REL-6: this is a REQUIRED check, but it used to run only
// on `pull_request` — in the merge_group suite that actually decides queue
// admission it reported `skipped`, and a skipped required check is satisfied,
// so a queued change could merge having never had its patch coverage judged
// there.
func PatchCoverageRunsInTheQueue(condition string) error {
	if !strings.Contains(condition, "github.event_name == 'merge_group'") {
		return fmt.Errorf("patch-coverage's `if:` (%s) does not admit merge_group — a queued change can merge without patch coverage ever being judged there", condition)
	}
	return nil
}

func TestPatchCoverageRunsInTheMergeQueue(t *testing.T) {
	block := workflowfile.Job(t, ".github/workflows/ci.yml", "patch-coverage")

	if err := PatchCoverageRunsInTheQueue(jobCondition(t, block)); err != nil {
		t.Fatal(err)
	}
}

func TestPatchCoverageRunsInTheQueueRefusesAPullRequestOnlyCondition(t *testing.T) {
	if err := PatchCoverageRunsInTheQueue("github.event_name == 'pull_request'"); err == nil {
		t.Fatal("a condition admitting only pull_request was accepted")
	}
}

// ---------------------------------------------------------------------------
// REL-7: --ignore-unfixed removed at the three vulnerability-gating Trivy
// scans.
// ---------------------------------------------------------------------------

// vulnScanLine matches any Trivy invocation that gates on vulnerabilities
// (`--scanners vuln`), across whichever files call it — a fourth site added
// later is judged by the same rule, not only the three REL-7 touched.
var vulnScanLine = regexp.MustCompile(`(?m)^.*--scanners vuln.*$`)

func vulnScanLines(t *testing.T, workflow string) []string {
	t.Helper()
	return vulnScanLine.FindAllString(workflowfile.Read(t, workflow), -1)
}

// NoIgnoreUnfixedAmongVulnScans refuses any vulnerability-gating Trivy
// invocation that carries --ignore-unfixed. REL-7: it was set on the
// filesystem scan, the image scan, and the pre-publish re-scan — all three
// required or gating checks, all three writing what the Security tab or the
// publish gate reads — so a CRITICAL with no upstream fix yet, the ordinary
// case for a fresh CVE, tripped none of them.
func NoIgnoreUnfixedAmongVulnScans(lines []string) error {
	var offending []string
	for _, line := range lines {
		if strings.Contains(line, "--ignore-unfixed") {
			offending = append(offending, strings.TrimSpace(line))
		}
	}
	if len(offending) > 0 {
		return fmt.Errorf("%d vulnerability-gating Trivy scan(s) carry --ignore-unfixed, hiding an unfixed CRITICAL/HIGH finding from the gate and the Security tab: %v", len(offending), offending)
	}
	return nil
}

func TestNoIgnoreUnfixedAmongTheThreeVulnGatingTrivyScans(t *testing.T) {
	// Every workflow is swept, not just the two REL-7 happened to touch —
	// otherwise a vulnerability-gating scan added to a third workflow is
	// never even opened, and neither half of this test would notice: this
	// loop wouldn't read it, and a count fixed at 3 would still see exactly
	// 3 among the files it DID read.
	var lines []string
	for _, workflow := range allWorkflowFiles(t) {
		lines = append(lines, vulnScanLines(t, workflow)...)
	}

	// A hardcoded expected count has the same staleness problem the REL-10
	// guard below refuses: it goes stale the day a workflow adds another
	// vulnerability-gating scan. The property this guards is "no such scan
	// carries --ignore-unfixed", checked over every one this sweep finds;
	// this assertion only guards against the sweep itself going vacuous
	// (finding zero scans because the pattern or the directory broke).
	if len(lines) == 0 {
		t.Fatal("found zero vulnerability-gating Trivy scans across every workflow — the scan itself is broken, since security.yml and docker-image.yml both gate on one")
	}

	if err := NoIgnoreUnfixedAmongVulnScans(lines); err != nil {
		t.Fatal(err)
	}
}

func TestNoIgnoreUnfixedAmongVulnScansRefusesAnOffendingLine(t *testing.T) {
	clean := []string{`run: docker run "$TRIVY" image --scanners vuln --severity HIGH,CRITICAL --exit-code 1 x`}
	if err := NoIgnoreUnfixedAmongVulnScans(clean); err != nil {
		t.Fatalf("a scan without --ignore-unfixed was refused: %v", err)
	}

	dirty := []string{`run: docker run "$TRIVY" image --scanners vuln --ignore-unfixed --severity HIGH,CRITICAL --exit-code 1 x`}
	if err := NoIgnoreUnfixedAmongVulnScans(dirty); err == nil {
		t.Fatal("a scan carrying --ignore-unfixed was accepted")
	}
}

// ---------------------------------------------------------------------------
// REL-9: the changelog-fragment gate no longer splices the PR base ref
// straight into a shell command.
// ---------------------------------------------------------------------------

var stepHeader = regexp.MustCompile(`(?m)^      - name: `)

// stepBlock cuts one step out of a job block by its `name:`, the same
// fail-closed shape workflowfile.Job cuts a job out of a workflow with: a
// renamed or removed step is a failure here, not a silently empty search.
func stepBlock(t *testing.T, jobBlock, stepName string) string {
	t.Helper()

	header := "\n      - name: " + stepName + "\n"
	start := strings.Index(jobBlock, header)
	if start < 0 {
		t.Fatalf("no step named %q — it was renamed or removed, and this guard would judge nothing", stepName)
	}
	rest := jobBlock[start+len(header):]
	if next := stepHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

// runBody returns a step's `run:` key and everything more indented beneath
// it — the block-scalar body — stopping at the first line back at or above
// `run:`'s own indentation: a sibling key (`env:`, `with:`, `if:`, …) or the
// end of the step. YAML mapping keys are unordered, so `env:` cannot be
// assumed to sit above `run:` in the text; cutting at the next sibling KEY,
// wherever it falls, is what keeps a legitimate `${{ }}` in a same-step
// `env:` block from ever entering the text this file inspects for a splice.
func runBody(t *testing.T, block string) string {
	t.Helper()

	lines := strings.Split(block, "\n")

	start, indent := -1, 0
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "run:") {
			start = i
			indent = len(line) - len(trimmed)
			break
		}
	}
	if start < 0 {
		t.Fatalf("step has no `run:` key")
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		lineIndent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if lineIndent <= indent {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// NoUntrustedRefSplicedIntoRun refuses a `run:` body that reaches a GitHub
// Actions event expression directly, and separately requires the safe
// replacement to actually be present — an absence-only check would pass a
// `run:` block that dropped the base-ref fetch entirely and verified
// nothing. REL-9: `git fetch --no-tags origin ${{
// github.event.pull_request.base.ref }}` interpolated an unescaped
// expression straight into `run:`; a ref name is fork-PR-controlled. Fixed by
// routing it through env.PR_BASE_REF and quoting the shell use:
// "$PR_BASE_REF" — the same pattern `ci.yml`'s `changes` job already used.
func NoUntrustedRefSplicedIntoRun(body string) error {
	if strings.Contains(body, "${{") {
		return fmt.Errorf("a `run:` block splices an Actions expression directly rather than through a quoted env var: %q", body)
	}
	if !strings.Contains(body, `"$PR_BASE_REF"`) {
		return fmt.Errorf("a `run:` block no longer reaches the base ref through the quoted $PR_BASE_REF shell variable: %q", body)
	}
	return nil
}

func TestChangelogFragmentDoesNotSpliceTheBaseRefIntoRun(t *testing.T) {
	block := workflowfile.Job(t, ".github/workflows/changelog.yml", "changelog-fragment")
	step := stepBlock(t, block, "Check changelog fragment")

	if err := NoUntrustedRefSplicedIntoRun(runBody(t, step)); err != nil {
		t.Fatal(err)
	}
}

// TestRunBodyStopsAtTheNextSiblingKeyRegardlessOfOrder proves runBody does not
// rely on `env:` sitting above `run:` in the text. YAML mapping keys are
// unordered — a step with `run:` first and `env:` after is valid and behaves
// identically — and a reader that assumed `env:`-then-`run:` would fold a
// same-step `env:`'s legitimate `${{ }}` straight into the body this file
// scans for a splice, failing a correct workflow.
func TestRunBodyStopsAtTheNextSiblingKeyRegardlessOfOrder(t *testing.T) {
	block := strings.Join([]string{
		"      - name: Some step",
		"        run: |",
		"          echo hi",
		"        env:",
		"          PR_BASE_REF: ${{ github.event.pull_request.base.ref }}",
		"",
	}, "\n")

	body := runBody(t, block)

	if strings.Contains(body, "${{") {
		t.Fatalf("runBody with env: AFTER run: leaked the env: block's expression into the body: %q", body)
	}
	if body != "        run: |\n          echo hi" {
		t.Fatalf("runBody with env: AFTER run: returned %q, want just the run: block", body)
	}
}

func TestNoUntrustedRefSplicedIntoRunRefusesARawSplice(t *testing.T) {
	spliced := "\n        run: |\n          git fetch --no-tags origin ${{ github.event.pull_request.base.ref }}\n"
	if err := NoUntrustedRefSplicedIntoRun(spliced); err == nil {
		t.Fatal("a run: block splicing the raw expression was accepted")
	}

	unquoted := "\n        run: |\n          git fetch --no-tags origin $PR_BASE_REF\n"
	if err := NoUntrustedRefSplicedIntoRun(unquoted); err == nil {
		t.Fatal("a run: block using the safe variable unquoted was accepted")
	}
}

// ---------------------------------------------------------------------------
// REL-10: every actions/checkout step sets persist-credentials: false.
// ---------------------------------------------------------------------------

var stepMarkerLine = regexp.MustCompile(`(?m)^(\s*)- `)

// CheckoutSitesMissingPersistCredentialsFalse scans one workflow's raw text
// for every `actions/checkout` step and reports which ones do not set
// `persist-credentials: false` anywhere in their own step mapping. `total` is
// every checkout site FOUND by this scan, never a number carried in this
// file — the completeness the caller asserts is "every site found", so a
// checkout step added later is judged the same way without this file needing
// an edit.
func CheckoutSitesMissingPersistCredentialsFalse(content string) (missing []string, total int) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "uses: actions/checkout@") {
			continue
		}
		total++

		// A step's own indentation is that of its "- " marker, which may be
		// this very line (a combined "- uses: …") or a preceding "- name:"
		// line — walk backward to find it, so the body captured below is the
		// whole step mapping and not just the siblings of `uses:` itself.
		stepIndent := -1
		for j := i; j >= 0; j-- {
			if m := stepMarkerLine.FindStringSubmatch(lines[j]); m != nil {
				stepIndent = len(m[1])
				break
			}
		}
		if stepIndent < 0 {
			missing = append(missing, fmt.Sprintf("line %d: %s (no enclosing step marker found)", i+1, strings.TrimSpace(line)))
			continue
		}

		set := false
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" {
				continue
			}
			indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
			if indent <= stepIndent {
				break
			}
			if trimmed == "persist-credentials: false" {
				set = true
				break
			}
		}
		if !set {
			missing = append(missing, fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return missing, total
}

// TestEveryCheckoutSetsPersistCredentialsFalse sweeps every workflow file and
// judges every actions/checkout site it finds — not a fixed 26, since that
// count goes stale the day a workflow adds another checkout step. REL-10: it
// was present at exactly 1 of 26 occurrences across 8 files; none of the
// other 25 push or otherwise need the persisted git credential afterward.
func TestEveryCheckoutSetsPersistCredentialsFalse(t *testing.T) {
	var allMissing []string
	total := 0
	for _, workflow := range allWorkflowFiles(t) {
		missing, count := CheckoutSitesMissingPersistCredentialsFalse(workflowfile.Read(t, workflow))
		total += count
		for _, m := range missing {
			allMissing = append(allMissing, workflow+": "+m)
		}
	}

	if total == 0 {
		t.Fatal("found zero actions/checkout sites across every workflow — the scan itself is broken, since this repository checks out its own code in several of them")
	}
	if len(allMissing) > 0 {
		t.Fatalf("%d of %d actions/checkout site(s) do not set persist-credentials: false: %v", len(allMissing), total, allMissing)
	}
}

func TestCheckoutSitesMissingPersistCredentialsFalseCatchesAMissingFlag(t *testing.T) {
	content := strings.Join([]string{
		"jobs:",
		"  build:",
		"    steps:",
		"      - name: Checkout",
		"        uses: actions/checkout@deadbeef # v7.0.1",
		"        with:",
		"          persist-credentials: false",
		"",
		"      - name: Checkout again",
		"        uses: actions/checkout@deadbeef # v7.0.1",
		"        with:",
		"          fetch-depth: 0",
		"",
	}, "\n")

	missing, total := CheckoutSitesMissingPersistCredentialsFalse(content)

	if total != 2 {
		t.Fatalf("found %d checkout site(s), want 2", total)
	}
	if len(missing) != 1 || !strings.HasPrefix(missing[0], "line 10:") {
		t.Fatalf("found missing site(s) %v, want exactly line 10 (the second checkout, carrying only fetch-depth)", missing)
	}
}

// ---------------------------------------------------------------------------
// run_frontend's allowlist covers every Tailwind @source, not just web/**.
// ---------------------------------------------------------------------------

// stripCSSComments drops CSS comments so prose that mentions the directive is
// not mistaken for one. It tracks quotes: a glob such as "templates/**/*.html"
// contains `/*` and `*/` and is not a comment.
func stripCSSComments(css string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case quote != 0:
			if c == '\\' && i+1 < len(css) {
				b.WriteByte(c)
				i++
				c = css[i]
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += 2 + end + 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// cssSourceDirective matches every `@source` token; each one must then be
// classified by cssSourcePath or cssSourceIgnored, or the scan refuses the
// file — a guard keyed on one spelling of the directive would let a second
// spelling (single quotes, extra spaces) walk past it.
var cssSourceDirective = regexp.MustCompile(`@source\b[^;]*;`)

// cssSourcePath matches a path declaration in either quote style.
var cssSourcePath = regexp.MustCompile(`^@source\s+(?:"([^"]+)"|'([^']+)')\s*;$`)

// cssSourceIgnored matches the forms that add no file to the build's inputs:
// `@source not "..."` (an exclusion) and `@source inline(...)` (a literal
// class list, no path).
var cssSourceIgnored = regexp.MustCompile(`^@source\s+(?:not\s|inline\()`)

// cssSources returns every @source path declared there, resolved to a
// module-root-relative, forward-slashed path — the same shape a file name in
// the `changes` job's diff has. Glob syntax ("**", "*") is kept rather than
// expanded: the caller decides how to test one against a regexp, because
// "does the pattern match this literal path" and "does it match every
// possible expansion of this glob" are different questions.
func cssSources(t *testing.T) []string {
	t.Helper()

	cssPath := filepath.Join(repoRoot(t), "web", "src", "css", "input.css")
	raw, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("read web/src/css/input.css: %v", err)
	}

	sources, err := CSSSourcePaths(string(raw))
	if err != nil {
		t.Fatalf("web/src/css/input.css: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("found zero @source paths in web/src/css/input.css — the scan itself is broken, since the Tailwind build declares its content sources there")
	}
	return sources
}

// CSSSourcePaths returns every path an `@source` in css declares, resolved
// against web/src/css, and refuses any `@source` it cannot classify.
func CSSSourcePaths(css string) ([]string, error) {
	var sources []string
	for _, directive := range cssSourceDirective.FindAllString(stripCSSComments(css), -1) {
		if cssSourceIgnored.MatchString(directive) {
			continue
		}
		m := cssSourcePath.FindStringSubmatch(directive)
		if m == nil {
			return nil, fmt.Errorf("unclassified @source directive %q: teach this guard its form before relying on it", directive)
		}
		source := m[1] + m[2]
		sources = append(sources, path.Clean(path.Join("web/src/css", source)))
	}
	return sources, nil
}

func TestCSSSourcePathsReadsEveryDirectiveForm(t *testing.T) {
	css := `/* @source "ignored/in/a/comment"; */
.x { content: "a\"b"; } /* @source "also/ignored"; */
@source "../../../internal/a/**/*.html"; /* trailing */
@source   '../../../internal/b.go' ;
@source not "../../../internal/c";
@source inline("underline");
`
	got, err := CSSSourcePaths(css)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/a/**/*.html", "internal/b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("CSSSourcePaths = %v, want %v", got, want)
	}

	if _, err := CSSSourcePaths(`@source url(../x);`); err == nil {
		t.Fatal("an @source of an unknown form was accepted silently")
	}
}

// runFrontendPattern extracts the `grep -E '...'` argument the `changes` job
// evaluates for `frontend_changes` — the same string the shell step itself
// runs against a diff, read out by plain substring search rather than a
// second regexp, so nothing here has to agree with itself about how to escape
// one.
func runFrontendPattern(t *testing.T) string {
	t.Helper()

	block := workflowfile.Job(t, ".github/workflows/ci.yml", "changes")
	marker := `frontend_changes="$(printf '%s\n' "$files" | grep -E '`
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatal("no `frontend_changes=` assignment found in the `changes` job — it was renamed or reshaped, and this guard would judge nothing")
	}
	rest := block[start+len(marker):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatal("`frontend_changes=`'s grep -E argument has no closing quote — the line was reshaped, and this guard would judge nothing")
	}
	return rest[:end]
}

// samplePathForSource turns a possibly-globbed @source path into one concrete
// path a real file there would have. "**" (any depth of directories) and "*"
// (any single path segment) are Tailwind's only wildcards among these
// sources; substituting a literal, slash-free segment for each cannot
// accidentally satisfy an alternative in the pattern that a real expansion
// would not.
func samplePathForSource(source string) string {
	sample := strings.ReplaceAll(source, "**", "sampledir")
	return strings.ReplaceAll(sample, "*", "samplefile")
}

// FrontendPatternCoversCSSSources reports every CSS @source (as the concrete
// sample path a real file there would have) the run_frontend allowlist
// regexp does not match — named sites, never a count, so the caller can say
// exactly which `@source` a pattern edit dropped.
func FrontendPatternCoversCSSSources(pattern string, sources []string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("run_frontend pattern %q does not compile: %w", pattern, err)
	}
	var uncovered []string
	for _, source := range sources {
		if !re.MatchString(samplePathForSource(source)) {
			uncovered = append(uncovered, source)
		}
	}
	return uncovered, nil
}

// TestRunFrontendPatternCoversEveryCSSSource is the real-file proof: every
// @source web/src/css/input.css declares today must reach a real diff on
// test-frontend's run_frontend output, or a change confined to one silently
// skips the job whose "Committed bundles must match a fresh build" step is
// the only thing that would catch the resulting stale CSS.
func TestRunFrontendPatternCoversEveryCSSSource(t *testing.T) {
	pattern := runFrontendPattern(t)
	sources := cssSources(t)

	uncovered, err := FrontendPatternCoversCSSSources(pattern, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(uncovered) > 0 {
		t.Fatalf("run_frontend's allowlist does not cover %d @source path(s) declared by web/src/css/input.css: %v", len(uncovered), uncovered)
	}
}

// TestFrontendPatternCoversCSSSourcesRefusesAMissingSource proves the guard
// above actually refuses something, rather than passing vacuously: with the
// real internal/templates alternative stripped out of the real pattern, the
// real @source lines from input.css must come back naming exactly that source
// as uncovered.
func TestFrontendPatternCoversCSSSourcesRefusesAMissingSource(t *testing.T) {
	pattern := runFrontendPattern(t)
	sources := cssSources(t)

	narrowed := strings.Replace(pattern, "internal/templates/|", "", 1)
	if narrowed == pattern {
		t.Fatal("the internal/templates alternative was not found in the real pattern — this test no longer narrows anything")
	}

	uncovered, err := FrontendPatternCoversCSSSources(narrowed, sources)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range uncovered {
		if strings.HasPrefix(u, "internal/templates/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dropping the internal/templates alternative from the pattern did not surface it as uncovered: %v", uncovered)
	}
}

// ---------------------------------------------------------------------------
// The `changes` job's detect step, executed rather than read.
// ---------------------------------------------------------------------------

// detectScript returns the detect step's `run: |` block, de-indented, exactly
// as the runner hands it to bash.
func detectScript(t *testing.T) string {
	t.Helper()

	block := workflowfile.Job(t, ".github/workflows/ci.yml", "changes")
	start := strings.Index(block, "id: detect")
	if start < 0 {
		t.Fatal("no `id: detect` step in the `changes` job")
	}
	rest := block[start:]
	run := strings.Index(rest, "run: |\n")
	if run < 0 {
		t.Fatal("the detect step has no `run: |` block")
	}

	var lines []string
	indent := -1
	for _, line := range strings.Split(rest[run+len("run: |\n"):], "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			lines = append(lines, "")
			continue
		}
		depth := len(line) - len(trimmed)
		if indent < 0 {
			indent = depth
		}
		if depth < indent {
			break
		}
		lines = append(lines, line[indent:])
	}
	return strings.Join(lines, "\n")
}

// runDetect runs script in a throwaway repository whose `main` holds one file
// and whose checked-out branch adds files on top, and returns the outputs the
// script wrote to GITHUB_OUTPUT. The repository is its own `origin`, so the
// script's `git fetch origin main` resolves without a network.
func runDetect(t *testing.T, script, event string, files []string) map[string]string {
	t.Helper()

	dir := t.TempDir()
	bash := requireBash(t, dir)
	// Hermetic: the fixture repository must not pick up the machine's git
	// config (hooks, signing, default branch).
	env := append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=ciguards", "GIT_AUTHOR_EMAIL=ciguards@example.invalid",
		"GIT_COMMITTER_NAME=ciguards", "GIT_COMMITTER_EMAIL=ciguards@example.invalid",
	)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("README.md")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	baseSHA := ""
	if event == "merge_group" {
		rev := exec.Command("git", "rev-parse", "HEAD")
		rev.Dir = dir
		rev.Env = env
		sha, err := rev.Output()
		if err != nil {
			t.Fatalf("git rev-parse HEAD: %v", err)
		}
		baseSHA = strings.TrimSpace(string(sha))
	}
	git("remote", "add", "origin", dir)
	git("checkout", "-q", "-b", "change")
	for _, f := range files {
		write(f)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "change")

	output := filepath.ToSlash(filepath.Join(t.TempDir(), "output"))
	// As the runner runs a step with no `shell:` — `bash -e {0}`, from a file.
	// Not `-c`: the step is long enough that a Windows command line truncates
	// it silently, inside a comment, with exit status 0.
	scriptFile := filepath.Join(t.TempDir(), "detect.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "-e", filepath.ToSlash(scriptFile))
	cmd.Dir = dir
	cmd.Env = append(env, "EVENT_NAME="+event, "BASE_REF=main", "QUEUE_BASE_SHA="+baseSHA, "GITHUB_OUTPUT="+output)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detect step failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("detect step wrote no outputs: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			got[k] = v
		}
	}
	return got
}

// requireBash returns a bash that can enter dir and run git — being on PATH is
// not the test (on Windows that is often WSL's, which cannot see the fixture).
// A guard that reports green because it could not look is worse than none, so
// only a Windows developer machine outside CI may skip.
func requireBash(t *testing.T, dir string) string {
	t.Helper()

	path, err := exec.LookPath("bash")
	if err == nil {
		out, probeErr := exec.Command(path, "-c", `cd "$1" && git --version >/dev/null && printf ok`, "probe", filepath.ToSlash(dir)).Output()
		if got := strings.TrimSpace(string(out)); probeErr != nil || got != "ok" {
			err = fmt.Errorf("%s answered %q, not \"ok\": %v", path, got, probeErr)
		}
	}
	if err != nil {
		if runtime.GOOS != "windows" || os.Getenv("CI") != "" {
			t.Fatalf("bash with git is required to execute the detect step, and this guard proves nothing without it: %v", err)
		}
		t.Skipf("bash with git is required to execute the detect step: %v", err)
	}
	return path
}

type detectCase struct {
	name  string
	event string
	files []string
	want  map[string]string
}

var detectCases = []detectCase{
	{"docs only", "pull_request", []string{"docs/a.md"},
		map[string]string{"run_e2e": "false", "run_core": "false", "run_frontend": "false"}},
	{"Go test file only", "pull_request", []string{"internal/x/a_test.go"},
		map[string]string{"run_e2e": "false", "run_core": "true", "run_unit": "true", "run_race": "true", "run_frontend": "false"}},
	{"Go testdata only", "pull_request", []string{"internal/x/testdata/f.json"},
		map[string]string{"run_e2e": "false", "run_core": "true"}},
	{"testdata under e2e", "pull_request", []string{"e2e/testdata/f.json"},
		map[string]string{"run_e2e": "true", "run_frontend": "true"}},
	{"prod Go only", "pull_request", []string{"internal/x/a.go"},
		map[string]string{"run_e2e": "true", "run_core": "true", "run_frontend": "false"}},
	{"template only", "pull_request", []string{"internal/templates/a.html"},
		map[string]string{"run_frontend": "true"}},
	{"this workflow only", "pull_request", []string{".github/workflows/ci.yml"},
		map[string]string{"run_frontend": "true", "run_e2e": "true"}},
	{"non-ASCII frontend path", "pull_request", []string{"web/src/js/é.js"},
		map[string]string{"run_frontend": "true"}},
	{"merge_group, Go test file only", "merge_group", []string{"internal/x/a_test.go"},
		map[string]string{"run_e2e": "false", "run_core": "true", "run_frontend": "false"}},
	{"merge_group, template only", "merge_group", []string{"internal/templates/a.html"},
		map[string]string{"run_e2e": "true", "run_frontend": "true"}},
	{"push", "push", []string{"web/src/js/a.js"},
		map[string]string{"run_core": "false", "run_frontend": "false", "run_e2e": "true"}},
}

func TestDetectStepDecidesEachLaneFromTheDiff(t *testing.T) {
	script := detectScript(t)
	for _, c := range detectCases {
		t.Run(c.name, func(t *testing.T) {
			got := runDetect(t, script, c.event, c.files)
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q (all outputs: %v)", k, got[k], v, got)
				}
			}
		})
	}
}

// TestDetectStepHarnessRefusesTheUnfixedShapes proves the harness above can
// fail: each mutation reverts one fix, and the case it exists for must flip.
func TestDetectStepHarnessRefusesTheUnfixedShapes(t *testing.T) {
	script := detectScript(t)
	for _, m := range []struct {
		name, from, to string
		files          []string
		output         string
	}{
		{"quoted paths", "git -c core.quotePath=false diff", "git diff", []string{"web/src/js/é.js"}, "run_frontend"},
		{"workflow not an input", `|\.github/workflows/ci\.yml$`, "", []string{".github/workflows/ci.yml"}, "run_frontend"},
	} {
		t.Run(m.name, func(t *testing.T) {
			mutated := strings.Replace(script, m.from, m.to, 1)
			if mutated == script {
				t.Fatalf("%q not found in the detect step: this check no longer reverts anything", m.from)
			}
			if got := runDetect(t, mutated, "pull_request", m.files)[m.output]; got != "false" {
				t.Fatalf("with the fix reverted, %s = %q; the harness cannot see the defect it guards", m.output, got)
			}
		})
	}
}
