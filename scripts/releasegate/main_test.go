// Package releasegate holds the release tag gate to the one thing it exists to
// assert: that the six checks `:latest` waits for were really run on the
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
//
// The split is only correct while ci.yml still behaves the way it is read here,
// so that reading is asserted too — the two premises live in another file, and
// nothing else would notice them changing.
package releasegate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/testenv"
	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
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
	changesJob      = "changes"
)

// The two env keys the step splits the required checks across. They are read
// off the workflow rather than restated, so the judgement below follows the
// workflow's own list; these names are only how it is found.
const (
	queueChecksKey = "QUEUE_CHECKS"
	heavyChecksKey = "HEAVY_CHECKS"
)

var (
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
	// `actions/runs?head_sha=` reports. The event is the whole of what the gate
	// selects on, so it is the whole of what a fixture states.
	runs   map[string]string
	checks []checkRun
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
		{suite, "e2e-cross-browser", "skipped"},
	}
}

func greenPush(suite string) []checkRun {
	return []checkRun{
		{suite, "test", "success"},
		{suite, "race", "success"},
		{suite, "e2e", "success"},
		{suite, "e2e-postgres-smoke", "success"},
		{suite, "image-smoke", "success"},
		{suite, "e2e-cross-browser", "success"},
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
			name:   "a normally merged commit, both runs present",
			runs:   bothRuns(),
			checks: merged(),
		},
		{
			// The finding. `test` carries no verdict on the push run whatever
			// it says, so a `skipped` there with no queue run behind it is a
			// tag published on nothing at all.
			name:        "`test` skipped on the push run and no merge-queue run exists",
			runs:        map[string]string{pushSuite: "push"},
			checks:      withConclusion(greenPush(pushSuite), pushSuite, "test", "skipped"),
			wantRefusal: true,
		},
		{
			// The same `skipped`, with the evidence. Refusing this one too
			// would be a gate that cannot be satisfied rather than a gate.
			name:   "`test` skipped on the push run, the merge-queue run carries its verdict",
			runs:   bothRuns(),
			checks: withConclusion(merged(), pushSuite, "test", "skipped"),
		},
		{
			// The vacuous green in full: the queue found a real race and the
			// push run reports `success` for `race` regardless, because every
			// lane beneath it is cleared there.
			name:        "the merge-queue run failed a lane the push run reports green",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), queueSuite, "race", "failure"),
			wantRefusal: true,
		},
		{
			// `skipped` in the QUEUE run, which is the half the fix moved the
			// verdict to. The gate jobs carry `if: !cancelled()`, so they run
			// and report even when every lane beneath them is cleared —
			// `skipped` there means the run was cancelled outright, and a
			// cancelled run is not a verdict. Without this fixture, restoring
			// `success|skipped` on the queue half would read green here.
			name:        "`test` skipped in the merge-queue run",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), queueSuite, "test", "skipped"),
			wantRefusal: true,
		},
		{
			name:        "`e2e` still running in the merge-queue run",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), queueSuite, "e2e", "pending"),
			wantRefusal: true,
		},
		{
			// The image lanes run on push and nowhere else, so a `skipped`
			// there is the absence of the only verdict that exists.
			name:        "`image-smoke` skipped on the push run",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), pushSuite, "image-smoke", "skipped"),
			wantRefusal: true,
		},
		{
			name:        "`e2e-postgres-smoke` still running on the push run",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), pushSuite, "e2e-postgres-smoke", "pending"),
			wantRefusal: true,
		},
		{
			// Every row is judged, never the newest or the first, and this is
			// the case that decides which: a failed push run and a dispatched
			// one that passed sit side by side, both of them heavy evidence.
			// Taking either one alone would let a smoke failure be re-run away.
			//
			// The passing rows come FIRST in this fixture on purpose. A gate
			// that stopped at the first match per name would read green over
			// the failure and this scenario would prove nothing; with the
			// failure last, only a gate that judges every row refuses.
			name: "a failed push run and a later dispatch run that passed",
			runs: map[string]string{
				queueSuite:    "merge_group",
				pushSuite:     "push",
				dispatchSuite: "workflow_dispatch",
			},
			checks: append(
				append(greenQueue(queueSuite), greenPush(dispatchSuite)...),
				withConclusion(greenPush(pushSuite), pushSuite, "image-smoke", "failure")...),
			wantRefusal: true,
		},
		{
			// The heavy run's verdict for a queue-judged lane is vacuous, and
			// vacuous is not the same as ignored: every lane beneath `test` is
			// cleared on push, so a FAILURE there is a lane that ran when it
			// should not have been able to, and the gate does not walk past it
			// merely because it reads the real verdict elsewhere.
			name:        "`test` failed in the push run, where its lanes are cleared",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), pushSuite, "test", "failure"),
			wantRefusal: true,
		},
		{
			name:        "the push run has not been created yet",
			runs:        map[string]string{queueSuite: "merge_group"},
			checks:      greenQueue(queueSuite),
			wantRefusal: true,
		},
		{
			// `RUN_HEAVY` is push, release and dispatch alike, so a dispatched
			// run carries a real verdict for the image lanes and the gate takes
			// it. This is the operator's way out when a push run is lost to the
			// one-pending-run-per-group rule.
			name:   "the push run was lost and CI was dispatched instead",
			runs:   map[string]string{queueSuite: "merge_group", dispatchSuite: "workflow_dispatch"},
			checks: append(greenQueue(queueSuite), greenPush(dispatchSuite)...),
		},
		{
			name:        "`e2e` never reported in the merge-queue run",
			runs:        bothRuns(),
			checks:      without(merged(), queueSuite, "e2e"),
			wantRefusal: true,
		},
		{
			// A commit that reached `main` outside the queue has no run in
			// which the core lanes ever judged its diff.
			name:        "no run of any kind exists for the commit",
			runs:        map[string]string{},
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

// TestTheSplitStillRestsOnWhatCiActuallyDoes asserts the two facts about ci.yml
// the split is derived from. Neither lives in the workflow this package
// otherwise reads, and neither would announce itself when it changed:
//
//   - `push` clears `run_core` unconditionally. That is the whole reason
//     `test`, `race` and `e2e` are read from the merge-queue run. Were it to
//     stop, those three would carry a real verdict on the push too and this
//     gate would be refusing tags on a mechanism that no longer exists.
//   - `push` carries no base to diff against, so `run_e2e` stays true. That is
//     the whole reason `image-smoke` may be required to be `success` rather
//     than tolerated as `skipped` on a documentation-only commit.
func TestTheSplitStillRestsOnWhatCiActuallyDoes(t *testing.T) {
	block := workflowfile.Job(t, rollingWorkflow, changesJob)

	for _, premise := range []struct {
		text  string
		claim string
	}{
		{
			text: "if [ \"$EVENT_NAME\" = \"push\" ]; then\n            run_core=false",
			claim: "a push to `main` no longer clears `run_core` unconditionally. " +
				gateWorkflow + " reads `test`, `race` and `e2e` from the merge-queue run BECAUSE the push run's verdict for them is vacuous; if the push run now does that work, re-derive the split before changing it",
		},
		{
			// The `*)` arm, matched as code rather than by the verdict
			// sentence beside it: the fact is that `push` reaches a branch
			// which clears the base, and a message can be preserved through
			// exactly the rework this premise needs to notice.
			text: "            *)\n              base=\"\"",
			claim: "a push to `main` now has a base to diff against, so `run_e2e` may go false there. " +
				gateWorkflow + " requires `success` from `image-smoke` in the push run BECAUSE that lane runs on every push whatever the diff touched; if it can now be skipped, that requirement blocks a documentation-only release tag forever",
		},
	} {
		if !strings.Contains(block, premise.text) {
			t.Errorf("%s, job %q: %s", rollingWorkflow, changesJob, premise.claim)
		}
	}
}

// permissionRank orders the three values a scope can take. A caller grants a
// ceiling, so `read` where the called job wants `write` is as broken as an
// absent scope.
var permissionRank = map[string]int{"none": 0, "read": 1, "write": 2}

var permissionEntry = regexp.MustCompile(`^      ([a-z-]+): (none|read|write)$`)

// TestTheCallerCeilingCoversEveryScopeTheCalledWorkflowDeclares is the guard for
// a defect this very change nearly shipped: the gate gained `actions: read` and
// ci.yml's `publish-image` — which reaches this workflow through
// `uses: ./.github/workflows/docker-image.yml` — kept the ceiling it had.
//
// A reusable workflow's job cannot request a scope its caller does not hold, and
// the refusal lands on the CALL rather than on the job that asked, so it does
// not matter that the tag gate is skipped on that path. What it costs is the
// whole publish: `:latest` stops following `main`, which is the failure closed
// one pull request before this one. The comment at the caller says so; this is
// what makes it true.
func TestTheCallerCeilingCoversEveryScopeTheCalledWorkflowDeclares(t *testing.T) {
	ceiling := declaredPermissions(t, workflowfile.Job(t, rollingWorkflow, rollingJob))
	if len(ceiling) == 0 {
		t.Fatalf("%s, job %q declares no `permissions:` block, so it grants the called workflow the repository default and this comparison would pass over anything", rollingWorkflow, rollingJob)
	}

	content := workflowfile.Read(t, gateWorkflow)

	blocks := strings.Split(content, "\n    permissions:\n")[1:]

	// One block per job, or this guard is covering only the jobs it happened to
	// recognise. Finding SOME is the dangerous outcome, not finding none: the
	// scopes it did read still compare cleanly, so a job whose block is written
	// in a shape this reader walks past asks for whatever it likes and the
	// guard reports green — the defect it exists to catch, one job over.
	jobs := workflowfile.JobHeaders(t, gateWorkflow, content)
	if len(blocks) != len(jobs) {
		t.Fatalf("%s has %d jobs and %d job-level `permissions:` blocks this guard can read. Every job's block has to be readable here, because the ones it cannot see are the ones that widen unnoticed — write the block in the usual shape, or teach this reader the new one",
			gateWorkflow, len(jobs), len(blocks))
	}

	requested := map[string]string{}
	for _, block := range blocks {
		for scope, level := range declaredPermissions(t, "\n    permissions:\n"+block) {
			if permissionRank[level] > permissionRank[requested[scope]] {
				requested[scope] = level
			}
		}
	}
	if len(requested) == 0 {
		t.Fatalf("%s declares no job-level `permissions:` at all, which this guard reads as its own failure rather than as nothing to check", gateWorkflow)
	}

	for _, scope := range sortedKeys(requested) {
		if permissionRank[ceiling[scope]] < permissionRank[requested[scope]] {
			t.Errorf("%s declares `%s: %s`, and %s's %q grants `%s: %s`. A caller cannot grant a called workflow more than it holds, and the shortfall fails the CALL, not the job that asked — so the publish stops entirely and `:latest` stops following `main`. Add the scope to that job's `permissions:` block",
				gateWorkflow, scope, requested[scope], rollingWorkflow, rollingJob, scope, orNone(ceiling[scope]))
		}
	}
}

// declaredPermissions reads one `permissions:` mapping out of a job block.
func declaredPermissions(t *testing.T, block string) map[string]string {
	t.Helper()

	marker := "    permissions:\n"
	start := strings.Index(block, marker)
	if start < 0 {
		return nil
	}

	granted := map[string]string{}
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		// Comments and blank lines are skipped, never stopped at. A blank line
		// between grouped entries is legal and unremarkable, and stopping there
		// would drop every scope below it — read as the called workflow's
		// request that is the silent direction, since a scope nobody collected
		// is a scope nobody checks the ceiling for. What ends the block is a key
		// at the job's own indentation, which is not an entry and not blank.
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		match := permissionEntry.FindStringSubmatch(line)
		if match == nil {
			break
		}
		granted[match[1]] = match[2]
	}
	return granted
}

