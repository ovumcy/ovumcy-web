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
	"path/filepath"
	"regexp"
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
// swept in without this file needing an edit.
func allWorkflowFiles(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
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
	var lines []string
	for _, workflow := range []string{
		".github/workflows/security.yml",
		".github/workflows/docker-image.yml",
	} {
		lines = append(lines, vulnScanLines(t, workflow)...)
	}

	// REL-7 touched exactly three vulnerability-gating Trivy invocations
	// (trivy-fs and trivy-image in security.yml; the pre-publish re-scan in
	// docker-image.yml). A count that has moved means a scan site was added
	// or removed and deserves a human decision about --ignore-unfixed, not a
	// guard that silently widens or narrows around it.
	if len(lines) != 3 {
		t.Fatalf("found %d vulnerability-gating Trivy scan(s), want exactly the three REL-7 covers: %v", len(lines), lines)
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

// runBody returns a step's `run:` key onward, deliberately excluding the
// `env:` mapping above it: `env:` is where a GitHub Actions expression is
// SUPPOSED to land, and only a splice that reaches `run:` itself is the
// untrusted-expression defect this guards against.
func runBody(t *testing.T, block string) string {
	t.Helper()

	idx := strings.Index(block, "\n        run:")
	if idx < 0 {
		t.Fatalf("step has no `run:` key at the expected 8-space indentation")
	}
	return block[idx:]
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
