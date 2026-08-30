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

// eventDisjunction is the ONE `||` the gate is allowed to hold: the two events
// that may publish. Everything else has to be a conjunction, and gateProblems
// enforces exactly that — a clause wrapped in an `||` still carries the
// substring the per-dependency check matches on while no longer gating
// anything, which is the one way those checks read green over a reopened gate.
// Spelled with the same spacing the workflow uses: rewritten differently it is
// not recognised, its own `||` survives into the check, and the condition is
// referred back to a reader rather than guessed at.
const eventDisjunction = "(github.event_name == 'push' || github.event_name == 'workflow_dispatch')"

var (
	jobHeader   = regexp.MustCompile(`(?m)^  [A-Za-z0-9_.-]+:[ \t]*$`)
	needsEntry  = regexp.MustCompile(`^-[ \t]+([A-Za-z0-9_.-]+)$`)
	plainName   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	foldMarker  = regexp.MustCompile(`^[>|][-+]?[ \t]*`)
	whitespaces = regexp.MustCompile(`[ \t\n\r]+`)
)

// TestPublishImageGateOverridesTheImplicitSuccessAndJudgesEveryDependency reads
// the real workflow and judges the gate it declares.
func TestPublishImageGateOverridesTheImplicitSuccessAndJudgesEveryDependency(t *testing.T) {
	block := jobBlock(t)
	needs := jobNeeds(t, block)

	for _, problem := range gateProblems(jobField(t, block, "if"), needs) {
		t.Errorf("%s, job %q: %s", workflowPath, publishJob, problem)
	}

	sort.Strings(needs)
	if strings.Join(needs, ",") != strings.Join(wantNeeds, ",") {
		t.Errorf("%s: %q now depends on %v, not on %v — a dependency dropped from `needs:` also disappears from the checks above, which read that same list, so the change has to be judged here by hand",
			workflowPath, publishJob, needs, wantNeeds)
	}
}

// gateProblems is the judgement itself, over a condition and the dependency
// list it is supposed to gate on. It is separated from the workflow it reads so
// that the rules can be proven against conditions this repository does not
// contain — a guard whose only input is the tree it passes on has never been
// shown to fail for the right reason.
func gateProblems(condition string, needs []string) []string {
	var problems []string

	if !strings.Contains(condition, "!cancelled()") {
		problems = append(problems, "the `if` holds no status-check function ("+condition+"), so GitHub supplies the implicit `success()` — which reads the whole ancestor graph and is false on every push to `main`, skipping the publish inside a green run")
	}

	for _, job := range needs {
		clause := "needs." + job + ".result == 'success'"
		if !strings.Contains(condition, clause) {
			problems = append(problems, "the `if` does not carry "+clause+", so a "+job+" that FAILED still publishes: `!cancelled()` suppresses the implicit `success()` for every dependency at once, and nothing else judges them")
		}
	}

	// The clauses above are substrings, and a substring survives being wrapped
	// in an `||` — `(needs.image-smoke.result == 'success' || <anything>)` reads
	// green above while gating on nothing. Requiring the condition to be a pure
	// conjunction apart from the two publishing events is what closes that: any
	// other `||` is refused here and judged by a reader.
	if strings.Contains(strings.Replace(condition, eventDisjunction, "<events>", 1), "||") {
		problems = append(problems, "the `if` holds an `||` beyond the two publishing events ("+condition+"), and a clause inside an `||` gates on nothing while still reading as present — write the gate as a conjunction, or judge this condition by hand")
	}

	return problems
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

	needs := parseNeeds(jobFieldLines(t, block, "needs"))
	if len(needs) == 0 {
		t.Fatalf("%s: %q lists no dependencies, so it is gated on nothing at all", workflowPath, publishJob)
	}
	return needs
}

