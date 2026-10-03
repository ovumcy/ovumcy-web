package releasegate

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The gate's `test`, `race` and `e2e` are read from the merge-queue run, and
// that run reports all three `success` whether its lanes executed or ci.yml's
// `changes` job cleared them for a tree the queued pull request had already
// tested (the proven-tree skip). Observed on 285d14c0: queue run 100349007410
// green with test-go-shard, test-go-rest and every race lane skipped, the work
// done once in PR 962's own run. A release commit could thus be tagged with no
// Go or race job executed in any run the gate read. `WORK_CHECKS` names a real
// job per lane, and the gate takes its `success` only from a run of ci.yml that
// the merge queue or a push to `main` started — never from a pull request run,
// which executes the pull request branch's own copy of the workflow; these tests
// hold the gate to it, over the real script.

// extraSuite is a further run on the commit, and otherWorkflowFile a workflow
// that declares the same job names ci.yml does. Neither is anything ci.yml's
// merge-queue or main-push runs produce.
const (
	extraSuite        = "90500000001"
	otherWorkflowFile = ".github/workflows/security.yml"
)

// dispatched is the suite of a run with no base to diff against, so every lane
// ran, the image ones and both Go jobs included: a dispatched run, a release
// run, a pull request run, or whatever the case lists it as.
func dispatched(suite string) []checkRun {
	rows := withConclusion(greenPush(suite), suite, unitJob, "success")
	return withConclusion(rows, suite, raceJob, "success")
}

