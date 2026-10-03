package publishorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const (
	tipJob  = "confirm-main-tip"
	tipStep = "Stop when a newer commit is already on main"

	// tipCondition is the job's own `if:`, as pinned: the job exists for the
	// alias path, and a release tag has no tip to be the newest of.
	tipCondition = "github.ref_type != 'tag'"
)

// TestTheTipJobRunsOnTheAliasPathAndFailsClosed pins what running the step
// cannot see: the runner, not bash, honours `if:` and `continue-on-error:`, and
// the job's own scope. A step that failed open would let a commit through on an
// API that did not answer, and a job that ran on a tag would ask a question a
// release has no answer to.
func TestTheTipJobRunsOnTheAliasPathAndFailsClosed(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, tipJob)

	got, err := workflowfile.JobCondition(job)
	if err != nil {
		t.Fatalf("%s, job %q: the job-level `if:` cannot be read (%v); want exactly `${{ %s }}`:\n%s", publishWorkflow, tipJob, err, tipCondition, job)
	}
	if workflowfile.WithoutSpaces(got) != workflowfile.WithoutSpaces(tipCondition) {
		t.Errorf("%s, job %q: the job-level `if:` reads %q; want exactly `${{ %s }}`", publishWorkflow, tipJob, workflowfile.WithoutSpaces(got), tipCondition)
	}

	if found := workflowfile.JobFailOpenKeys(job); len(found) > 0 {
		t.Errorf("%s, job %q carries %q: a failed tip read would no longer stop the publish", publishWorkflow, tipJob, found)
	}
	step := workflowfile.Step(t, publishWorkflow, tipJob, job, tipStep)
	if found := workflowfile.StepFailOpenKeys(step); len(found) > 0 {
		t.Errorf("%s, job %q, step %q carries %q: its refusal would no longer stop the publish", publishWorkflow, tipJob, tipStep, found)
	}

	// The job reads one ref and one comparison; contents: read is the whole of
	// what that takes, and anything wider belongs on a job that needs it.
	if !strings.Contains(job, "\n    permissions:\n      contents: read\n") {
		t.Errorf("%s, job %q does not declare exactly `permissions: contents: read`:\n%s", publishWorkflow, tipJob, job)
	}
}