func orNone(level string) string {
	if level == "" {
		return "none"
	}
	return level
}

// runGate executes the extracted script with `gh` and `git` shadowed by shell
// functions serving the scenario. Functions rather than stub executables on
// PATH: a bash function shadows an external command everywhere the script could
// reach one, and needs no executable bit, which Windows does not carry.
//
// The stubs answer the endpoint, not the `--jq` — they emit the rows the gate's
// own projection would produce. So these fixtures prove which rows the gate
// judges and how, and NOT that its jq expressions are spelled right. The
// endpoints themselves are pinned hard: anything the gate asks for other than
// the two it is written around is an error here, which is what makes a return
// to the `check-suites`-by-`head_branch` shape fail loudly rather than be
// served.
func runGate(t *testing.T, script string, env map[string]string, state scenario) (string, error) {
	t.Helper()

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return shellQuote(filepath.ToSlash(path))
	}

	var workflowRuns, checkRuns strings.Builder
	for _, suite := range sortedKeys(state.runs) {
		workflowRuns.WriteString(state.runs[suite] + "\t" + suite + "\n")
	}
	for _, row := range state.checks {
		checkRuns.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\n")
	}

	preamble := strings.Join([]string{
		`gh() {`,
		`  url=""`,
		`  for arg in "$@"; do case "$arg" in repos/*) url="$arg";; esac; done`,
		`  case "$url" in`,
		`    */actions/runs*) cat ` + write("workflow_runs.tsv", workflowRuns.String()) + ` ;;`,
		`    */check-runs*) cat ` + write("check_runs.tsv", checkRuns.String()) + ` ;;`,
		`    *) echo "the gate called an endpoint this fixture does not serve: $*" >&2; return 1 ;;`,
		`  esac`,
		`}`,
		`git() {`,
		`  case "${1:-}" in`,
		`    rev-parse) printf '%s\n' "$GITHUB_SHA" ;;`,
		`    *) echo "the gate ran an unexpected git command: $*" >&2; return 1 ;;`,
		`  esac`,
		`}`,
		"",
	}, "\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	bash := bashPath(t)
	requireWorkingBash(t, bash)

	command := exec.CommandContext(ctx, bash, "-c", preamble+script)
	command.Env = append(os.Environ(),
		"GITHUB_SHA=5049126faa3152cced900c304c3640e4ec724ba5",
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"GITHUB_REF_NAME=v2.0.0",
		"GH_TOKEN=stub",
	)
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}

	output, err := command.CombinedOutput()
	return string(output), err
}