// TestReleaseTagGateRequiresARealJobNotAQueueGreen runs the gate over the states
// of the world that separate "the lanes ran" from "the gates are green": the
// first is the finding, the rest bound what may stand in for a real job.
func TestReleaseTagGateRequiresARealJobNotAQueueGreen(t *testing.T) {
	script := stepScript(t)
	env := stepEnv(t)

	// skippedQueue is the commit every case below starts from: the queue and the
	// push ran no Go job, so whatever else the case adds is the only proof.
	skippedQueue := func(extra ...checkRun) []checkRun {
		return append(append(provenQueue(queueSuite), greenPush(pushSuite)...), extra...)
	}

	for _, testCase := range []scenario{
		{
			name:   "the queue ran the lanes for real",
			runs:   bothRuns(),
			checks: merged(),
		},
		{
			// The finding: every gate green, no Go job run in any run the gate
			// reads.
			name:        "the queue skipped every lane for a proven tree and no other run carries them",
			runs:        bothRuns(),
			checks:      skippedQueue(),
			wantRefusal: true,
		},
		{
			// A pull request run of ci.yml, under the job names the gate asks
			// for, with both jobs `success`: the lanes did run there, and it is
			// still not proof. It ran the copy of the workflow on the pull
			// request's branch.
			name:        "the queue skipped every lane and a ci.yml pull_request run carries the real jobs",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "pull_request"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			runBranches: map[string]string{extraSuite: "feature/proposal"},
			wantRefusal: true,
		},
		{
			// A fork's pull request can come from a branch named `main`, so the
			// head branch a run lists does not make it a run on `main`: the event
			// is what decides.
			name:        "the real jobs are in a ci.yml pull_request run whose head branch is named main",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "pull_request"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			runBranches: map[string]string{extraSuite: "main"},
			wantRefusal: true,
		},
		{
			// A run started by hand runs every lane and leaves real jobs on the
			// commit, but it can be started against any branch whose tip is this
			// commit, so what its jobs say is not read.
			name:        "the queue skipped every lane and CI was dispatched on the commit",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", dispatchSuite: "workflow_dispatch"},
			checks:      skippedQueue(dispatched(dispatchSuite)...),
			wantRefusal: true,
		},
		{
			name:        "the queue skipped every lane and a release run carries the real jobs",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "release"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			wantRefusal: true,
		},
		{
			// The positive controls of the cases above: the same extra run is read
			// when it is a ci.yml run of an event that counts.
			name:   "the queue skipped every lane and a second ci.yml push run on main carries the real jobs",
			runs:   map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "push"},
			checks: skippedQueue(dispatched(extraSuite)...),
		},
		{
			name:   "the queue skipped every lane and a second ci.yml merge-queue run carries the real jobs",
			runs:   map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "merge_group"},
			checks: skippedQueue(dispatched(extraSuite)...),
		},
		{
			name:        "the real jobs are in a push run on a branch other than main",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "push"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			runBranches: map[string]string{extraSuite: "feature/untrusted"},
			wantRefusal: true,
		},
		{
			// Same event, same job names, another workflow file: the check-runs
			// endpoint lists them beside ci.yml's, and the name alone proves nothing.
			name:        "the real jobs are in a merge-queue run of another workflow file",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "merge_group"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			runFiles:    map[string]string{extraSuite: otherWorkflowFile},
			wantRefusal: true,
		},
		{
			name:        "the real jobs are in a push run of another workflow file",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "push"},
			checks:      skippedQueue(dispatched(extraSuite)...),
			runFiles:    map[string]string{extraSuite: otherWorkflowFile},
			wantRefusal: true,
		},
		{
			// The gates are read the same way: a green `test`, `race` and `e2e`
			// from a merge-queue run of another workflow file is no queue verdict.
			name:        "the only merge-queue run is another workflow file's",
			runs:        bothRuns(),
			checks:      merged(),
			runFiles:    map[string]string{queueSuite: otherWorkflowFile},
			wantRefusal: true,
		},
		{
			name:        "the only push run is another workflow file's",
			runs:        bothRuns(),
			checks:      merged(),
			runFiles:    map[string]string{pushSuite: otherWorkflowFile},
			wantRefusal: true,
		},
		{
			name:        "a run listed with no workflow file is not ci.yml's",
			runs:        bothRuns(),
			checks:      merged(),
			runFiles:    map[string]string{queueSuite: ""},
			wantRefusal: true,
		},
		{
			// Only `test-go-shard (1)` ran: the race lane is cleared for a diff
			// with no compiled Go, so the tag has no race job to point at on this
			// commit.
			name:        "the unit lane ran and the race lane was cleared for a diff with no compiled Go",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), queueSuite, raceJob, "skipped"),
			wantRefusal: true,
		},
		{
			name:        "the unit lane is still running in the queue run",
			runs:        bothRuns(),
			checks:      withConclusion(merged(), queueSuite, unitJob, "pending"),
			wantRefusal: true,
		},
		{
			// A failure on the commit refuses even beside a pull request run that
			// passed: it is a verdict about this commit, and the pull request's is
			// about the branch it ran on.
			name:        "the race lane failed on the commit and a pull_request run passed",
			runs:        map[string]string{queueSuite: "merge_group", pushSuite: "push", extraSuite: "pull_request"},
			checks:      append(withConclusion(merged(), queueSuite, raceJob, "failure"), dispatched(extraSuite)...),
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
			if testCase.wantRefusal {
				for _, stubAbort := range []string{stubUnservedEndpoint, stubUnexpectedGit} {
					if strings.Contains(output, stubAbort) {
						t.Fatalf("the refusal came from a harness stub (%q), not from the gate's own judgement.\n%s", stubAbort, output)
					}
				}
			}
		})
	}
}