// TestTheTipStepStopsOnlyOnAStrictlyNewerCommit runs the step for real against
// a stubbed `gh`. The property: the answer is `false` only on positive evidence
// that main's tip descends from this commit, and `true` on every other answer
// GitHub can give — the publish job's own freshness check judges those — while
// an unreadable tip or comparison fails the job and writes no answer at all.
func TestTheTipStepStopsOnlyOnAStrictlyNewerCommit(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, tipJob)
	bash := requireBash(t)
	script := stepScript(t, job, tipStep)

	history := strings.Join([]string{oldCommit, midCommit, newCommit}, " ")

	for _, testCase := range []struct {
		name string
		// run is the commit this publish is for; tip is what main points at.
		run, tip string
		// refFails and compareFails make the corresponding read unanswerable.
		refFails, compareFails bool
		// wantOutput is the step's `is_tip` answer; empty means none may be
		// written, which is what a failing step owes.
		wantOutput string
		// wantError is the step's own message for the branch this case is about.
		wantError string
		// wantCompare is the exact comparison asked, `<run>...<tip>`; empty
		// means none may be asked.
		wantCompare string
		// wantReads is how many times the ref must be read.
		wantReads int
		// wantMessage is text the passing step must say.
		wantMessage string
	}{
		{
			name:        "this commit is the tip",
			run:         midCommit,
			tip:         midCommit,
			wantOutput:  "true",
			wantReads:   1,
			wantMessage: "is the tip of main",
		},
		{
			// The measured shape: run 36782607690 was for the ancestor
			// 64e4361c while its descendant had published.
			name:        "main has moved on to a descendant",
			run:         oldCommit,
			tip:         newCommit,
			wantOutput:  "false",
			wantCompare: oldCommit + "..." + newCommit,
			wantReads:   1,
			wantMessage: "is not the newest commit on main",
		},
		{
			name:        "the tip is a distant descendant, not the next commit",
			run:         midCommit,
			tip:         newCommit,
			wantOutput:  "false",
			wantCompare: midCommit + "..." + newCommit,
			wantReads:   1,
			wantMessage: "is not the newest commit on main",
		},
		{
			// A stale read of the ref: the tip is behind this commit. Not a newer
			// commit, so the run goes on to the freshness check.
			name:        "the tip is behind this commit",
			run:         newCommit,
			tip:         oldCommit,
			wantOutput:  "true",
			wantCompare: newCommit + "..." + oldCommit,
			wantReads:   1,
			wantMessage: "'behind'",
		},
		{
			name:        "the tip and this commit have diverged",
			run:         elsewhereCommit,
			tip:         newCommit,
			wantOutput:  "true",
			wantCompare: elsewhereCommit + "..." + newCommit,
			wantReads:   1,
			wantMessage: "'diverged'",
		},
		{
			name:        "the ref cannot be read",
			run:         oldCommit,
			tip:         newCommit,
			refFails:    true,
			wantError:   "tip of main could not be read",
			wantReads:   3,
			wantOutput:  "",
			wantCompare: "",
		},
		{
			name:        "the ref names something that is not a commit",
			run:         oldCommit,
			tip:         "refs/heads/main",
			wantError:   "tip of main could not be read",
			wantReads:   3,
			wantOutput:  "",
			wantCompare: "",
		},
		{
			name:         "GitHub cannot compare the two commits",
			run:          oldCommit,
			tip:          newCommit,
			compareFails: true,
			wantError:    "could not compare",
			wantReads:    1,
			wantOutput:   "",
			wantCompare:  oldCommit + "..." + newCommit,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := filepath.ToSlash(t.TempDir())
			outputFile := dir + "/github_output"
			if err := os.WriteFile(outputFile, nil, 0o600); err != nil {
				t.Fatalf("create the step output file: %v", err)
			}

			command := runBashScript(t, bash, job, tipStep, tipStub()+"\n"+script)
			command.Env = append(os.Environ(),
				"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
				"GITHUB_SHA="+testCase.run,
				"GITHUB_OUTPUT="+outputFile,
				"GH_TOKEN=stub",
				"STUB_DIR="+dir,
				"STUB_HISTORY="+history,
				"STUB_TIP="+testCase.tip,
				"STUB_REF_FAILS="+boolDigit(testCase.refFails),
				"STUB_COMPARE_FAILS="+boolDigit(testCase.compareFails),
			)

			outputBytes, runErr := command.CombinedOutput()
			output := string(outputBytes)

			calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
			reads, compares := 0, 0
			for _, line := range strings.Split(string(calls), "\n") {
				switch {
				case strings.HasPrefix(line, "GH api repos/ovumcy/ovumcy-web/git/ref/heads/main "):
					reads++
				case strings.HasPrefix(line, "GH api repos/ovumcy/ovumcy-web/compare/"):
					compares++
					if want := "GH api repos/ovumcy/ovumcy-web/compare/" + testCase.wantCompare + " --jq .status"; testCase.wantCompare == "" || line != want {
						t.Fatalf("the step asked GitHub %q, want %q (base is THIS run's commit, head is main's tip)", line, want)
					}
				case strings.HasPrefix(line, "GH "):
					t.Fatalf("the step asked GitHub something this fixture does not serve: %q", line)
				}
			}
			if reads != testCase.wantReads {
				t.Fatalf("the step read the ref %d times, want %d:\n%s", reads, testCase.wantReads, calls)
			}
			if testCase.wantCompare != "" && compares != 1 {
				t.Fatalf("the step asked for the comparison %s %d times, want exactly once:\n%s", testCase.wantCompare, compares, calls)
			}

			answer, readErr := os.ReadFile(outputFile)
			if readErr != nil {
				t.Fatalf("read the step output file: %v", readErr)
			}

			if testCase.wantError != "" {
				if runErr == nil {
					t.Fatalf("the step let this run proceed and owed a refusal.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				if strings.TrimSpace(string(answer)) != "" {
					t.Fatalf("a failing step wrote an answer the publish job could read: %q", answer)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("the step failed a run it owes an answer: %v\n%s", runErr, output)
			}
			if got, want := strings.TrimSpace(string(answer)), "is_tip="+testCase.wantOutput; got != want {
				t.Fatalf("the step answered %q, want %q:\n%s", got, want, output)
			}
			if !strings.Contains(output, testCase.wantMessage) {
				t.Fatalf("the step passed without saying %q:\n%s", testCase.wantMessage, output)
			}
		})
	}
}

func boolDigit(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// tipStub shadows `gh` and `sleep`. The ref read answers the tip the fixture
// supplies; the comparison is computed from a history order, so its verdict
// follows the direction the step asked in — a step that swapped base and head
// would be answered the opposite verdict by the same history.
func tipStub() string {
	return strings.Join([]string{
		`sleep() { :; }`,
		`gh() {`,
		`  printf 'GH %s\n' "$*" >> "$STUB_DIR/calls.log"`,
		`  case "$2" in`,
		`    */git/ref/heads/main)`,
		`      if [ "$STUB_REF_FAILS" = 1 ]; then echo "gh: HTTP 404" >&2; return 1; fi`,
		`      printf '%s\n' "$STUB_TIP" ;;`,
		`    */compare/*)`,
		`      if [ "$STUB_COMPARE_FAILS" = 1 ]; then echo "gh: HTTP 404" >&2; return 1; fi`,
		`      local range="${2##*/compare/}" base head i=0 ib=-1 ih=-1 c`,
		`      base="${range%%...*}"; head="${range##*...}"`,
		`      for c in $STUB_HISTORY; do`,
		`        if [ "$c" = "$base" ]; then ib=$i; fi`,
		`        if [ "$c" = "$head" ]; then ih=$i; fi`,
		`        i=$(( i + 1 ))`,
		`      done`,
		`      if [ "$base" = "$head" ]; then echo identical`,
		`      elif [ "$ib" -lt 0 ] || [ "$ih" -lt 0 ]; then echo diverged`,
		`      elif [ "$ib" -lt "$ih" ]; then echo ahead`,
		`      else echo behind; fi ;;`,
		`    *) echo "the step called an endpoint this fixture does not serve: $2" >&2; return 1 ;;`,
		`  esac`,
		`}`,
	}, "\n")
}
