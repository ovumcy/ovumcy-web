package releasegate

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testenv"
	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// A commit that only adds documentation runs no race lane of its own: the queue
// clears it for a diff with no compiled Go. The gate used to refuse such a tag
// whatever had run before it. It now accepts a Go job's success from the
// nearest first-parent ancestor whose inputs for that lane are the tagged
// commit's — the same tree-equality premise as ci.yml's proven-tree skip, over
// the lane's inputs — and these tests run the gate's real script over a real
// repository to hold it to that: a success on an identical-inputs ancestor, in
// a merge-queue or main-push run of ci.yml, is accepted, and a missing,
// skipped, neutral or failed run, a run of another workflow file or of another
// event (a pull request run included), a change to the inputs, or a walk past
// the bound is not.

// historyCommit is one commit of the fixture history together with the CI facts
// recorded for it.
type historyCommit struct {
	// files are written before the commit; the last commit of a history is the
	// tagged one.
	files map[string]string
	// suites maps a run id to the event of a workflow run on the commit; checks
	// are the jobs those runs executed.
	suites map[string]string
	checks []checkRun
	// forged are check runs posted to the commit by a token holding `checks:
	// write`: the Checks API would serve them, no run lists a job behind them,
	// and the gate must not read them.
	forged []checkRun
	// suiteFiles and suiteBranches override, per run, the workflow file and head
	// branch the run is listed with (default: ci.yml, `main` or the queue's branch).
	suiteFiles    map[string]string
	suiteBranches map[string]string
	// unreadable names the reads the API refuses for this commit: "runs" and
	// "jobs" (the jobs of each of its runs). The stub fails the call instead of
	// answering empty.
	unreadable []string
	// indexPaths are paths put into the commit without a file in the work tree,
	// for names a file system cannot hold (one with a newline in it).
	indexPaths []string
}

// walkCase is one history and what the gate owes it.
type walkCase struct {
	name    string
	history []historyCommit
	// bound overrides ANCESTOR_WALK_BOUND; empty keeps the workflow's. env
	// overrides any other value of the step's env.
	bound string
	env   map[string]string
	// wantRefusal is the verdict; wantOutput must appear in the output either
	// way, which is what ties a verdict to the reason it was reached for.
	wantRefusal bool
	wantOutput  string
}

// inputs of the two lanes, as files a commit can change.
const (
	goFile        = "internal/walk/walk.go"
	changelogFile = "CHANGELOG.md"
	// A page no test reads. The name is not a real file of the repository: the
	// doc-read guard counts any existing path a test spells as a read of it.
	inertDocFile = "docs/walk-fixture-notes.md"
	// Files the Go binary embeds. Neither name is a real file of the repository.
	sqlFile    = "migrations/0999_walk_fixture.sql"
	localeFile = "internal/i18n/locales/walk_fixture.json"
	readmeFile = "README.md"

	ciYamlPath     = ".github/workflows/ci.yml"
	detectStepName = "Detect whether browser e2e is needed"
)

// detectStepStub is a ci.yml holding only a detect step whose `run:` text is the
// given lines, for the cases where the step misbehaves.
func detectStepStub(lines ...string) string {
	return "name: CI\njobs:\n  changes:\n    steps:\n      - name: " + detectStepName + "\n        run: |\n          " +
		strings.Join(lines, "\n          ") + "\n"
}

// baseCommit is the root: the real ci.yml and a readme, committed once so no
// later diff touches the workflow directory.
func baseCommit(t *testing.T, ciYaml string) historyCommit {
	t.Helper()

	if ciYaml == "" {
		ciYaml = workflowfile.Read(t, rollingWorkflow)
	}
	return historyCommit{files: map[string]string{ciYamlPath: ciYaml, "README.md": "readme\n"}}
}

// ran is a commit whose merge-queue run executed both Go jobs, or whichever
// conclusions the arguments say.
func ran(suite, unit, race string) historyCommit {
	return historyCommit{
		suites: map[string]string{suite: "merge_group"},
		checks: []checkRun{{suite, unitJob, unit}, {suite, raceJob, race}},
	}
}

// withUnreadable returns the commit with reads the API will refuse for it.
func (c historyCommit) withUnreadable(kinds ...string) historyCommit {
	c.unreadable = append(append([]string{}, c.unreadable...), kinds...)
	return c
}