// TestReleaseTagGateReadsJobsAndNeverCheckRuns holds the gate to the source of
// its proof. A check run is whatever any token holding `checks: write` posts to
// a commit, under any name and with any conclusion, and the Checks API serves it
// beside the jobs of the real runs; a job is what a run executed. Every case
// below gives the Checks API a forged row (the stub serves it and fails the test
// if the gate asks for it at all) and the Actions API the real state, and the
// real state decides.
func TestReleaseTagGateReadsJobsAndNeverCheckRuns(t *testing.T) {
	script := stepScript(t)
	env := stepEnv(t)

	skippedQueue := func(extra ...checkRun) []checkRun {
		return append(append(provenQueue(queueSuite), greenPush(pushSuite)...), extra...)
	}

	for _, testCase := range []struct {
		scenario
		wantOutput string
	}{
		{
			// The control: every job is real.
			scenario:   scenario{name: "every job is real", runs: bothRuns(), checks: merged()},
			wantOutput: "real jobs (",
		},
		{
			// The finding. The queue skipped every Go lane for a proven tree, so no
			// job proves the work, and a `success` under each name is posted to
			// the queue run's suite and the push run's alike.
			scenario: scenario{
				name: "the Go jobs were skipped and forged check runs say success", runs: bothRuns(), checks: skippedQueue(),
				forged: []checkRun{
					{queueSuite, unitJob, "success"}, {queueSuite, raceJob, "success"},
					{pushSuite, unitJob, "success"}, {pushSuite, raceJob, "success"},
				},
			},
			wantOutput: "no `test-go-shard (1)` job concluded success on",
		},
		{
			// Everything the gate asks for, forged: a gate that read check runs
			// would find every name green in every run it admits.
			scenario: scenario{
				name: "the Go jobs were skipped and every check the gate reads is forged green", runs: bothRuns(), checks: skippedQueue(),
				forged: merged(),
			},
			wantOutput: "no `test-go-shard (1)` job concluded success on",
		},
		{
			scenario: scenario{
				name: "only the unit job was skipped and a forged check run says success", runs: bothRuns(),
				checks: withConclusion(merged(), queueSuite, unitJob, "skipped"),
				forged: []checkRun{{queueSuite, unitJob, "success"}},
			},
			wantOutput: "no `test-go-shard (1)` job concluded success on",
		},
		{
			// A real failure is not outvoted by a forged success of the same name.
			scenario: scenario{
				name: "a real race job failed and a forged check run says success", runs: bothRuns(),
				checks: withConclusion(merged(), queueSuite, raceJob, "failure"),
				forged: []checkRun{{queueSuite, raceJob, "success"}},
			},
			wantOutput: "a `race-db (1)` job on",
		},
		{
			// The gates are read the same way: a forged `success` for the image
			// lane the push run skipped is not its verdict.
			scenario: scenario{
				name: "the image lane was skipped and a forged check run says success", runs: bothRuns(),
				checks: withConclusion(merged(), pushSuite, "image-smoke", "skipped"),
				forged: []checkRun{{pushSuite, "image-smoke", "success"}},
			},
			wantOutput: "a `image-smoke` job in the push, release or dispatch run",
		},
		{
			// A forged failure is no verdict either, in the other direction: it
			// refuses nothing that the runs' jobs accept.
			scenario: scenario{
				name: "every job is real and a forged check run says failure", runs: bothRuns(), checks: merged(),
				forged: []checkRun{{queueSuite, unitJob, "failure"}, {queueSuite, "test", "failure"}},
			},
			wantOutput: "real jobs (",
		},
		{
			// `filter=all` lists every attempt: the newest of a name speaks for it.
			scenario: scenario{
				name: "a lane failed on the first attempt and passed on the re-run", runs: bothRuns(),
				checks: withConclusion(merged(), queueSuite, "race", "failure"),
				reruns: []checkRun{{queueSuite, "race", "success"}},
			},
			wantOutput: "real jobs (",
		},
		{
			scenario: scenario{
				name: "a lane passed on the first attempt and failed on the re-run", runs: bothRuns(), checks: merged(),
				reruns: []checkRun{{queueSuite, "race", "failure"}},
			},
			wantOutput: "a `race` job in the merge-queue run",
		},
		{
			scenario: scenario{
				name: "the unit job passed on the first attempt and its re-run is pending", runs: bothRuns(), checks: merged(),
				reruns: []checkRun{{queueSuite, unitJob, "pending"}},
			},
			wantOutput: "a `test-go-shard (1)` job on",
		},
		{
			scenario: scenario{
				name: "the jobs of the runs cannot be read", runs: bothRuns(), checks: merged(), jobsUnreadable: true,
			},
			wantOutput: "could not be read",
		},
	} {
		wantRefusal := testCase.wantOutput != "real jobs ("
		testCase.scenario.wantRefusal = wantRefusal
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runGate(t, script, env, testCase.scenario)

			if wantRefusal && err == nil {
				t.Fatalf("the gate published this tag and owed a refusal.\n%s", output)
			}
			if !wantRefusal && err != nil {
				t.Fatalf("the gate refused a tag it owes: %v\n%s", err, output)
			}
			for _, stubAbort := range []string{stubUnservedEndpoint, stubUnexpectedGit} {
				if strings.Contains(output, stubAbort) {
					t.Fatalf("the verdict came from a harness stub (%q), not from the gate's own judgement.\n%s", stubAbort, output)
				}
			}
			if !strings.Contains(output, testCase.wantOutput) {
				t.Fatalf("the output does not carry %q, so the verdict was reached for another reason.\n%s", testCase.wantOutput, output)
			}
		})
	}
}

