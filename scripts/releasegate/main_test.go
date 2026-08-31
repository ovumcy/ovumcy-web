// Package releasegate holds the release tag gate to the one thing it exists to
// assert: that the five checks `:latest` waits for were really run on the
// commit being tagged.
//
// `verify-release-tag` in .github/workflows/docker-image.yml is the only gate
// on the release path — a tag push starts no CI run of its own, and branch
// protection guards merges into `main`, not tags. It used to read the check
// suite whose `head_branch` is `main` and accept `success` or `skipped` from it
// for all five names alike. Both halves of that are wrong at once, and they
// contradict each other:
//
//   - it refused the merge queue's suite as untrustworthy, in a comment that
//     measured `image-smoke` reporting `skipped` there and `e2e-postgres-smoke`
//     reporting a `success` with every step gated off by `RUN_HEAVY`;
//   - and then it accepted `test`, `race` and `e2e` off the push suite, where
//     ci.yml's `changes` job clears `run_core` UNCONDITIONALLY on the grounds
//     that the queue has already run them. Measured at 4b3c01d7: 4 s, 2 s and
//     4 s with every lane beneath them `skipped`. A green with no work behind
//     it, on every push, for reasons that have nothing to do with the commit.
//
// So each of the five had a verdict read off a run whose own gate never looked
// at that commit's diff. What the gate asks now is the same question per lane,
// aimed at the run where the lane actually executes: `test`, `race` and `e2e`
// at the `merge_group` run, which this rebase queue produces on the
// byte-identical SHA; `e2e-postgres-smoke` and `image-smoke` at the push,
// release or dispatch run, which is what their own `RUN_HEAVY` selects.
//
// The tests below run the gate's REAL shell — extracted from the workflow, over
// stubbed API responses — rather than restating its rules in Go. A guard that
// reimplements what it guards proves only that two copies agree, and the two
// copies drift; and this failure was silent for as long as it existed, because
// a release image nobody pulled is not a signal any workflow reads.
package releasegate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The workflow, the job, and the step whose script is under test. Each is
// looked up by name and fails the suite when absent: a renamed step is a guard
// that silently judges nothing, which is the shape of the defect itself.
const (
	gateWorkflow = ".github/workflows/docker-image.yml"
	gateJob      = "verify-release-tag"
	gateStep     = "Assert the required checks passed on the tagged commit"

	rollingWorkflow = ".github/workflows/ci.yml"
	rollingJob      = "publish-image"
)

// The two env keys the step splits the required checks across. They are read
// off the workflow rather than restated, so the judgement below follows the
// workflow's own list; these names are only how it is found.
const (
	queueChecksKey = "QUEUE_CHECKS"
	heavyChecksKey = "HEAVY_CHECKS"
)

