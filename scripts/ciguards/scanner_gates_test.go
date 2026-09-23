package ciguards

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const (
	securityWorkflow = ".github/workflows/security.yml"
	codeqlWorkflow   = ".github/workflows/codeql.yml"
)

// ---------------------------------------------------------------------------
// A `changes`-gated scanner job must not fail open when `changes` itself is
// cancelled or fails.
// ---------------------------------------------------------------------------

// changesGatedJobs names every job, in every workflow, whose `needs:` names a
// `changes` job. The checks run over the set enumerated from the workflows
// (jobsThatNeedChanges), not over this list, so a job added later is judged
// before anyone writes a row for it; TestEveryJobThatNeedsChangesIsPinned
// holds the two to each other both ways, so a job the enumeration stops
// seeing — a `needs:` form the parser no longer reads — fails by name.
var changesGatedJobs = []struct {
	workflow string
	job      string
}{
	{ciWorkflow, "test-go-shard"},
	{ciWorkflow, "test-go-rest"},
	{ciWorkflow, "test-go-analysis"},
	{ciWorkflow, "test-frontend"},
	{ciWorkflow, "race-services"},
	{ciWorkflow, "race-rest"},
	{ciWorkflow, "e2e-shard"},
	{ciWorkflow, "e2e-postgres-smoke"},
	{ciWorkflow, "e2e-cross-browser"},
	{ciWorkflow, "image-smoke"},
	{securityWorkflow, "gosec"},
	{securityWorkflow, "govulncheck"},
	{securityWorkflow, "trivy-fs"},
	{securityWorkflow, "trivy-image"},
	{codeqlWorkflow, "analyze"},
}

// JobIfSurvivesACancelledOrFailedChanges refuses the job-level `if:` of a job
// that needs `changes` unless it carries `!cancelled()`; condition is "" for
// a job with no `if:`. Without it, GitHub Actions attaches an implicit
// success()-of-needs predicate — to an `if:` with no status-check function of
// its own, and to a job with no `if:` at all: a FAILED `changes` job then
// SKIPS this job rather than running it, and a skipped job is a SATISFIED
// required check, directly or through a gate that accepts `skipped`.
func JobIfSurvivesACancelledOrFailedChanges(condition string) error {
	if condition == "" {
		return fmt.Errorf("no job-level `if:` — the implicit success() skips this job when `changes` fails, into a satisfied required check; add `if: ${{ !cancelled() }}`")
	}
	if !strings.Contains(condition, "!cancelled()") {
		return fmt.Errorf("`if:` (%s) has no `!cancelled()` — a failed or cancelled `changes` job skips this job into a satisfied required check instead of running it", condition)
	}
	if m := changesStatusRead.FindString(condition); m != "" {
		return fmt.Errorf("`if:` (%s) reads %s beside `!cancelled()` — that turns false once `changes` fails, and the job still skips into a satisfied required check", condition, m)
	}
	return nil
}

// changesStatusRead matches a read of a dependency's status inside a job
// condition: `!cancelled()` survives a failed `changes` only while nothing
// conjoined to it asks whether `changes` succeeded.
var changesStatusRead = regexp.MustCompile(`\b(success|failure)\(\)|needs(\.changes|\[\s*['"]changes['"]\s*\]|\.\*)\.result`)