// TestAGateReadingCheckRunsWouldAcceptTheForgedProof is the negative control of
// the cases above: the same script, with its one read of a run's jobs pointed at
// the commit's check runs as an earlier version read them, publishes the tag the
// forged check runs vouch for. Without it the refusals above could be refusals
// for any reason; with it they are the jobs read doing the work. The edit is on
// the script text held in memory and nothing else.
func TestAGateReadingCheckRunsWouldAcceptTheForgedProof(t *testing.T) {
	script := stepScript(t)
	const jobsEndpoint = "actions/runs/${jobs_run}/jobs?filter=all&per_page=100"
	if strings.Count(script, jobsEndpoint) != 1 {
		t.Fatalf("%s, step %q: the read of a run's jobs is not spelled `%s` exactly once, so this control cannot point it elsewhere", gateWorkflow, gateStep, jobsEndpoint)
	}
	checkRunsReader := strings.Replace(script, jobsEndpoint, "commits/${sha}/check-runs?per_page=100", 1)

	state := scenario{
		name: "the Go jobs were skipped and forged check runs say success", runs: bothRuns(),
		checks: append(provenQueue(queueSuite), greenPush(pushSuite)...),
		forged: merged(),
	}

	output, err := runGateAllowingChecksRead(t, checkRunsReader, stepEnv(t), state)
	if err != nil || !strings.Contains(output, stubChecksAPIRead) {
		t.Fatalf("a gate that reads check runs was expected to publish on forged rows (exit: %v); the forged cases above would then prove nothing.\n%s", err, output)
	}
	if _, err := runGate(t, script, stepEnv(t), state); err == nil {
		t.Fatalf("the real gate published this tag on forged check runs")
	}
}

// TestNoStepOfTheTagGateReadsAnythingButTheTwoActionsEndpoints is the same
// invariant read off the source, so a new read cannot slip in beside the ones
// above: every API path the gate step spells is one of the run listing and one
// run's jobs, and nothing in the file asks the Checks API or the commit status
// API for a verdict.
func TestNoStepOfTheTagGateReadsAnythingButTheTwoActionsEndpoints(t *testing.T) {
	code := func(text string) string {
		var kept []string
		for _, line := range strings.Split(text, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}

	paths := map[string]bool{}
	for _, match := range regexp.MustCompile(`repos/\$\{GITHUB_REPOSITORY\}/([^"?\s]*)`).FindAllStringSubmatch(code(stepScript(t)), -1) {
		paths[match[1]] = true
	}
	want := map[string]bool{"actions/runs": true, "actions/runs/${jobs_run}/jobs": true}
	for path := range paths {
		if !want[path] {
			t.Errorf("%s, step %q reads `%s`: the gate's proof is the run listing and one run's jobs, and nothing else may feed it", gateWorkflow, gateStep, path)
		}
	}
	for path := range want {
		if !paths[path] {
			t.Errorf("%s, step %q no longer reads `%s`: this guard would pass over a gate that reads nothing it names", gateWorkflow, gateStep, path)
		}
	}

	workflow := code(workflowfile.Read(t, gateWorkflow))
	for _, forbidden := range []string{"check-runs", "check-suites", "check_suite", "/statuses", "/status\"", "checks:"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("%s names `%s` outside a comment: a check run or a status is whatever a token with write access to them posts, and is no proof", gateWorkflow, forbidden)
		}
	}
}