// bashPath locates the shell the workflow's `shell: bash` steps are written
// for. Absent it, there is nothing to run the gate's own script under, and a
// skip is honest where a Go reimplementation would not be — unless the lane
// running this package declared bash mandatory (OVUMCY_REQUIRE_BASH), in
// which case the same absence is a failure: this suite is the only thing that
// exercises the gate's real script, and a lane that promised to run it cannot
// report green having found no shell to run it under. A bash that resolves
// but does not behave like one is a separate, always-fatal case — see
// requireWorkingBash below.
func bashPath(t *testing.T) string {
	t.Helper()
	return testenv.RequireLookPath(t, "bash", "bash")
}

// requireWorkingBash is the operational half bashPath does not cover: a
// binary named bash that resolves on PATH but does not run scripts the way
// this suite's fixtures assume — a WSL launcher stub with no distro
// installed answers a lookup exactly as a real bash does — fails the suite
// outright, on every machine, whatever the owning lane declared. The probe
// script matches the preamble every fixture already runs under: a shell
// unable to report its own exit code correctly would corrupt every case in
// this file identically.
func requireWorkingBash(t *testing.T, path string) {
	t.Helper()
	testenv.ProbeShell(t, path, "printf ok", "ok")
}

// shellQuote makes a path safe to paste into the stub preamble. A temporary
// directory is named after the test, and a name this file gains later may put a
// space in it — unquoted, the stub would then `cat` two paths that do not
// exist, and the resulting refusal would read as the gate's rather than the
// harness's.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
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

	block := workflowfile.Job(t, gateWorkflow, gateJob)
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

	block := workflowfile.Job(t, rollingWorkflow, rollingJob)
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
