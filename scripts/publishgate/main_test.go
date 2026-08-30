// Package publishgate holds the CI workflow's publish gate to a shape that an
// `if` carrying no status-check function cannot have.
//
// `publish-image` in .github/workflows/ci.yml is the only path by which
// `ghcr.io/<repo>:latest` follows `main`. It lists five jobs in `needs:` and
// carried an `if` naming only the event and the ref. With no status-check
// function in it, GitHub supplies the implicit `success()` — and a skip
// propagates transitively, while a gate that rescues ITSELF with `!cancelled()`
// does not rescue what depends on it. All five gates report `success` on
// `push`, but every lane beneath them is skipped there (`changes` clears
// `run_core` on `push` by design), so the implicit predicate was false and the
// publish job was skipped inside a run that reported green. Six consecutive
// runs — 33217555948, 33213567903, 33188023148, 32905532451, 32882518209,
// 32853636750 — show it `skipped` with `started_at == completed_at`, and at
// `4b3c01d7` the registry's `latest`, `main` and `sha-3891c0f` all pointed at
// one index 242 commits behind `main`.
//
// Nothing in a green CI run says so. A skipped job is a satisfied check, and a
// tag that never appeared in a registry is not a signal any workflow reads, so
// the failure is silent by construction and stayed silent for 242 commits.
// This guard is the signal. It asserts the two halves that only work together —
// the status override that suppresses the implicit `success()`, and an explicit
// `success` requirement per dependency, since `!cancelled()` alone would publish
// off a FAILED gate — and it reads the requirements off the job's own `needs:`
// list, so a sixth dependency added without a sixth clause is a failure here
// rather than a hole in the gate.
package publishgate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// workflowPath is the workflow that owns both the publish job and the five
// jobs it gates on: `needs:` cannot cross a workflow boundary, which is why the
// gate lives there rather than beside the publish itself.
var workflowPath = filepath.Join(".github", "workflows", "ci.yml")

// publishJob is the job whose `if` this package guards.
const publishJob = "publish-image"

// wantNeeds is the dependency set the gate was designed around. The
// per-dependency assertions below are driven by the ACTUAL `needs:` list, not
// by this one — that is what makes an added dependency fail. This list is the
// other direction: a dependency silently dropped from `needs:` weakens the gate
// just as much, and would leave a check that reads the actual list green.
var wantNeeds = []string{"e2e", "e2e-postgres-smoke", "image-smoke", "race", "test"}

var (
	jobHeader   = regexp.MustCompile(`(?m)^  [A-Za-z0-9_.-]+:[ \t]*$`)
	needsEntry  = regexp.MustCompile(`(?m)^[ \t]*-[ \t]+([A-Za-z0-9_.-]+)[ \t]*$`)
	foldMarker  = regexp.MustCompile(`^[>|][-+]?[ \t]*`)
	whitespaces = regexp.MustCompile(`[ \t\n\r]+`)
)

// TestPublishImageGateOverridesTheImplicitSuccessAndJudgesEveryDependency is
// the whole package: both halves of the gate, over every job the publish
// actually depends on.
func TestPublishImageGateOverridesTheImplicitSuccessAndJudgesEveryDependency(t *testing.T) {
	block := jobBlock(t)
	condition := jobField(t, block, "if")
	needs := jobNeeds(t, block)

	if !strings.Contains(condition, "!cancelled()") {
		t.Errorf("%s: the `if` of %q holds no status-check function (%q), so GitHub supplies the implicit `success()` — which reads the whole ancestor graph and is false on every push to `main`, skipping the publish inside a green run",
			workflowPath, publishJob, condition)
	}

	for _, job := range needs {
		clause := "needs." + job + ".result == 'success'"
		if !strings.Contains(condition, clause) {
			t.Errorf("%s: the `if` of %q does not carry %q, so a %q that FAILED still publishes: `!cancelled()` suppresses the implicit `success()` for every dependency at once, and nothing else judges them",
				workflowPath, publishJob, clause, job)
		}
	}

	sort.Strings(needs)
	if strings.Join(needs, ",") != strings.Join(wantNeeds, ",") {
		t.Errorf("%s: %q now depends on %v, not on %v — a dependency dropped from `needs:` also disappears from the loop above, which reads that same list, so the change has to be judged here by hand",
			workflowPath, publishJob, needs, wantNeeds)
	}
}

// jobBlock returns the text of the publish job, from its header to the next job
// header at the same indentation. It fails closed: a renamed or moved job is a
// failure here, never a silently empty search.
func jobBlock(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), workflowPath))
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")

	header := "\n  " + publishJob + ":\n"
	start := strings.Index(content, header)
	if start < 0 {
		t.Fatalf("%s: no job named %q — the publish path was renamed or removed, and this guard would judge nothing", workflowPath, publishJob)
	}
	rest := content[start+len(header):]

	if next := jobHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

// jobNeeds reads the job's dependency list off the workflow rather than
// restating it, so the per-dependency assertions grow with the job.
func jobNeeds(t *testing.T, block string) []string {
	t.Helper()

	var needs []string
	for _, line := range jobFieldLines(t, block, "needs") {
		if match := needsEntry.FindStringSubmatch(line); match != nil {
			needs = append(needs, match[1])
		}
	}
	if len(needs) == 0 {
		t.Fatalf("%s: %q lists no dependencies, so it is gated on nothing at all", workflowPath, publishJob)
	}
	return needs
}

// jobField returns the value of one key of the job mapping as a single line,
// with the block-scalar indicator stripped and whitespace collapsed, so that
// folding an expression across lines does not change what this file matches on.
func jobField(t *testing.T, block, key string) string {
	t.Helper()

	joined := strings.TrimSpace(strings.Join(jobFieldLines(t, block, key), " "))
	return whitespaces.ReplaceAllString(foldMarker.ReplaceAllString(joined, ""), " ")
}

// jobFieldLines returns one key of the job mapping line by line — the text
// after the colon, then every more-indented line under it, comments and blank
// lines dropped. A sequence value has to stay line-shaped: collapsing it would
// run its entries together into one string with no item boundary left.
func jobFieldLines(t *testing.T, block, key string) []string {
	t.Helper()

	var (
		value   []string
		reading bool
	)
	for _, line := range strings.Split(block, "\n") {
		switch {
		case strings.HasPrefix(line, "    "+key+":"):
			reading = true
			value = append(value, strings.TrimSpace(strings.TrimPrefix(line, "    "+key+":")))
			continue
		case !reading:
			continue
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			continue
		case strings.HasPrefix(line, "     "):
			value = append(value, strings.TrimSpace(line))
			continue
		}
		reading = false
	}
	if len(value) == 0 {
		t.Fatalf("%s: %q has no `%s:` key", workflowPath, publishJob, key)
	}
	return value
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
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