// TestTheWorkChecksNameJobsCiActuallyRuns ties each WORK_CHECKS entry back to
// ci.yml: a context the workflow never produces is a gate that refuses every
// tag, and one its `name:` renamed is a gate that judges nothing. Each entry is
// `<job> (<cell>)`, which is how GitHub names a matrix cell that has no `name:`.
func TestTheWorkChecksNameJobsCiActuallyRuns(t *testing.T) {
	entry := regexp.MustCompile(`^([a-z][a-z0-9-]*) \(([0-9]+)\)$`)
	names := strings.Split(stepEnv(t)[workChecksKey], "|")
	if len(names) < 2 {
		t.Fatalf("%s=%q: the unit lane and the race lane are each owed a real job", workChecksKey, strings.Join(names, "|"))
	}

	seen := map[string]bool{}
	for _, name := range names {
		match := entry.FindStringSubmatch(name)
		if match == nil {
			t.Errorf("%s entry %q is not `<job> (<matrix cell>)`", workChecksKey, name)
			continue
		}
		job, cell := match[1], match[2]
		seen[job] = true

		block := workflowfile.Job(t, rollingWorkflow, job)
		if regexp.MustCompile(`(?m)^    name:`).MatchString(block) {
			t.Errorf("%s, job %q sets `name:`, so its check context is not %q", rollingWorkflow, job, name)
		}
		if !regexp.MustCompile(`(?m)^          - ` + cell + `\s*$`).MatchString(block) {
			t.Errorf("%s, job %q has no matrix cell %s, so no check run named %q exists", rollingWorkflow, job, cell, name)
		}
	}
	for _, job := range []string{"test-go-shard", "race-db"} {
		if !seen[job] {
			t.Errorf("%s names no cell of %q: the %s lane has no real job owed", workChecksKey, job, job)
		}
	}
}

// TestTheProvenTreeSkipStillRestsOnWhatCiActuallyDoes asserts the two facts
// about ci.yml the work check is derived from. Neither would announce itself
// when it changed: the gate would keep refusing, or keep passing, for a reason
// that no longer holds.
//
//   - A proven queue entry clears `run_core`, which is why a green `test`/`race`
//     in the queue run is no evidence of work, and why the gate looks past it.
//   - The two jobs are cleared by `run_unit` and `run_race`, read fail-safe, so
//     "skipped" there means the lane was cleared, not that it failed to start.
func TestTheProvenTreeSkipStillRestsOnWhatCiActuallyDoes(t *testing.T) {
	changes := workflowfile.Job(t, rollingWorkflow, changesJob)
	for _, premise := range []struct{ text, claim string }{
		{
			text:  "if [ -n \"$proven\" ]; then\n              run_core=false",
			claim: "a proven merge-queue entry no longer clears `run_core`. " + gateWorkflow + " asks for a real job BECAUSE the queue's `test`/`race` are green with their lanes skipped; if the skip is gone the work check is redundant, and if it moved, re-derive where the real job runs",
		},
	} {
		if !strings.Contains(changes, premise.text) {
			t.Errorf("%s, job %q: %s", rollingWorkflow, changesJob, premise.claim)
		}
	}

	for job, output := range map[string]string{"test-go-shard": "run_unit", "race-db": "run_race"} {
		condition, err := workflowfile.JobIf(workflowfile.Job(t, rollingWorkflow, job))
		if err != nil {
			t.Errorf("%s, job %q: `if:` cannot be read: %v", rollingWorkflow, job, err)
			continue
		}
		if !strings.Contains(condition, "needs.changes.outputs."+output+" != 'false'") {
			t.Errorf("%s, job %q no longer clears on `%s`: %s", rollingWorkflow, job, output, condition)
		}
	}
}