var (
	jobHeader  = regexp.MustCompile(`(?m)^  [A-Za-z0-9_.-]+:[ \t]*$`)
	stepHeader = regexp.MustCompile(`(?m)^      - name: `)
	envEntry   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*): (.*)$`)
	needsEntry = regexp.MustCompile(`^      - ([A-Za-z0-9_.-]+)$`)
)

// checkRun is one row of the check-runs endpoint as the gate's `--jq` projects
// it: which suite it belongs to, the context name, and the conclusion.
type checkRun struct {
	suite      string
	name       string
	conclusion string
}

// scenario is a state of the API for one commit: which workflow runs exist and
// under which event, and what check runs each of their suites carries.
type scenario struct {
	name string
	// runs maps a check-suite id to the event that produced it, exactly what
	// `actions/runs?head_sha=` reports.
	runs map[string]string
	// The `head_branch` of each suite, which is what the gate used to select on
	// and no longer does. Kept in the fixtures because the pre-fix script reads
	// it, so these same scenarios drive both shapes and show which one refuses.
	branches map[string]string
	checks   []checkRun
	// wantRefusal is what the gate owes this state of the world.
	wantRefusal bool
}

// The suite ids the fixtures use. Two suites on one commit is the normal shape
// of a merged commit here, measured 2026-08-31 on 5049126f.
const (
	queueSuite    = "90335713116"
	pushSuite     = "90335932105"
	dispatchSuite = "90400000001"
)

// greenQueue and greenPush are the conclusions a normal merge leaves in each
// suite: the queue ran the core lanes and skipped the image ones, the push did
// the opposite. Both are correct, and neither is a verdict on its own.
func greenQueue(suite string) []checkRun {
	return []checkRun{
		{suite, "test", "success"},
		{suite, "race", "success"},
		{suite, "e2e", "success"},
		{suite, "e2e-postgres-smoke", "success"},
		{suite, "image-smoke", "skipped"},
	}
}

func greenPush(suite string) []checkRun {
	return []checkRun{
		{suite, "test", "success"},
		{suite, "race", "success"},
		{suite, "e2e", "success"},
		{suite, "e2e-postgres-smoke", "success"},
		{suite, "image-smoke", "success"},
	}
}

// withConclusion returns the rows with one name's conclusion in one suite
// replaced, so a fixture states its one difference from a green commit rather
// than restating the whole world.
func withConclusion(rows []checkRun, suite, name, conclusion string) []checkRun {
	out := make([]checkRun, 0, len(rows))
	for _, row := range rows {
		if row.suite == suite && row.name == name {
			row.conclusion = conclusion
		}
		out = append(out, row)
	}
	return out
}

// without removes one name from one suite entirely, which is what an absent
// check run looks like.
func without(rows []checkRun, suite, name string) []checkRun {
	out := make([]checkRun, 0, len(rows))
	for _, row := range rows {
		if row.suite == suite && row.name == name {
			continue
		}
		out = append(out, row)
	}
	return out
}

func merged() []checkRun {
	return append(greenQueue(queueSuite), greenPush(pushSuite)...)
}

func bothRuns() map[string]string {
	return map[string]string{queueSuite: "merge_group", pushSuite: "push"}
}

func bothBranches() map[string]string {
	return map[string]string{
		queueSuite: "gh-readonly-queue/main/pr-650-1bba31433dbbb1474f7d6e21e43ff2597ed4d8a2",
		pushSuite:  "main",
	}
}

// TestReleaseTagGateJudgesEachLaneWhereItActuallyRan runs the workflow's own
// script over states of the API this repository has held and states it must
// refuse. Three of these are the finding: a `skipped` accepted with no queue
// run behind it, a queue-run failure hidden behind the push run's vacuous
// green, and `image-smoke` skipped on the only event that runs it.
func TestReleaseTagGateJudgesEachLaneWhereItActuallyRan(t *testing.T) {
	script := stepScript(t)
	env := stepEnv(t)

	for _, testCase := range []scenario{
		{
			name:     "a normally merged commit, both runs present",
			runs:     bothRuns(),
			branches: bothBranches(),
			checks:   merged(),
		},
		{
			// The finding. `test` carries no verdict on the push run whatever
			// it says, so a `skipped` there with no queue run behind it is a
			// tag published on nothing at all.
			name:        "`test` skipped on the push run and no merge-queue run exists",
			runs:        map[string]string{pushSuite: "push"},
			branches:    map[string]string{pushSuite: "main"},
			checks:      withConclusion(greenPush(pushSuite), pushSuite, "test", "skipped"),
			wantRefusal: true,
		},
		{
			// The same `skipped`, with the evidence. Refusing this one too
			// would be a gate that cannot be satisfied rather than a gate.
			name:     "`test` skipped on the push run, the merge-queue run carries its verdict",
			runs:     bothRuns(),
			branches: bothBranches(),
			checks:   withConclusion(merged(), pushSuite, "test", "skipped"),
		},
		{
			// The vacuous green in full: the queue found a real race and the
			// push run reports `success` for `race` regardless, because every
			// lane beneath it is cleared there.
			name:        "the merge-queue run failed a lane the push run reports green",
			runs:        bothRuns(),
			branches:    bothBranches(),
			checks:      withConclusion(merged(), queueSuite, "race", "failure"),
			wantRefusal: true,
		},
		{
			// The image lanes run on push and nowhere else, so a `skipped`
			// there is the absence of the only verdict that exists.
			name:        "`image-smoke` skipped on the push run",
			runs:        bothRuns(),
			branches:    bothBranches(),
			checks:      withConclusion(merged(), pushSuite, "image-smoke", "skipped"),
			wantRefusal: true,
		},
		{
			name:        "`e2e-postgres-smoke` still running on the push run",
			runs:        bothRuns(),
			branches:    bothBranches(),
			checks:      withConclusion(merged(), pushSuite, "e2e-postgres-smoke", "pending"),
			wantRefusal: true,
		},
		{
			name:        "the push run has not been created yet",
			runs:        map[string]string{queueSuite: "merge_group"},
			branches:    map[string]string{queueSuite: bothBranches()[queueSuite]},
			checks:      greenQueue(queueSuite),
			wantRefusal: true,
		},
		{
			// `RUN_HEAVY` is push, release and dispatch alike, so a dispatched
			// run on `main` carries a real verdict for the image lanes and the
			// gate takes it. This is the operator's way out when a push run is
			// lost to the one-pending-run-per-group rule.
			name:     "the push run was lost and CI was dispatched on main instead",
			runs:     map[string]string{queueSuite: "merge_group", dispatchSuite: "workflow_dispatch"},
			branches: map[string]string{queueSuite: bothBranches()[queueSuite], dispatchSuite: "main"},
			checks:   append(greenQueue(queueSuite), greenPush(dispatchSuite)...),
		},
		{
			name:        "`e2e` never reported in the merge-queue run",
			runs:        bothRuns(),
			branches:    bothBranches(),
			checks:      without(merged(), queueSuite, "e2e"),
			wantRefusal: true,
		},
		{
			// A commit that reached `main` outside the queue has no run in
			// which the core lanes ever judged its diff.
			name:        "no run of any kind exists for the commit",
			runs:        map[string]string{},
			branches:    map[string]string{},
			checks:      nil,
			wantRefusal: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runGate(t, script, env, testCase)

			if testCase.wantRefusal && err == nil {
				t.Fatalf("the gate published this tag and owed a refusal.\n%s", output)
			}
			if !testCase.wantRefusal && err != nil {
				t.Fatalf("the gate refused a tag it owes: %v\n%s", err, output)
			}
		})
	}
}

// TestReleaseTagGateRequiresExactlyWhatTheRollingPathRequires ties the two
// halves of the split back to ci.yml. The release path must not be more
// permissive than the rolling one — a commit whose `image-smoke` failed holds
// `:latest` back, and tagging it must not ship a signed image anyway — and the
// split is only a claim about WHERE each verdict is read, never about which
// checks are owed. A name dropped from either half, or moved out of both,
// silently narrows the gate; a name added to `publish-image` and to neither
// half here leaves the release path behind the rolling one again.
func TestReleaseTagGateRequiresExactlyWhatTheRollingPathRequires(t *testing.T) {
	env := stepEnv(t)
	queue := strings.Fields(env[queueChecksKey])
	heavy := strings.Fields(env[heavyChecksKey])

	if len(queue) == 0 || len(heavy) == 0 {
		t.Fatalf("%s, step %q: %s=%q and %s=%q — an empty half is a set of checks nobody reads a verdict for",
			gateWorkflow, gateStep, queueChecksKey, env[queueChecksKey], heavyChecksKey, env[heavyChecksKey])
	}

	seen := map[string]bool{}
	for _, name := range append(append([]string{}, queue...), heavy...) {
		if seen[name] {
			t.Errorf("%s, step %q: %q is in both halves, so it is judged in a run where it does not run", gateWorkflow, gateStep, name)
		}
		seen[name] = true
	}

	got := append(append([]string{}, queue...), heavy...)
	want := rollingNeeds(t)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s, step %q gates a release on %v, while %s's %q gates `:latest` on %v — the release path may not be the more permissive of the two",
			gateWorkflow, gateStep, got, rollingWorkflow, rollingJob, want)
	}
}

// runGate executes the extracted script with `gh` and `git` shadowed by shell
// functions serving the scenario. Functions rather than stub executables on
// PATH: a bash function shadows an external command everywhere the script could
// reach one, and needs no executable bit, which Windows does not carry.
//
// The stubs answer the endpoint, not the `--jq` — they emit the rows the gate's
// own projection would produce. So these fixtures prove which rows the gate
// judges and how, and NOT that its jq expressions are spelled right; the
// endpoint each call goes to is pinned, because an unrecognised URL is an error
// here rather than an empty answer.
func runGate(t *testing.T, script string, env map[string]string, state scenario) (string, error) {
	t.Helper()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is required to run the gate's own script as the workflow runs it: %v", err)
	}

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return filepath.ToSlash(path)
	}

	var workflowRuns, checkRuns, mainSuites strings.Builder
	for _, suite := range sortedKeys(state.runs) {
		workflowRuns.WriteString(state.runs[suite] + "\t" + suite + "\n")
	}
	for _, suite := range sortedKeys(state.branches) {
		if state.branches[suite] == "main" {
			mainSuites.WriteString(suite + "\n")
		}
	}
	for _, row := range state.checks {
		checkRuns.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\n")
	}

	preamble := "" +
		"gh() {\n" +
		"  url=\"\"\n" +
		"  for arg in \"$@\"; do case \"$arg\" in repos/*) url=\"$arg\";; esac; done\n" +
		"  case \"$url\" in\n" +
		"    */actions/runs*) cat " + write("workflow_runs.tsv", workflowRuns.String()) + " ;;\n" +
		"    */check-runs*) cat " + write("check_runs.tsv", checkRuns.String()) + " ;;\n" +
		"    */check-suites*) cat " + write("main_suites.tsv", mainSuites.String()) + " ;;\n" +
		"    *) echo \"the gate called an endpoint this fixture does not serve: $*\" >&2; return 1 ;;\n" +
		"  esac\n" +
		"}\n" +
		"git() {\n" +
		"  case \"${1:-}\" in\n" +
		"    rev-parse) printf '%s\\n' \"$GITHUB_SHA\" ;;\n" +
		"    *) echo \"the gate ran an unexpected git command: $*\" >&2; return 1 ;;\n" +
		"  esac\n" +
		"}\n"

	command := exec.Command(bash, "-c", preamble+script)
	command.Env = append(os.Environ(),
		"GITHUB_SHA=5049126faa3152cced900c304c3640e4ec724ba5",
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"GITHUB_REF_NAME=v2.0.0",
		"GH_TOKEN=stub",
	)
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}

	done := make(chan struct{})
	timer := time.AfterFunc(2*time.Minute, func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		close(done)
	})
	output, err := command.CombinedOutput()
	if timer.Stop() {
		close(done)
	}
	<-done

	return string(output), err
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// stepScript returns the `run:` block of the gate's judging step, dedented the
// way GitHub hands it to bash.
func stepScript(t *testing.T) string {
	t.Helper()

	block := stepBlock(t)
	marker := "        run: |\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `run: |` block — the gate's script moved, and this guard would run nothing", gateWorkflow, gateStep)
	}

	var script []string
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			script = append(script, "")
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		script = append(script, strings.TrimPrefix(line, "          "))
	}
	return strings.Join(script, "\n")
}

// stepEnv returns the step's `env:` mapping, minus the entries whose value is a
// workflow expression — those are supplied by the harness instead.
func stepEnv(t *testing.T) map[string]string {
	t.Helper()

	block := stepBlock(t)
	marker := "        env:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `env:` mapping — the check names the gate reads are declared there", gateWorkflow, gateStep)
	}

	env := map[string]string{}
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		entry := strings.TrimPrefix(line, "          ")
		if strings.HasPrefix(entry, "#") || strings.HasPrefix(entry, " ") {
			continue
		}
		match := envEntry.FindStringSubmatch(entry)
		if match == nil || strings.Contains(match[2], "${{") {
			continue
		}
		env[match[1]] = strings.TrimSpace(match[2])
	}

	for _, key := range []string{queueChecksKey, heavyChecksKey} {
		if env[key] == "" {
			t.Fatalf("%s, step %q declares no %s. The gate reads each lane's verdict from the run where that lane executes, and this is where the two halves are named; a step without them is judging all five somewhere one of them never ran",
				gateWorkflow, gateStep, key)
		}
	}
	return env
}

// stepBlock returns the text of the judging step, from its `- name:` line to
// the next step at the same indentation.
func stepBlock(t *testing.T) string {
	t.Helper()

	block := jobBlock(t, gateWorkflow, gateJob)
	header := "      - name: " + gateStep + "\n"
	start := strings.Index(block, header)
	if start < 0 {
		t.Fatalf("%s, job %q: no step named %q — the gate was renamed or removed, and this guard would judge nothing", gateWorkflow, gateJob, gateStep)
	}
	rest := block[start+len(header):]

	if next := stepHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

// rollingNeeds reads `publish-image`'s dependency list, the set of checks
// `:latest` waits for. The release gate is compared against it rather than
// against a copy of it kept here.
func rollingNeeds(t *testing.T) []string {
	t.Helper()

	block := jobBlock(t, rollingWorkflow, rollingJob)
	marker := "    needs:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, job %q: no `needs:` list", rollingWorkflow, rollingJob)
	}

	var needs []string
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		match := needsEntry.FindStringSubmatch(line)
		if match == nil {
			break
		}
		needs = append(needs, match[1])
	}
	if len(needs) == 0 {
		t.Fatalf("%s, job %q lists no dependencies, so there is nothing to hold the release gate to", rollingWorkflow, rollingJob)
	}
	return needs
}

// jobBlock returns the text of one job, from its header to the next job header
// at the same indentation. It fails closed: a renamed job is a failure here,
// never a silently empty search.
func jobBlock(t *testing.T, workflow, job string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(workflow)))
	if err != nil {
		t.Fatalf("read %s: %v", workflow, err)
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")

	header := "\n  " + job + ":\n"
	start := strings.Index(content, header)
	if start < 0 {
		t.Fatalf("%s: no job named %q", workflow, job)
	}
	rest := content[start+len(header):]

	if next := jobHeader.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
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