// jobIfValue is a job's own single-line `if:` value, or "" when it has none.
func jobIfValue(block string) string {
	if m := jobIfLine.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

// gatedJob is one job that needs `changes`, with its block.
type gatedJob struct {
	workflow, job, block string
}

// jobsThatNeedChanges enumerates, from every workflow file, each job whose
// `needs:` names `changes`.
func jobsThatNeedChanges(t *testing.T) []gatedJob {
	t.Helper()
	var jobs []gatedJob
	for _, wf := range allWorkflowFiles(t) {
		for _, header := range workflowfile.JobHeaders(t, wf, workflowfile.Read(t, wf)) {
			name := strings.TrimSuffix(strings.TrimSpace(header), ":")
			if name != "changes" && jobNeedsChanges(t, wf, name) {
				jobs = append(jobs, gatedJob{wf, name, workflowfile.Job(t, wf, name)})
			}
		}
	}
	return jobs
}

// changesOutputComparison matches a comparison against one of `changes`'
// outputs wherever it appears in a job — its own `if:`, or a matrix job's
// step-level `env:` expression — so one rule reads both.
var changesOutputComparison = regexp.MustCompile(`needs\.changes\.outputs\.[A-Za-z_]+\s*(==|!=)\s*'(true|false)'`)

// ChangesOutputComparisonsAreFailSafe refuses any comparison against a
// `changes` output that is not `!= 'false'` — in particular `== 'true'`,
// which reads an ABSENT or empty output (a crashed `changes` job, an output
// renamed on one side of an edit) as "do not run". A job that reads no
// `changes` output at all is refused too: it would wait on `changes` for
// ordering alone, and it tells this scan it is broken.
func ChangesOutputComparisonsAreFailSafe(content string) error {
	if !strings.Contains(content, "needs.changes.outputs.") {
		return fmt.Errorf("reads no `changes` output — either the job needs `changes` for ordering alone, or this scan no longer sees how it reads one")
	}
	matches := changesOutputComparison.FindAllStringSubmatch(content, -1)
	var offending []string
	for _, m := range matches {
		if m[1] != "!=" || m[2] != "false" {
			offending = append(offending, m[0])
		}
	}
	if len(offending) > 0 {
		return fmt.Errorf("%d comparison(s) against a `changes` output are not `!= 'false'`: %v", len(offending), offending)
	}
	return nil
}

// negatedFalseTerm matches one matrix leg's term in codeql.yml's
// RUN_THIS_LANGUAGE: a NEGATED equals-false, which keeps a matrix leg outside
// the named languages defaulting to "run".
var negatedFalseTerm = regexp.MustCompile(`!\(matrix\.language == '[a-z-]+' && needs\.changes\.outputs\.[A-Za-z_]+ == 'false'\)`)

// RunThisLanguageNeverSkipsAnUnlistedLanguage refuses codeql.yml's
// RUN_THIS_LANGUAGE unless it is built from exactly one negated-false term
// per matrix leg (three today) and never compares a `changes` output against
// 'true' — an OR of per-language positives is fail-CLOSED for a leg added
// later, since none of the named terms would match it.
func RunThisLanguageNeverSkipsAnUnlistedLanguage(content string) error {
	// Scoped to needs.changes.outputs.* comparisons: the steps' own
	// env.RUN_THIS_LANGUAGE == 'true' guards compare the already-resolved
	// string and are the correct shape for a step-level gate.
	for _, m := range changesOutputComparison.FindAllStringSubmatch(content, -1) {
		if m[2] == "true" {
			return fmt.Errorf("RUN_THIS_LANGUAGE compares a `changes` output against 'true' (%q) — that reads an absent/empty output as skip", m[0])
		}
	}
	terms := negatedFalseTerm.FindAllString(content, -1)
	if len(terms) != 3 {
		return fmt.Errorf("found %d negated-false term(s) in RUN_THIS_LANGUAGE, want 3 (one per matrix leg — go, javascript-typescript, actions): %v", len(terms), terms)
	}
	return nil
}

func TestEveryJobThatNeedsChangesSurvivesACancelledOrFailedChanges(t *testing.T) {
	for _, j := range jobsThatNeedChanges(t) {
		if err := JobIfSurvivesACancelledOrFailedChanges(jobIfValue(j.block)); err != nil {
			t.Errorf("%s %s: %v", j.workflow, j.job, err)
		}
		check := ChangesOutputComparisonsAreFailSafe
		if j.workflow == codeqlWorkflow && j.job == "analyze" {
			check = RunThisLanguageNeverSkipsAnUnlistedLanguage
		}
		if err := check(j.block); err != nil {
			t.Errorf("%s %s: %v", j.workflow, j.job, err)
		}
	}
}

func TestJobIfSurvivesACancelledOrFailedChangesRefusesAMissingIf(t *testing.T) {
	if err := JobIfSurvivesACancelledOrFailedChanges(jobIfValue("    needs:\n      - changes\n    steps:\n        if: ${{ !cancelled() }}\n")); err == nil {
		t.Fatal("a job with no job-level `if:` (only a step's) was accepted")
	}
}

func TestJobIfSurvivesACancelledOrFailedChangesRefusesThePreFixCondition(t *testing.T) {
	if err := JobIfSurvivesACancelledOrFailedChanges("needs.changes.outputs.run_go != 'false'"); err == nil {
		t.Fatal("a condition with no !cancelled() was accepted")
	}
}

func TestJobIfSurvivesACancelledOrFailedChangesRefusesAStatusConjunct(t *testing.T) {
	for _, condition := range []string{
		"${{ !cancelled() && needs.changes.result == 'success' }}",
		"${{ !cancelled() && needs['changes'].result != 'failure' }}",
		"${{ !cancelled() && !contains(needs.*.result, 'failure') }}",
		"${{ !cancelled() && success() }}",
		"${{ !cancelled() && !failure() }}",
	} {
		if err := JobIfSurvivesACancelledOrFailedChanges(condition); err == nil {
			t.Errorf("%s was accepted, though it skips the job once `changes` fails", condition)
		}
	}
	if err := JobIfSurvivesACancelledOrFailedChanges("${{ !cancelled() && needs.changes.outputs.run_e2e != 'false' && github.event_name == 'push' }}"); err != nil {
		t.Errorf("a fail-safe output read beside !cancelled() was refused: %v", err)
	}
}

func TestChangesOutputComparisonsAreFailSafeRefusesAnEqualsTrueComparison(t *testing.T) {
	if err := ChangesOutputComparisonsAreFailSafe("if: needs.changes.outputs.run_go == 'true'"); err == nil {
		t.Fatal("an == 'true' comparison was accepted")
	}
}

func TestRunThisLanguageNeverSkipsAnUnlistedLanguageRefusesTheOldORShape(t *testing.T) {
	old := "(matrix.language == 'go' && needs.changes.outputs.run_go != 'false') || " +
		"(matrix.language == 'javascript-typescript' && needs.changes.outputs.run_js != 'false') || " +
		"(matrix.language == 'actions' && needs.changes.outputs.run_actions != 'false')"
	if err := RunThisLanguageNeverSkipsAnUnlistedLanguage(old); err == nil {
		t.Fatal("the OR-of-positives shape (fail-closed for an unlisted matrix leg) was accepted")
	}
}

// jobNeedsKey is a job's own `needs:` key and the rest of its line. `[ \t]*`,
// never `\s*`: the latter crosses the newline of an empty value and reads the
// block sequence's first item as a scalar.
var jobNeedsKey = regexp.MustCompile(`(?m)^    needs:[ \t]*(.*)$`)

// yamlScalar strips a trailing comment and one pair of matching quotes.
func yamlScalar(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return s
}

// JobNeeds returns the job ids a job block's `needs:` names, in each of the
// three forms YAML writes a list in: a scalar (`needs: changes`), a flow
// sequence (`needs: [changes, build]`, quoted or not) and a block sequence
// (one `- changes` per line below the key). A job with no `needs:` returns
// nil. A shape outside those three — a flow sequence spanning lines, an item
// that is not a scalar — is refused rather than read as some other list.
func JobNeeds(block string) ([]string, error) {
	loc := jobNeedsKey.FindStringSubmatchIndex(block)
	if loc == nil {
		return nil, nil
	}
	inline := yamlScalar(block[loc[2]:loc[3]])
	switch {
	case strings.HasPrefix(inline, "["):
		if !strings.HasSuffix(inline, "]") {
			return nil, fmt.Errorf("`needs: %s`: a flow sequence spanning lines is not modelled", inline)
		}
		var needs []string
		for _, item := range strings.Split(inline[1:len(inline)-1], ",") {
			if item = yamlScalar(item); item == "" {
				return nil, fmt.Errorf("`needs: %s`: an empty item", inline)
			}
			needs = append(needs, item)
		}
		return needs, nil
	case inline != "":
		return []string{inline}, nil
	}
	var needs []string
	for _, line := range strings.Split(block[loc[1]:], "\n")[1:] {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(trimmed)
		if indent < 4 || (indent == 4 && !strings.HasPrefix(trimmed, "- ")) {
			break
		}
		if !strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("`needs:` block line %q is not a `- item`", line)
		}
		item := yamlScalar(trimmed[2:])
		if item == "" || strings.ContainsAny(item, "[]{}:") {
			return nil, fmt.Errorf("`needs:` block item %q is not a job id", line)
		}
		needs = append(needs, item)
	}
	if len(needs) == 0 {
		return nil, fmt.Errorf("`needs:` has neither a value nor a block sequence below it")
	}
	return needs, nil
}