// parseNeeds reads a dependency list in any of the three spellings YAML allows
// for it. All three are the same list to GitHub, so all three have to be the
// same list here: a reader that knew only the block sequence would report "no
// dependencies at all" over a workflow that in fact declares five, and the
// per-dependency checks would never run.
func parseNeeds(lines []string) []string {
	var needs []string

	if inline := strings.TrimSpace(strings.Join(lines, " ")); strings.HasPrefix(inline, "[") {
		for _, item := range strings.Split(strings.Trim(inline, "[]"), ",") {
			if name := strings.TrimSpace(item); plainName.MatchString(name) {
				needs = append(needs, name)
			}
		}
		return needs
	}

	for _, line := range lines {
		if match := needsEntry.FindStringSubmatch(line); match != nil {
			needs = append(needs, match[1])
		}
	}
	if len(needs) == 0 && len(lines) == 1 && plainName.MatchString(lines[0]) {
		// `needs: one-job`, the single-dependency spelling.
		return []string{lines[0]}
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

// goodCondition rebuilds the shape the workflow declares out of wantNeeds, so
// the fixtures below cannot drift away from the dependency set the guard is
// written around.
func goodCondition() string {
	clauses := []string{"!cancelled()", eventDisjunction, "github.ref == 'refs/heads/main'"}
	for _, job := range wantNeeds {
		clauses = append(clauses, "needs."+job+".result == 'success'")
	}
	return "${{ " + strings.Join(clauses, " && ") + " }}"
}

// TestGateProblemsRefusesEveryShapeThatReadsGreenWhileGatingOnNothing proves the
// rules against conditions this repository does not contain. The workflow test
// above can only ever show that today's condition passes; these fixtures are
// what show the guard refuses the ones it exists to refuse — including the
// `||` wrap, which every substring check in it would otherwise walk straight
// past.
func TestGateProblemsRefusesEveryShapeThatReadsGreenWhileGatingOnNothing(t *testing.T) {
	good := goodCondition()

	for _, testCase := range []struct {
		name      string
		condition string
		want      int
	}{
		{"the shape the workflow declares", good, 0},
		{
			"no status function, so GitHub adds the implicit success()",
			strings.Replace(good, "!cancelled() && ", "", 1),
			1,
		},
		{
			"one dependency unjudged",
			strings.Replace(good, " && needs.image-smoke.result == 'success'", "", 1),
			1,
		},
		{
			"a clause wrapped in an `||`, which leaves the substring in place",
			strings.Replace(good, "needs.image-smoke.result == 'success'",
				"(needs.image-smoke.result == 'success' || github.event_name == 'workflow_dispatch')", 1),
			1,
		},
		{
			"the whole conjunction bypassed by an actor check",
			strings.Replace(good, "${{ ", "${{ github.actor == 'someone' || ", 1),
			1,
		},
		{
			"the event disjunction respelled, which this guard refuses to read",
			strings.Replace(good, eventDisjunction, "(github.event_name=='push'||github.event_name=='workflow_dispatch')", 1),
			1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := gateProblems(testCase.condition, wantNeeds)
			if len(got) != testCase.want {
				t.Fatalf("got %d problems %q, want %d", len(got), got, testCase.want)
			}
		})
	}
}

// TestParseNeedsReadsEveryYAMLSpellingOfADependencyList holds the reader to all
// three spellings. A reader that knew only one of them would report the gate as
// depending on nothing over a reformatted but identical workflow, and skip
// every per-dependency check on the way.
func TestParseNeedsReadsEveryYAMLSpellingOfADependencyList(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"block sequence", []string{"", "- test", "- image-smoke"}, "test,image-smoke"},
		{"flow sequence", []string{"[test, image-smoke]"}, "test,image-smoke"},
		{"flow sequence folded over two lines", []string{"[test,", "image-smoke]"}, "test,image-smoke"},
		{"a single dependency as a scalar", []string{"test"}, "test"},
		{"an empty list", []string{""}, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := strings.Join(parseNeeds(testCase.lines), ","); got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
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
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