// withFile returns the commit with one more file written.
func (c historyCommit) withFile(path, content string) historyCommit {
	files := map[string]string{}
	for key, value := range c.files {
		files[key] = value
	}
	files[path] = content
	c.files = files
	return c
}

// tagged is the commit the tag points at: every gate green in the queue and the
// push run, with the two Go jobs as given.
func tagged(unit, race string) historyCommit {
	rows := append(greenQueue(queueSuite), greenPush(pushSuite)...)
	rows = withConclusion(rows, queueSuite, unitJob, unit)
	rows = withConclusion(rows, queueSuite, raceJob, race)
	return historyCommit{suites: bothRuns(), checks: rows}
}

// TestReleaseTagAcceptsAGoJobFromAnAncestorWithTheSameInputs is the walk, over
// real history. Each case states its one difference from the accepted shape.
func TestReleaseTagAcceptsAGoJobFromAnAncestorWithTheSameInputs(t *testing.T) {
	base := baseCommit(t, "")

	// goCommit changes compiled Go and ran the lanes; docEdit changes only the
	// changelog and ran neither Go job (the queue cleared them).
	goCommit := func(unit, race string) historyCommit {
		return ran("7000001", unit, race).withFile(goFile, "package walk\n")
	}
	docEdit := func(n int, suite string) historyCommit {
		c := ran(suite, "skipped", "skipped").withFile(changelogFile, "entry "+strconv.Itoa(n)+"\n")
		return c
	}
	release := func(unit, race string) historyCommit {
		return tagged(unit, race).withFile(changelogFile, "release\n")
	}

	// inRun is a Go commit whose real jobs are in a single run of the given event,
	// with the file and head branch overridden when given.
	inRun := func(event, file, branch string) historyCommit {
		c := historyCommit{
			suites: map[string]string{"7000001": event},
			checks: []checkRun{{"7000001", unitJob, "success"}, {"7000001", raceJob, "success"}},
		}.withFile(goFile, "package walk\n")
		if file != "" {
			c.suiteFiles = map[string]string{"7000001": file}
		}
		if branch != "" {
			c.suiteBranches = map[string]string{"7000001": branch}
		}
		return c
	}

	cases := []walkCase{
		{
			// The case the walk exists for: the tag's own runs skipped the race
			// lane, the nearest commit that changed Go ran it.
			name:       "a documentation-only tag takes the race job from the Go commit before it",
			history:    []historyCommit{base, goCommit("success", "success"), release("success", "skipped")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			// The change between the ancestor and the tag is compiled Go: the
			// ancestor's run says nothing about these inputs.
			name: "a Go change between the ancestor and the tag",
			history: []historyCommit{base, goCommit("success", "success"),
				ran("7000002", "skipped", "skipped").withFile("internal/walk/more.go", "package walk\n"),
				release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			// The bound is the only thing refusing this one: the next case is the
			// same history with a bound that reaches the Go commit.
			name: "the Go commit is past the walk bound",
			history: []historyCommit{base, goCommit("success", "success"),
				docEdit(1, "7000011"), docEdit(2, "7000012"), docEdit(3, "7000013"),
				release("success", "skipped")},
			bound:       "3",
			wantRefusal: true,
			wantOutput:  "the walk stops at 3",
		},
		{
			name: "the same history with a bound that reaches the Go commit",
			history: []historyCommit{base, goCommit("success", "success"),
				docEdit(1, "7000011"), docEdit(2, "7000012"), docEdit(3, "7000013"),
				release("success", "skipped")},
			bound:      "4",
			wantOutput: "4 first-parent commit(s) back",
		},
		{
			name:        "the ancestor's race job failed",
			history:     []historyCommit{base, goCommit("success", "failure"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "concluded \"failure\"",
		},
		{
			// A skipped job on the ancestor is not proof, and the root commit
			// before it is separated from the tag by the Go change the ancestor
			// made.
			name:        "the ancestor's race job was skipped",
			history:     []historyCommit{base, goCommit("success", "skipped"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			name:        "the ancestor's race job is neutral",
			history:     []historyCommit{base, goCommit("success", "neutral"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			name: "the ancestor's race job never reported",
			history: []historyCommit{base, historyCommit{
				suites: map[string]string{"7000001": "merge_group"},
				checks: []checkRun{{"7000001", unitJob, "success"}},
			}.withFile(goFile, "package walk\n"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			// No Go change anywhere in the history and no run of the job in it:
			// the walk runs out of commits rather than of evidence it could read.
			name:        "no commit of the history ran the race job",
			history:     []historyCommit{base, docEdit(1, "7000011"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "history ends after 2 first-parent ancestor(s)",
		},
		{
			// A run of the right event on the right commit with the right job
			// names, from another workflow file, is not the evidence: the walk
			// goes on to the root, which the Go change separates from the tag.
			name:        "the ancestor's job is in a run of another workflow file",
			history:     []historyCommit{base, inRun("merge_group", otherWorkflowFile, ""), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			name:        "the ancestor's job is in a run started by hand",
			history:     []historyCommit{base, inRun("workflow_dispatch", "", ""), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			name:        "the ancestor's job is in a push run on a branch other than main",
			history:     []historyCommit{base, inRun("push", "", "feature/untrusted"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			// A pull request run of ci.yml is no proof for an ancestor either,
			// whatever head branch it lists.
			name:        "the ancestor's job is in a ci.yml pull_request run",
			history:     []historyCommit{base, inRun("pull_request", "", "feature/proposal"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			name:        "the ancestor's job is in a ci.yml pull_request run whose head branch is named main",
			history:     []historyCommit{base, inRun("pull_request", "", "main"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			// The positive controls of the five above: the same ancestor, listed
			// as a push run of ci.yml on main or as a merge-queue run of ci.yml.
			name:       "the ancestor's job is in a push run of ci.yml on main",
			history:    []historyCommit{base, inRun("push", "", ""), release("success", "skipped")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			name:       "the ancestor's job is in a merge-queue run of ci.yml",
			history:    []historyCommit{base, inRun("merge_group", "", ""), release("success", "skipped")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			// A document a Go test reads changed since the ancestor: its unit run
			// did not see the new text, so it is no proof for the unit lane (the
			// race lane, whose inputs are compiled Go, is still taken from it).
			name:        "the unit job is not taken across a document a Go test reads",
			history:     []historyCommit{base, goCommit("success", "success"), release("skipped", "success")},
			wantRefusal: true,
			wantOutput:  "sets run_unit for what changed between",
		},
		{
			// Only a document no test reads changed: both jobs come from the Go
			// commit.
			name: "both jobs are taken across a document no Go test reads",
			history: []historyCommit{base, goCommit("success", "success"),
				tagged("skipped", "skipped").withFile(inertDocFile, "notes\n")},
			wantOutput: "`test-go-shard (1)` concluded success on",
		},
		{
			// The classification is ci.yml's, not a list the gate keeps: the
			// same history with a ci.yml whose detect step forces the unit
			// lane for the page is refused, where the real one accepts it.
			name: "the unit job is not taken across a path the checked-out detect step runs it for",
			history: []historyCommit{
				baseCommit(t, strings.Replace(workflowfile.Read(t, rollingWorkflow),
					`licenses_re='^THIRD_PARTY_LICENSES\.md$'`,
					`licenses_re='^(THIRD_PARTY_LICENSES\.md|docs/walk-fixture-notes\.md)$'`, 1)),
				goCommit("success", "success"),
				tagged("skipped", "skipped").withFile(inertDocFile, "notes\n")},
			wantRefusal: true,
			wantOutput:  "sets run_unit for what changed between",
		},
		{
			// Embedded SQL is compiled into the binary and read by the unit
			// tests: ci.yml's detect step runs the unit lane for it, so the unit
			// job's success on the ancestor is no proof for the tag.
			name:        "the unit job is not taken across embedded SQL",
			history:     []historyCommit{base, goCommit("success", "success"), tagged("skipped", "skipped").withFile(sqlFile, "select 1;\n")},
			wantRefusal: true,
			wantOutput:  "sets run_unit for what changed between",
		},
		{
			// The same for a locale file, whichever commit between carried it
			// and whether or not that commit has a run of its own: the walk
			// compares the ancestor with the tag, not with the commit after it.
			name: "the unit job is not taken across a locale file changed in a commit with no run",
			history: []historyCommit{base, goCommit("success", "success"),
				historyCommit{}.withFile(localeFile, "{}\n"),
				tagged("skipped", "skipped").withFile(inertDocFile, "notes\n")},
			wantRefusal: true,
			wantOutput:  "sets run_unit for what changed between",
		},
		{
			// ci.yml's decision about the race lane is that embedded data cannot
			// introduce a race, so with the unit job proven on the tag itself the
			// race job is taken from the ancestor. The unit job has no such
			// ground: see the cases above.
			name:       "the race job follows the lane filter across embedded SQL",
			history:    []historyCommit{base, goCommit("success", "success"), tagged("success", "skipped").withFile(sqlFile, "select 1;\n")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			name:       "the race job is taken across a readme edit",
			history:    []historyCommit{base, goCommit("success", "success"), tagged("success", "skipped").withFile(readmeFile, "new readme\n")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			// A readme is read by a Go test, so the detect step runs the unit
			// lane for it.
			name:        "the unit job is not taken across a readme edit",
			history:     []historyCommit{base, goCommit("success", "success"), tagged("skipped", "skipped").withFile(readmeFile, "new readme\n")},
			wantRefusal: true,
			wantOutput:  "sets run_unit for what changed between",
		},
		{
			// A commit between with no run at all costs nothing: the ancestor
			// is compared with the tag directly.
			name: "a commit between with no run on record is passed over",
			history: []historyCommit{base, goCommit("success", "success"),
				historyCommit{}.withFile(inertDocFile, "notes\n"),
				release("success", "skipped")},
			wantOutput: "`race-db (1)` concluded success on",
		},
		{
			// No path differs: the ancestor's tree is the tag's, so what ran on it
			// ran on this.
			name:       "an empty commit after the Go commit takes both jobs from it",
			history:    []historyCommit{base, goCommit("success", "success"), tagged("skipped", "skipped")},
			wantOutput: "`test-go-shard (1)` concluded success on",
		},
		{
			// A path holding a newline cannot be listed one per line, which is
			// how the detect step reads its input: neither half of it may be
			// judged on its own.
			name: "a changed path holding a newline is not judged",
			history: []historyCommit{base, goCommit("success", "success"),
				func() historyCommit {
					c := tagged("success", "skipped")
					c.indexPaths = []string{"docs/walk\nfixture.md"}
					return c
				}()},
			wantRefusal: true,
			wantOutput:  "holds a newline",
		},
		{
			name:        "the runs of the nearest ancestor cannot be read",
			history:     []historyCommit{base, goCommit("success", "success").withUnreadable("runs"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "the runs of ",
		},
		{
			name:        "the jobs of the nearest ancestor's runs cannot be read",
			history:     []historyCommit{base, goCommit("success", "success").withUnreadable("jobs"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "the runs of ",
		},
		{
			// The finding. The nearest Go commit has no job that succeeded: its
			// runs list the two jobs `skipped`, and somebody holding `checks:
			// write` posted a `success` under each name. The Checks API would
			// return them; the walk reads the run's jobs and refuses.
			name: "the ancestor's jobs succeeded only as forged check runs",
			history: []historyCommit{base,
				func() historyCommit {
					c := ran("7000001", "skipped", "skipped").withFile(goFile, "package walk\n")
					c.forged = []checkRun{{"7000001", unitJob, "success"}, {"7000001", raceJob, "success"}}
					return c
				}(),
				release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "sets run_race for what changed between",
		},
		{
			// A failure on the ancestor's run is not erased by a forged success.
			name: "the ancestor's race job failed and a forged check run says success",
			history: []historyCommit{base,
				func() historyCommit {
					c := goCommit("success", "failure")
					c.forged = []checkRun{{"7000001", raceJob, "success"}}
					return c
				}(),
				release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "concluded \"failure\"",
		},
		{
			name:        "ci.yml no longer has the detect step",
			history:     []historyCommit{baseCommit(t, "name: CI\n"), goCommit("success", "success"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "could not be cut out",
		},
		{
			name: "the detect step fails",
			history: []historyCommit{baseCommit(t, detectStepStub("echo broken >&2", "exit 1")),
				goCommit("success", "success"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "detect step failed",
		},
		{
			name: "the detect step answers neither true nor false",
			history: []historyCommit{baseCommit(t, detectStepStub(`echo "run_race=maybe" >> "$GITHUB_OUTPUT"`)),
				goCommit("success", "success"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "neither true nor false",
		},
		{
			name: "the detect step writes no answer for the lane",
			history: []historyCommit{baseCommit(t, detectStepStub(`echo "run_unit=false" >> "$GITHUB_OUTPUT"`)),
				goCommit("success", "success"), release("success", "skipped")},
			wantRefusal: true,
			wantOutput:  "wrote run_race as ''",
		},
		{
			name:        "a check the gate knows no lane output for",
			history:     []historyCommit{base, goCommit("success", "success"), release("success", "skipped")},
			env:         map[string]string{"WORK_CHECKS": "test-go-shard (1)|race-db (1)|lint (1)"},
			wantRefusal: true,
			wantOutput:  "no output of ci.yml's detect step is known for `lint (1)`",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runWalk(t, testCase)

			if testCase.wantRefusal && err == nil {
				t.Fatalf("the gate published this tag and owed a refusal.\n%s", output)
			}
			if !testCase.wantRefusal && err != nil {
				t.Fatalf("the gate refused a tag it owes: %v\n%s", err, output)
			}
			for _, stubAbort := range []string{stubUnservedEndpoint, stubUnexpectedGit} {
				if strings.Contains(output, stubAbort) {
					t.Fatalf("the outcome came from a harness stub (%q), not from the gate's own judgement.\n%s", stubAbort, output)
				}
			}
			if strings.Contains(output, stubChecksAPIRead) {
				t.Fatalf("the gate asked the Checks API for something, and no check run is proof.\n%s", output)
			}
			if !strings.Contains(output, testCase.wantOutput) {
				t.Fatalf("the gate's output does not carry %q, so the verdict was reached for another reason.\n%s", testCase.wantOutput, output)
			}
		})
	}
}

// runWalk builds the history in a real repository, serves each commit's CI
// facts from files keyed by sha, and runs the gate's real script at the last
// commit.
func runWalk(t *testing.T, state walkCase) (string, error) {
	t.Helper()

	testenv.RequireLookPath(t, "git", "git")
	bash := bashPath(t)
	requireWorkingBash(t, bash)

	root := t.TempDir()
	emptyConfig := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(emptyConfig, nil, 0o600); err != nil {
		t.Fatalf("write the empty git config: %v", err)
	}
	environ := append(withoutGitEnv(os.Environ()),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+filepath.ToSlash(emptyConfig),
		"GIT_AUTHOR_NAME=releasegate", "GIT_AUTHOR_EMAIL=releasegate@example.invalid",
		"GIT_COMMITTER_NAME=releasegate", "GIT_COMMITTER_EMAIL=releasegate@example.invalid",
	)
	repo := &ancestryRepo{env: environ}

	work := filepath.Join(root, "work")
	data := filepath.Join(root, "data")
	for _, dir := range []string{work, data} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	repo.git(t, root, "init", "-q", "-b", "main", work)

	serve := func(name, content string) {
		if err := os.WriteFile(filepath.Join(data, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// A job row is (run id, name, conclusion, attempt): the jobs endpoint's
	// projection, one file per run, since the gate lists the jobs of one run at
	// a time. A run id belongs to one commit, or one file would answer for two.
	jobFile := func(run string, checks []checkRun) {
		var out strings.Builder
		for _, row := range checks {
			if row.suite == run {
				out.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\t1\n")
			}
		}
		serve("jobs_"+run, out.String())
	}
	runOwner := map[string]int{}

	var sha string
	for index, commit := range state.history {
		for path, content := range commit.files {
			full := filepath.Join(work, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("create the directory of %s: %v", path, err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
		repo.git(t, work, "add", "-A")
		for _, path := range commit.indexPaths {
			blob := repo.git(t, work, "hash-object", "-w", "--stdin")
			// protectNTFS refuses such a name on Windows; the history is only read.
			unprotected := &ancestryRepo{env: append(append([]string{}, environ...),
				"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.protectNTFS", "GIT_CONFIG_VALUE_0=false")}
			unprotected.git(t, work, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path)
		}
		repo.git(t, work, "commit", "-q", "--allow-empty", "-m", "commit "+strconv.Itoa(index))
		sha = repo.git(t, work, "rev-parse", "HEAD")

		var runs strings.Builder
		for _, suite := range sortedKeys(commit.suites) {
			branch := "main"
			if commit.suites[suite] == "merge_group" {
				branch = queueRunBranch
			}
			if override, ok := commit.suiteBranches[suite]; ok {
				branch = override
			}
			file := ciYamlPath
			if override, ok := commit.suiteFiles[suite]; ok {
				file = override
			}
			runs.WriteString(commit.suites[suite] + "\t" + suite + "\t" + branch + "\t" + file + "\n")
		}
		serve("runs_"+sha, runs.String())

		var forged strings.Builder
		for _, row := range commit.forged {
			forged.WriteString(row.suite + "\t" + row.name + "\t" + row.conclusion + "\n")
		}
		serve("forged_"+sha, forged.String())

		failing := func(kind, key string) {
			serve("fail_"+kind+"_"+key, "")
		}
		for _, run := range sortedKeys(commit.suites) {
			if owner, taken := runOwner[run]; taken {
				t.Fatalf("run id %s is on commit %d and on commit %d of the history: the fixture would serve one commit's jobs for the other", run, owner, index)
			}
			runOwner[run] = index
			jobFile(run, commit.checks)
			for _, kind := range commit.unreadable {
				if kind == "jobs" {
					failing("jobs", run)
				}
			}
		}
		for _, kind := range commit.unreadable {
			if kind == "runs" {
				failing("runs", sha)
			}
		}
	}

	preamble := strings.Join([]string{
		// One read: a `fail_<kind>_<key>` file makes the call fail the way an API
		// outage does, any other answers from `<kind>_<key>` and, with no such file,
		// with nothing.
		`reply() {`,
		`  if [ -e ` + shellQuote(filepath.ToSlash(data)) + `"/fail_$1_$2" ]; then echo "the harness made this read fail: $1 $2" >&2; return 1; fi`,
		`  cat ` + shellQuote(filepath.ToSlash(data)) + `"/$1_$2" 2>/dev/null || true`,
		`}`,
		`gh() {`,
		`  url=""`,
		`  for arg in "$@"; do case "$arg" in repos/*) url="$arg";; esac; done`,
		`  case "$url" in`,
		`    */actions/runs/*/jobs*) key="${url#*/actions/runs/}"; key="${key%%/*}"; reply jobs "$key" ;;`,
		`    */actions/runs?head_sha=*) key="${url#*head_sha=}"; key="${key%%&*}"; reply runs "$key" ;;`,
		`    */commits/*/check-runs*) echo "` + stubChecksAPIRead + `: $url" >&2; key="${url#*/commits/}"; key="${key%%/*}"; reply forged "$key" ;;`,
		`    *) echo "` + stubUnservedEndpoint + `: $*" >&2; return 1 ;;`,
		`  esac`,
		`}`,
		"",
	}, "\n")

	env := stepEnv(t)
	if state.bound != "" {
		env["ANCESTOR_WALK_BOUND"] = state.bound
	}
	for key, value := range state.env {
		env[key] = value
	}
	for key, value := range env {
		environ = append(environ, key+"="+value)
	}
	environ = append(environ,
		"GITHUB_SHA="+sha,
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"GITHUB_REF_NAME=v2.0.0",
		"GH_TOKEN=stub",
	)
	return runStepScript(t, bash, gateStep, stepBlock(t), preamble+stepScript(t), work, environ)
}

// cutDetectStep runs the gate's own `cut_detect_step` function over the given
// ci.yml text, as the gate runs it on the runner (in the directory holding
// .github/workflows/ci.yml), and returns what it cut out.
func cutDetectStep(t *testing.T, ciYaml string) string {
	t.Helper()

	script := stepScript(t)
	start := strings.Index(script, "\ncut_detect_step() {\n")
	if start < 0 {
		t.Fatalf("%s, step %q no longer defines cut_detect_step: the walk has no way to run ci.yml's detect step", gateWorkflow, gateStep)
	}
	length := strings.Index(script[start+1:], "\n}\n")
	if length < 0 {
		t.Fatalf("%s, step %q: cut_detect_step has no closing brace at the start of a line", gateWorkflow, gateStep)
	}
	function := script[start+1 : start+1+length+3]

	bash := bashPath(t)
	requireWorkingBash(t, bash)

	dir := t.TempDir()
	workflows := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0o755); err != nil {
		t.Fatalf("create the workflow directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflows, "ci.yml"), []byte(ciYaml), 0o600); err != nil {
		t.Fatalf("write ci.yml: %v", err)
	}
	driver := filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(driver, []byte(function+"\ncut_detect_step \"$1\"\n"), 0o600); err != nil {
		t.Fatalf("write the driver: %v", err)
	}
	cut := filepath.Join(dir, "detect.sh")

	command := exec.Command(bash, filepath.ToSlash(driver), filepath.ToSlash(cut))
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cut_detect_step failed on the real ci.yml: %v\n%s", err, output)
	}
	cutText, err := os.ReadFile(cut)
	if err != nil {
		t.Fatalf("read what was cut: %v", err)
	}
	return string(cutText)
}

// TestTheWalkRunsTheDetectStepItselfFromCiYml holds the single definition: the
// release gate decides which paths run a Go lane by running ci.yml's detect step
// over them, not by matching a list of its own. The step it cuts out of the file
// must be the step's own text, so a re-indented or renamed step reddens here
// rather than at a release, and nothing the step filters with may be spelled in
// the gate.
func TestTheWalkRunsTheDetectStepItselfFromCiYml(t *testing.T) {
	ci := workflowfile.Read(t, rollingWorkflow)
	detect := workflowfile.Step(t, rollingWorkflow, changesJob, workflowfile.Job(t, rollingWorkflow, changesJob), detectStepName)
	gate := workflowfile.Read(t, gateWorkflow)

	normalise := func(text string) string {
		return strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n ")
	}
	want := normalise(runScript(t, detectStepName, detect))
	if got := normalise(cutDetectStep(t, ci)); got != want {
		t.Errorf("%s: the text the gate cuts out of the detect step is not the step's own `run:` block (%d bytes against %d)", gateWorkflow, len(got), len(want))
	}

	var patterns []string
	named := regexp.MustCompile(`(?m)^\s*[a-z_]+_re='(.*)'\s*$`).FindAllStringSubmatch(detect, -1)
	if len(named) == 0 {
		t.Fatalf("%s: the detect step holds no `name_re='…'` pattern line; this guard would check nothing", rollingWorkflow)
	}
	for _, match := range named {
		patterns = append(patterns, match[1])
	}
	inline := regexp.MustCompile(`grep -v?E '([^']{12,})'`).FindAllStringSubmatch(detect, -1)
	if len(inline) == 0 {
		t.Fatalf("%s: the detect step holds no inline grep pattern; this guard would check nothing", rollingWorkflow)
	}
	for _, match := range inline {
		patterns = append(patterns, match[1])
	}
	for _, pattern := range patterns {
		if strings.Contains(gate, pattern) {
			t.Errorf("%s carries its own copy of the detect step's pattern %q: the gate runs the step and reads its answer", gateWorkflow, pattern)
		}
	}
}

// TestTheTagGateJobHoldsExactlyTheReadScopesItNeeds pins the job's permissions:
// the caller-ceiling guard checks one direction only (what the job asks for may
// not exceed what ci.yml grants), so a scope dropped from the job — or widened
// to write — would pass it.
func TestTheTagGateJobHoldsExactlyTheReadScopesItNeeds(t *testing.T) {
	got := declaredPermissions(t, workflowfile.Job(t, gateWorkflow, gateJob))
	want := map[string]string{"actions": "read", "contents": "read"}

	if len(got) != len(want) {
		t.Errorf("%s, job %q declares %v, want exactly %v", gateWorkflow, gateJob, got, want)
	}
	for scope, level := range want {
		if got[scope] != level {
			t.Errorf("%s, job %q declares `%s: %s`, want `%s: %s`: the step reads runs and their jobs (`actions`) and the checked-out history (`contents`), and no check run (`checks`) and no pull request", gateWorkflow, gateJob, scope, orNone(got[scope]), scope, level)
		}
	}
}