func TestJobNeedsReadsEveryYAMLListForm(t *testing.T) {
	for block, want := range map[string]string{
		"    needs: changes\n    if: x\n":                          "changes",
		"    needs: 'changes'  # quoted\n":                         "changes",
		"    needs: [changes]\n":                                   "changes",
		"    needs: [build, changes]\n":                            "build,changes",
		"    needs: [ \"build\" , 'changes' ]\n":                   "build,changes",
		"    needs:\n      - build\n      - changes\n    if: x\n":  "build,changes",
		"    needs:\n    - changes\n    - build\n    runs-on: x\n": "changes,build",
		"    runs-on: x\n":                                         "",
		"    needs:   \n      # a comment\n      - changes # why\n\n    steps:\n      - name: x\n": "changes",
	} {
		got, err := JobNeeds(block)
		if err != nil {
			t.Errorf("%q: %v", block, err)
			continue
		}
		if strings.Join(got, ",") != want {
			t.Errorf("%q: needs = %v, want %q", block, got, want)
		}
	}
	for _, refused := range []string{
		"    needs: [build,\n      changes]\n",
		"    needs:\n    runs-on: x\n",
		"    needs:\n      - [changes]\n",
		"    needs: [build, , changes]\n",
	} {
		if got, err := JobNeeds(refused); err == nil {
			t.Errorf("%q was read as %v instead of refused", refused, got)
		}
	}
}

// jobNeedsChanges reports whether a job's `needs:` names `changes`.
func jobNeedsChanges(t *testing.T, workflow, job string) bool {
	t.Helper()
	needs, err := JobNeeds(workflowfile.Job(t, workflow, job))
	if err != nil {
		t.Fatalf("%s %s: %v", workflow, job, err)
	}
	return contains(needs, "changes")
}

func TestEveryJobThatNeedsChangesIsPinned(t *testing.T) {
	pinned := map[string]bool{}
	for _, j := range changesGatedJobs {
		pinned[j.workflow+"::"+j.job] = true
	}

	seen := map[string]bool{}
	var missing []string
	for _, j := range jobsThatNeedChanges(t) {
		key := j.workflow + "::" + j.job
		seen[key] = true
		if !pinned[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("job(s) that need `changes` are not in changesGatedJobs: %v — once TestEveryJobThatNeedsChangesSurvivesACancelledOrFailedChanges passes for them, add each by name", missing)
	}
	for _, j := range changesGatedJobs {
		if key := j.workflow + "::" + j.job; !seen[key] {
			t.Fatalf("pinned job %s no longer declares `needs: changes` — changesGatedJobs is stale", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Every caller of the diff action: gated to the events with a base, and
// executed by the harness.
// ---------------------------------------------------------------------------

// detectCasesByWorkflow is the behaviour table each caller of the diff action
// is executed against.
var detectCasesByWorkflow = map[string][]detectCase{
	ciWorkflow: detectCases,
	securityWorkflow: {
		{"docs only", "pull_request", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"non-ASCII Go path", "pull_request", []string{"internal/api/évil.go"},
			map[string]string{"run_go": "true", "run_trivyfs": "false", "run_trivyimage": "true"}},
		{"tab in a Go path", "pull_request", []string{"internal/api/t\tx.go"},
			map[string]string{"run_go": "true", "run_trivyfs": "false", "run_trivyimage": "true"}},
		{"newline in a docs path", "pull_request", []string{"docs/n\nx.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"nested lockfile", "pull_request", []string{"web/workspace/nested/package-lock.json"},
			map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"web/embed.go", "pull_request", []string{"web/embed.go"},
			map[string]string{"run_go": "true", "run_trivyimage": "true"}},
		{"web/src only", "pull_request", []string{"web/src/js/a.js"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"Go test file only", "pull_request", []string{"internal/x/a_test.go"},
			map[string]string{"run_go": "true", "run_trivyimage": "false"}},
		{"this workflow", "pull_request", []string{securityWorkflow},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"the diff action", "pull_request", []string{diffAction + "/action.yml"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"uv.lock", "pull_request", []string{"tools/py/uv.lock"},
			map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "false"}},
		{"nested gradle.lockfile", "pull_request", []string{"android/app/gradle.lockfile"},
			map[string]string{"run_trivyfs": "true"}},
		{"packages.lock.json", "pull_request", []string{"tools/net/packages.lock.json"},
			map[string]string{"run_trivyfs": "true"}},
		{"pubspec.lock", "pull_request", []string{"mobile/pubspec.lock"},
			map[string]string{"run_trivyfs": "true"}},
		{"the pull-retry action", "pull_request", []string{".github/actions/docker-pull-retry/action.yml"},
			map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"merge_group, docs only", "merge_group", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_trivyfs": "false", "run_trivyimage": "false"}},
		{"push", "push", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"schedule", "schedule", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
		{"workflow_dispatch", "workflow_dispatch", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_trivyfs": "true", "run_trivyimage": "true"}},
	},
	codeqlWorkflow: {
		{"docs only", "pull_request", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_js": "false", "run_actions": "false"}},
		{"non-ASCII Go path", "pull_request", []string{"internal/api/évil.go"},
			map[string]string{"run_go": "true", "run_js": "false", "run_actions": "false"}},
		{"quote in a JS path", "pull_request", []string{`web/src/js/q"x.ts`},
			map[string]string{"run_go": "false", "run_js": "true", "run_actions": "false"}},
		{"backslash in a JS path", "pull_request", []string{`web/src/js/b\x.ts`},
			map[string]string{"run_go": "false", "run_js": "true", "run_actions": "false"}},
		{"template html", "pull_request", []string{"internal/templates/x.html"},
			map[string]string{"run_go": "false", "run_js": "true", "run_actions": "false"}},
		{"nested package.json", "pull_request", []string{"web/tools/package.json"},
			map[string]string{"run_js": "true"}},
		{"nested package-lock.json", "pull_request", []string{"e2e/fixtures/package-lock.json"},
			map[string]string{"run_js": "true"}},
		{"the diff action", "pull_request", []string{diffAction + "/action.yml"},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
		{"this workflow", "pull_request", []string{codeqlWorkflow},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
		{"merge_group, docs only", "merge_group", []string{"docs/x.md"},
			map[string]string{"run_go": "false", "run_js": "false", "run_actions": "false"}},
		{"push", "push", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
		{"schedule", "schedule", []string{"docs/x.md"},
			map[string]string{"run_go": "true", "run_js": "true", "run_actions": "true"}},
	},
}

// jobSteps cuts a job block into its steps, each re-indented so the key its
// item opens on sits at 8 spaces like the rest of its keys.
func jobSteps(block string) []string {
	start := strings.Index(block, "\n    steps:\n")
	if start < 0 {
		return nil
	}
	var steps []string
	for _, step := range stepHeader.Split(block[start:], -1)[1:] {
		steps = append(steps, "        "+step)
	}
	return steps
}

// DiffCallerStepsAreGated returns one problem per step of a `changes` job —
// its checkout, and its call of the diff action — that does not carry
// `if: diffEvents`, or that is missing.
func DiffCallerStepsAreGated(block string) []string {
	var problems []string
	steps := jobSteps(block)
	for _, needle := range []string{"uses: ./" + diffAction + "\n", "uses: actions/checkout@"} {
		found := false
		for _, step := range steps {
			if !strings.Contains(step, needle) {
				continue
			}
			found = true
			if !strings.Contains(step, "        if: "+diffEvents+"\n") {
				problems = append(problems, fmt.Sprintf("the step with %q does not carry `if: %s`", strings.TrimSpace(needle), diffEvents))
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("no step with %q", strings.TrimSpace(needle)))
		}
	}
	return problems
}

func TestDiffCallerStepsAreGatedSplitsOnEveryStepItem(t *testing.T) {
	checkout := "      - name: Checkout\n        if: " + diffEvents + "\n        uses: actions/checkout@abc\n"
	detect := "      - name: Detect\n        id: detect\n        run: |\n          true\n"
	for opening, list := range map[string]string{
		"id":   "      - id: diff\n        uses: ./" + diffAction + "\n",
		"uses": "      - uses: ./" + diffAction + "\n        id: diff\n",
	} {
		block := "  changes:\n    steps:\n" + checkout + list + detect
		if problems := DiffCallerStepsAreGated(block); len(problems) != 1 {
			t.Errorf("a list step opening on `- %s:` with no `if:` gave %v — it was read as part of the gated checkout above it", opening, problems)
		}
		gated := strings.Replace(list, "\n", "\n        if: "+diffEvents+"\n", 1)
		if problems := DiffCallerStepsAreGated("  changes:\n    steps:\n" + checkout + gated + detect); len(problems) != 0 {
			t.Errorf("a gated list step opening on `- %s:` was refused: %v", opening, problems)
		}
	}
	opensOnIf := "      - if: " + diffEvents + "\n        uses: ./" + diffAction + "\n"
	if problems := DiffCallerStepsAreGated("  changes:\n    steps:\n" + checkout + opensOnIf); len(problems) != 0 {
		t.Errorf("a list step opening on its own `- if:` was refused: %v", problems)
	}
}

// TestEveryCallerOfTheDiffActionIsGatedAndExecuted enumerates the action's
// callers from the workflows themselves: each must call it from its `changes`
// job, put diffEvents on that job's checkout and on the list step — the deep
// fetch is paid only where a base exists — and have a behaviour table above.
func TestEveryCallerOfTheDiffActionIsGatedAndExecuted(t *testing.T) {
	uses := "uses: ./" + diffAction + "\n"

	callers := map[string]bool{}
	for _, wf := range allWorkflowFiles(t) {
		content := workflowfile.Read(t, wf)
		for _, header := range workflowfile.JobHeaders(t, wf, content) {
			job := strings.TrimSuffix(strings.TrimSpace(header), ":")
			block := workflowfile.Job(t, wf, job)
			if !strings.Contains(block, uses) {
				continue
			}
			if job != "changes" {
				t.Errorf("%s: job %q calls %s; only a `changes` job's list step is executed by this package", wf, job, diffAction)
				continue
			}
			callers[wf] = true
			for _, problem := range DiffCallerStepsAreGated(block) {
				t.Errorf("%s changes: %s", wf, problem)
			}
		}
	}
	if len(callers) == 0 {
		t.Fatalf("no workflow calls %s — this sweep would judge nothing", diffAction)
	}
	for wf := range callers {
		if len(detectCasesByWorkflow[wf]) == 0 {
			t.Errorf("%s calls %s but has no behaviour table in detectCasesByWorkflow", wf, diffAction)
		}
	}
	for wf := range detectCasesByWorkflow {
		if !callers[wf] {
			t.Errorf("detectCasesByWorkflow has a table for %s, which no longer calls %s", wf, diffAction)
		}
	}
}

func TestScannerDetectStepsDecideEachScannerFromTheDiff(t *testing.T) {
	for _, wf := range []string{securityWorkflow, codeqlWorkflow} {
		for _, c := range detectCasesByWorkflow[wf] {
			t.Run(path.Base(wf)+"/"+c.name, func(t *testing.T) {
				got := detectRun{workflow: wf, event: c.event, files: c.files, queueBase: queueBaseReal}.run(t)
				for k, v := range c.want {
					if got[k] != v {
						t.Errorf("%s = %q, want %q (all outputs: %v)", k, got[k], v, got)
					}
				}
			})
		}
	}
}

// TestTrivyFSRunsForAnAddedBinary drives security.yml's content fallback: a
// changed blob git classifies as binary forces trivy-fs whatever its path.
func TestTrivyFSRunsForAnAddedBinary(t *testing.T) {
	got := detectRun{workflow: securityWorkflow, event: "pull_request", binaries: []string{"some/dir/asset.bin"}, queueBase: queueBaseReal}.run(t)
	if got["run_trivyfs"] != "true" {
		t.Fatalf("run_trivyfs = %q for a binary-only diff, want \"true\" (all outputs: %v)", got["run_trivyfs"], got)
	}
}

// TestTrivyFSRunsWhenTheBinaryProbeFails fails only the `--numstat` call, on a
// diff no path rule scans: the list step still produces the file list, so
// gosec and trivy-image stay scoped out, and trivy-fs runs because which blobs
// are binary is unknown.
func TestTrivyFSRunsWhenTheBinaryProbeFails(t *testing.T) {
	got := detectRun{workflow: securityWorkflow, event: "pull_request", files: []string{"docs/notes.md"}, queueBase: queueBaseReal, failGit: "--numstat"}.run(t)
	want := map[string]string{"run_go": "false", "run_trivyfs": "true", "run_trivyimage": "false"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q when git diff --numstat fails (all outputs: %v)", k, got[k], v, got)
		}
	}
}

// listSizeOutputs names, per caller of the diff action, one output a Go file
// sets to "true" — the probe TestTheFileListReachesEveryDetectStepWhateverItsSize
// reads at the end of an oversized list.
var listSizeOutputs = map[string]string{
	ciWorkflow:       "run_core",
	securityWorkflow: "run_go",
	codeqlWorkflow:   "run_go",
}

// TestTheFileListReachesEveryDetectStepWhateverItsSize hands every caller a
// list longer than one environment string may be (128 KiB on Linux, 32767
// characters on Windows). The list crosses as a file, so the detect step
// still starts, and the one Go file at its end still decides.
func TestTheFileListReachesEveryDetectStepWhateverItsSize(t *testing.T) {
	var files []string
	dir := "docs/" + strings.Repeat("d", 90) + "/"
	for i := range 1500 {
		files = append(files, fmt.Sprintf("%sf%04d.md", dir, i))
	}
	files = append(files, "internal/x/a.go")
	if size := len(strings.Join(files, "\n")); size <= 128<<10 {
		t.Fatalf("the list is %d bytes, not over the 128 KiB a Linux env string holds — this test proves nothing", size)
	}
	for wf := range detectCasesByWorkflow {
		output, ok := listSizeOutputs[wf]
		if !ok {
			t.Errorf("%s calls the diff action but has no probe output in listSizeOutputs", wf)
			continue
		}
		t.Run(path.Base(wf), func(t *testing.T) {
			got := detectRun{workflow: wf, event: "pull_request", files: files, queueBase: queueBaseReal}.run(t)
			if got[output] != "true" {
				t.Errorf("%s = %q for a %d-file list ending in a Go file, want \"true\"", output, got[output], len(files))
			}
		})
	}
}

// TestAnUnreadableFileListRunsEverything hands every caller's detect step a
// list path that names no file: it must read as an empty list, never fail
// the job and never narrow a lane.
func TestAnUnreadableFileListRunsEverything(t *testing.T) {
	list := `echo "files-path=$RUNNER_TEMP/absent/changed-files" >> "$GITHUB_OUTPUT"` + "\n"
	for wf := range detectCasesByWorkflow {
		t.Run(path.Base(wf), func(t *testing.T) {
			got := detectRun{workflow: wf, list: list, event: "pull_request", files: []string{"docs/x.md"}, queueBase: queueBaseReal}.run(t)
			judged := 0
			for k, v := range got {
				if !strings.HasPrefix(k, "run_") {
					continue
				}
				judged++
				if v != "true" {
					t.Errorf("%s = %q with an unreadable list, want \"true\" (all outputs: %v)", k, v, got)
				}
			}
			if judged == 0 {
				t.Fatalf("the detect step wrote no run_* output: %v", got)
			}
		})
	}
}

// repoGit runs git in the repository under test, in the hermetic environment.
func repoGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = hermeticGitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// trackedFiles lists the repository's tracked paths matching pathspecs.
func trackedFiles(t *testing.T, pathspecs ...string) []string {
	t.Helper()
	var files []string
	for _, f := range strings.Split(repoGit(t, append([]string{"ls-files", "-z", "--"}, pathspecs...)...), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// TestCodeQLJavaScriptGateFiresForEveryTrackedNpmManifest runs each npm
// manifest and lockfile the repository tracks, at any depth, through
// codeql.yml's detect step alone: every one shapes what the JavaScript
// analysis extracts.
func TestCodeQLJavaScriptGateFiresForEveryTrackedNpmManifest(t *testing.T) {
	manifests := trackedFiles(t, "*package.json", "*package-lock.json", "*npm-shrinkwrap.json")
	if !contains(manifests, "package.json") {
		t.Fatalf("the root package.json is not among the tracked manifests %v — the listing is broken", manifests)
	}
	for _, m := range manifests {
		got := detectRun{workflow: codeqlWorkflow, event: "pull_request", files: []string{m}, queueBase: queueBaseReal}.run(t)
		if got["run_js"] != "true" {
			t.Errorf("a diff touching only %s leaves run_js = %q", m, got["run_js"])
		}
	}
}

// ---------------------------------------------------------------------------
// trivy-image's gate covers every file the image is built from, as the
// Dockerfile and .dockerignore define that set.
// ---------------------------------------------------------------------------

var (
	dockerCopyInstruction = regexp.MustCompile(`(?i)^(COPY|ADD)\s+(.*)$`)
	dockerRunInstruction  = regexp.MustCompile(`(?i)^RUN\s+(.*)$`)
)

// runMountReadsContext reports whether one RUN --mount's options bind the
// build context: a bind mount (the default type) with no `from=` mounts the
// context itself, whatever its `source=` narrows it to.
func runMountReadsContext(options string) bool {
	mountType, fromStage := "bind", false
	for _, option := range strings.Split(options, ",") {
		key, value, _ := strings.Cut(option, "=")
		switch strings.ToLower(key) {
		case "type":
			mountType = strings.ToLower(value)
		case "from":
			fromStage = true
		}
	}
	return mountType == "bind" && !fromStage
}

// DockerContextSources returns every build-context path a COPY or ADD in
// dockerfile reads. An instruction carrying --from reads another stage, not
// the context, and is skipped. Forms this parser does not model — the JSON
// array form, heredocs, wildcards, URLs, and a RUN --mount that binds the
// context — are refused rather than guessed at.
func DockerContextSources(dockerfile string) ([]string, error) {
	joined := strings.ReplaceAll(strings.ReplaceAll(dockerfile, "\r\n", "\n"), "\\\n", " ")
	var sources []string
	for _, raw := range strings.Split(joined, "\n") {
		if run := dockerRunInstruction.FindStringSubmatch(strings.TrimSpace(raw)); run != nil {
			for _, field := range strings.Fields(run[1]) {
				if !strings.HasPrefix(field, "--") {
					break
				}
				if options, ok := strings.CutPrefix(field, "--mount="); ok && runMountReadsContext(options) {
					return nil, fmt.Errorf("%q: a RUN --mount binding the build context is not modelled", raw)
				}
			}
			continue
		}
		m := dockerCopyInstruction.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		if strings.Contains(m[2], "<<") {
			return nil, fmt.Errorf("%q: heredoc COPY/ADD is not modelled", raw)
		}
		var args []string
		fromStage := false
		for _, field := range strings.Fields(m[2]) {
			if strings.HasPrefix(field, "--") {
				fromStage = fromStage || strings.HasPrefix(field, "--from=")
				continue
			}
			args = append(args, field)
		}
		if fromStage {
			continue
		}
		if len(args) > 0 && strings.HasPrefix(args[0], "[") {
			return nil, fmt.Errorf("%q: the JSON form of COPY/ADD is not modelled", raw)
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("%q: no source and destination", raw)
		}
		for _, src := range args[:len(args)-1] {
			if strings.ContainsAny(src, "*?[") || strings.Contains(src, "://") {
				return nil, fmt.Errorf("%q: source %q is a wildcard or URL, which is not modelled", raw, src)
			}
			sources = append(sources, path.Clean(strings.TrimPrefix(src, "/")))
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no COPY or ADD reads the build context")
	}
	return sources, nil
}

// dockerignoreRule is one .dockerignore line: a pattern matched against a
// context path or any of its parent directories, excluding it — or, for a
// `!` line, including it again. The last matching line wins.
type dockerignoreRule struct {
	pattern *regexp.Regexp
	exclude bool
}

// DockerignoreRules parses .dockerignore with Docker's matching rules: a
// pattern is anchored at the context root, `*` and `?` stay within one path
// segment, `**` spans any number of them. Character classes and escapes are
// refused rather than guessed at.
func DockerignoreRules(content string) ([]dockerignoreRule, error) {
	var rules []dockerignoreRule
	for _, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := dockerignoreRule{exclude: true}
		if strings.HasPrefix(line, "!") {
			rule.exclude = false
			line = strings.TrimSpace(line[1:])
		}
		line = path.Clean(strings.TrimPrefix(line, "/"))
		var re strings.Builder
		re.WriteString("^")
		for i := 0; i < len(line); i++ {
			switch c := line[i]; {
			case c == '*' && i+1 < len(line) && line[i+1] == '*':
				i++
				if i+1 < len(line) && line[i+1] == '/' {
					i++
					re.WriteString("(.*/)?")
				} else {
					re.WriteString(".*")
				}
			case c == '*':
				re.WriteString("[^/]*")
			case c == '?':
				re.WriteString("[^/]")
			case c == '[' || c == '\\':
				return nil, fmt.Errorf(".dockerignore line %q: character classes and escapes are not modelled", raw)
			default:
				re.WriteString(regexp.QuoteMeta(line[i : i+1]))
			}
		}
		re.WriteString("$")
		compiled, err := regexp.Compile(re.String())
		if err != nil {
			return nil, fmt.Errorf(".dockerignore line %q: %w", raw, err)
		}
		rule.pattern = compiled
		rules = append(rules, rule)
	}
	return rules, nil
}

// DockerignoreExcludes reports whether rules keep file out of the context.
func DockerignoreExcludes(rules []dockerignoreRule, file string) bool {
	excluded := false
	for _, rule := range rules {
		for p := file; ; p = path.Dir(p) {
			if rule.pattern.MatchString(p) {
				excluded = rule.exclude
				break
			}
			if !strings.Contains(p, "/") {
				break
			}
		}
	}
	return excluded
}

// ImageAffectingFiles is every tracked file a COPY source covers and
// .dockerignore leaves in the context.
func ImageAffectingFiles(tracked, sources []string, rules []dockerignoreRule) []string {
	var files []string
	for _, f := range tracked {
		covered := false
		for _, src := range sources {
			if src == "." || f == src || strings.HasPrefix(f, src+"/") {
				covered = true
				break
			}
		}
		if covered && !DockerignoreExcludes(rules, f) {
			files = append(files, f)
		}
	}
	return files
}

func TestDockerignoreRulesMatchAsDockerDoes(t *testing.T) {
	rules, err := DockerignoreRules("# comment\n*.pem\n**/*_test.go\nweb/src/\nsecrets/\n!secrets/keep.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		"a.pem":                  true,
		"certs/a.pem":            false, // `*` stays in the root segment
		"a_test.go":              true,  // `**/` spans zero segments too
		"internal/x/a_test.go":   true,
		"web/src/js/a.js":        true, // a directory pattern excludes its contents
		"web/srcx/a.js":          false,
		"web/embed.go":           false,
		"secrets/key":            true,
		"secrets/keep.txt":       false, // the later `!` line wins
		"internal/x/a_test.go.x": false,
	} {
		if got := DockerignoreExcludes(rules, file); got != want {
			t.Errorf("%s: excluded = %v, want %v", file, got, want)
		}
	}
	if _, err := DockerignoreRules("web/[a-z]*\n"); err == nil {
		t.Error("a character class was accepted instead of refused")
	}
}

func TestDockerContextSourcesSkipsStageCopiesAndRefusesUnmodelledForms(t *testing.T) {
	got, err := DockerContextSources("FROM x AS b\nCOPY go.mod go.sum ./\ncopy --chown=1:1 web \\\n  ./web\nCOPY --from=b /out/app /app\n# COPY docs ./docs\n" +
		"RUN --mount=type=cache,target=/root/.cache go build\nRUN --mount=type=bind,from=b,source=/out,target=/in true\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"go.mod", "go.sum", "web"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sources = %v, want %v", got, want)
	}
	for _, refused := range []string{
		`COPY ["a", "b"]`, "COPY web/*.go ./", "ADD https://example.invalid/x /x", "COPY <<EOF /x",
		"COPY go.mod ./\nRUN --mount=type=bind,source=go.sum,target=go.sum go mod download",
		"COPY go.mod ./\nrun --network=none --mount=target=/src make",
	} {
		if _, err := DockerContextSources(refused + "\n"); err == nil {
			t.Errorf("%q was accepted instead of refused", refused)
		}
	}
}

var imageBuildKey = regexp.MustCompile(`(?m)^          (context|file): (\S+)$`)

// imageBuildDefinition reads, off trivy-image's own build step, the Dockerfile
// it builds and the ignore file Docker applies to that build: the
// Dockerfile's own `<Dockerfile>.dockerignore` when one is tracked, which
// Docker then prefers, else the context root's .dockerignore.
func imageBuildDefinition(t *testing.T) (dockerfile, dockerignore string) {
	t.Helper()
	keys := map[string][]string{}
	for _, m := range imageBuildKey.FindAllStringSubmatch(workflowfile.Job(t, securityWorkflow, "trivy-image"), -1) {
		keys[m[1]] = append(keys[m[1]], m[2])
	}
	if len(keys["file"]) != 1 || len(keys["context"]) != 1 {
		t.Fatalf("trivy-image's build step names file %v and context %v, want one of each — it was reshaped, and the derived set would be read off the wrong build", keys["file"], keys["context"])
	}
	dockerfile = path.Clean(keys["file"][0])
	dockerignore = dockerfile + ".dockerignore"
	if len(trackedFiles(t, dockerignore)) == 0 {
		dockerignore = path.Join(keys["context"][0], ".dockerignore")
	}
	return dockerfile, dockerignore
}

// imageAffectingFiles derives the set from the repository's own Dockerfile,
// .dockerignore and git index, plus those two build-definition files
// themselves: an edit to either changes the image without touching a file
// the build copies.
func imageAffectingFiles(t *testing.T) []string {
	t.Helper()
	dockerfile, dockerignore := imageBuildDefinition(t)
	sources, err := DockerContextSources(workflowfile.Read(t, dockerfile))
	if err != nil {
		t.Fatalf("%s: %v", dockerfile, err)
	}
	rules, err := DockerignoreRules(workflowfile.Read(t, dockerignore))
	if err != nil {
		t.Fatalf("%s: %v", dockerignore, err)
	}
	return append(ImageAffectingFiles(trackedFiles(t), sources, rules), dockerfile, dockerignore)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var trivyImageGateLine = regexp.MustCompile(`(?m)^([ \t]*)trivyimage_changes=.*$`)

// TestTrivyImageGateFiresForEveryFileTheImageIsBuiltFrom runs security.yml's
// list and detect steps once over a diff holding every image-affecting file,
// with the detect step instrumented to record what its trivy-image gate
// selected. The gate filters line by line, so a file it selects here is one
// that fires it alone, and one it drops never would.
func TestTrivyImageGateFiresForEveryFileTheImageIsBuiltFrom(t *testing.T) {
	files := imageAffectingFiles(t)

	if !contains(files, "web/embed.go") {
		t.Fatalf("web/embed.go — compiled into the image from web/ — is not in the set derived from the Dockerfile and .dockerignore (%d files)", len(files))
	}
	webSrc := trackedFiles(t, "web/src/")
	if len(webSrc) == 0 {
		t.Fatal("no tracked file under web/src/ — the exclusion check below would judge nothing")
	}
	if contains(files, webSrc[0]) {
		t.Fatalf("%s is in the derived set, but .dockerignore keeps web/src/ out of the build context", webSrc[0])
	}

	script := detectStep(t, securityWorkflow).script
	loc := trivyImageGateLine.FindStringSubmatchIndex(script)
	if loc == nil {
		t.Fatal("no `trivyimage_changes=` assignment in security.yml's detect step — it was renamed or reshaped, and this guard would judge nothing")
	}
	probe := filepath.ToSlash(filepath.Join(t.TempDir(), "gate"))
	indent := script[loc[2]:loc[3]]
	instrumented := script[:loc[1]] + "\n" + indent + `printf '%s\n' "$trivyimage_changes" > '` + probe + `'` + script[loc[1]:]

	detectRun{workflow: securityWorkflow, detect: instrumented, event: "pull_request", files: files, queueBase: queueBaseReal}.run(t)

	recorded, err := os.ReadFile(filepath.FromSlash(probe))
	if err != nil {
		t.Fatalf("the instrumented gate recorded nothing: %v", err)
	}
	selected := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(recorded), "\r\n", "\n"), "\n") {
		selected[line] = true
	}
	var missed []string
	for _, f := range files {
		if !selected[f] {
			missed = append(missed, f)
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Fatalf("trivy-image's gate does not fire for %d file(s) the Dockerfile copies into the image: %v", len(missed), missed)
	}
}
