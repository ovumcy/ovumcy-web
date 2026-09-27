package releasegate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/testenv"
	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The step that refuses a tag whose commit `main` does not contain, and the
// checkout it depends on. Looked up by name: a removed or renamed step fails
// the suite instead of leaving these cases judging nothing.
const (
	ancestryStep = "Assert the tagged commit is contained in main"
	checkoutStep = "Checkout"
)

// ancestryRepo is a real origin and a real full clone of it, shaped the way a
// release tag can arrive: on `main`'s tip, behind it, or on a branch `main`
// never merged.
type ancestryRepo struct {
	clone string
	env   []string
	// The commits and tag objects the cases point GITHUB_SHA at.
	mainOld, mainTip, offMain         string
	annotatedOnMain, annotatedOffMain string
}

// TestReleaseTagOffMainIsRefused runs the ancestry step's REAL script, under
// the flags its `shell:` compiles to, against a real repository. Each case
// pins one way the step could be weakened and still look like a check:
//
//   - `|| true`, a dropped `exit 1`, or the step deleted: the off-main tag
//     passes.
//   - merge-base's operands swapped: a tag behind `main`'s tip is refused.
//   - a string search through `git log` instead of the commit graph: `main`
//     here carries a revert whose message names the off-main commit, so the
//     search finds it and the off-main tag passes.
//   - trusting the checkout's `origin/main` instead of the explicit forced
//     fetch: the clone's `origin/main` is planted on the off-main commit.
func TestReleaseTagOffMainIsRefused(t *testing.T) {
	repo := newAncestryRepo(t)

	for _, testCase := range []struct {
		name        string
		sha         string
		staleMain   string
		wantRefusal bool
	}{
		{name: "lightweight tag on main's tip", sha: repo.mainTip},
		{name: "lightweight tag behind main's tip", sha: repo.mainOld},
		{name: "annotated tag behind main's tip", sha: repo.annotatedOnMain},
		{name: "lightweight tag on a branch main never merged", sha: repo.offMain, wantRefusal: true},
		{name: "annotated tag on a branch main never merged", sha: repo.annotatedOffMain, wantRefusal: true},
		{
			name:        "off-main tag with the checkout's origin/main planted on it",
			sha:         repo.annotatedOffMain,
			staleMain:   repo.offMain,
			wantRefusal: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.staleMain != "" {
				repo.git(t, repo.clone, "update-ref", "refs/remotes/origin/main", testCase.staleMain)
				t.Cleanup(func() { repo.git(t, repo.clone, "update-ref", "refs/remotes/origin/main", repo.mainTip) })
			}

			output, err := repo.runAncestryStep(t, testCase.sha)
			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("the ancestry step let a tag on a commit main does not contain through:\n%s", output)
				}
				if !strings.Contains(output, "::error::") || !strings.Contains(output, "not contained in main") {
					t.Fatalf("the ancestry step failed, but not with its refusal — a harness or git failure is not a verdict (exit: %v):\n%s", err, output)
				}
				return
			}
			if err != nil || !strings.Contains(output, "contained in main") {
				t.Fatalf("the ancestry step refused a tag on a commit main contains (exit: %v):\n%s", err, output)
			}
		})
	}
}

// TestAncestryStepRunsOnAFullHistory pins the checkout the step reads. On the
// default depth of 1 there is no history for merge-base to walk: every tag
// behind `main`'s tip would be refused, and the pressure to "fix" that is
// exactly the weakening the step exists to prevent.
func TestAncestryStepRunsOnAFullHistory(t *testing.T) {
	job := workflowfile.Job(t, gateWorkflow, gateJob)
	checkout := workflowfile.Step(t, gateWorkflow, gateJob, job, checkoutStep)
	if !strings.Contains(checkout, "fetch-depth: 0") {
		t.Fatalf("%s, job %q, step %q: no `fetch-depth: 0`; the ancestry check has no history to walk on a shallow clone:\n%s",
			gateWorkflow, gateJob, checkoutStep, checkout)
	}

	steps := workflowfile.Steps(job)
	checkoutAt, ancestryAt := -1, -1
	for i, step := range steps {
		switch {
		case strings.Contains(step, "name: "+checkoutStep+"\n"):
			checkoutAt = i
		case strings.Contains(step, "name: "+ancestryStep+"\n"):
			ancestryAt = i
		}
	}
	if checkoutAt < 0 || ancestryAt < 0 || ancestryAt < checkoutAt {
		t.Fatalf("%s, job %q: step %q must run after %q (found at %d and %d)",
			gateWorkflow, gateJob, ancestryStep, checkoutStep, ancestryAt, checkoutAt)
	}
}

// newAncestryRepo builds the fixture:
//
//	main:  root ── old ── revert-mention (tip)
//	                 \
//	side:             └── off-main
//
// The tip's message names the off-main commit, as `git revert` of a
// cherry-pick would.
func newAncestryRepo(t *testing.T) *ancestryRepo {
	t.Helper()

	testenv.RequireLookPath(t, "git", "git")

	root := t.TempDir()
	emptyConfig := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(emptyConfig, nil, 0o600); err != nil {
		t.Fatalf("write the empty git config: %v", err)
	}
	repo := &ancestryRepo{
		env: append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL="+filepath.ToSlash(emptyConfig),
			"GIT_AUTHOR_NAME=releasegate", "GIT_AUTHOR_EMAIL=releasegate@example.invalid",
			"GIT_COMMITTER_NAME=releasegate", "GIT_COMMITTER_EMAIL=releasegate@example.invalid",
		),
	}

	origin := filepath.Join(root, "origin.git")
	author := filepath.Join(root, "author")
	repo.clone = filepath.Join(root, "runner")

	repo.git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	repo.git(t, root, "init", "-q", "-b", "main", author)
	repo.git(t, author, "remote", "add", "origin", filepath.ToSlash(origin))

	commit := func(message string) string {
		repo.git(t, author, "commit", "-q", "--allow-empty", "-m", message)
		return repo.git(t, author, "rev-parse", "HEAD")
	}

	commit("root")
	repo.mainOld = commit("old")

	repo.git(t, author, "switch", "-q", "-c", "side")
	repo.offMain = commit("off-main")
	repo.git(t, author, "switch", "-q", "main")
	repo.mainTip = commit("Revert \"off-main\"\n\nThis reverts commit " + repo.offMain + ".")

	repo.git(t, author, "tag", "-a", "-m", "on main", "v1.0.0", repo.mainOld)
	repo.git(t, author, "tag", "-a", "-m", "off main", "v1.0.1", repo.offMain)
	repo.annotatedOnMain = repo.git(t, author, "rev-parse", "v1.0.0")
	repo.annotatedOffMain = repo.git(t, author, "rev-parse", "v1.0.1")
	if repo.annotatedOnMain == repo.mainOld || repo.annotatedOffMain == repo.offMain {
		t.Fatal("the annotated tags resolve to their commits; the dereference cases would test nothing")
	}

	repo.git(t, author, "push", "-q", "origin", "main", "side", "--tags")
	repo.git(t, root, "clone", "-q", filepath.ToSlash(origin), repo.clone)
	return repo
}

func (r *ancestryRepo) git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = r.env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// runAncestryStep runs the step as the runner does — a file, under the flags
// its `shell:` compiles to — inside the clone, with GITHUB_SHA at sha.
func (r *ancestryRepo) runAncestryStep(t *testing.T, sha string) (string, error) {
	t.Helper()

	block := workflowfile.Step(t, gateWorkflow, gateJob, workflowfile.Job(t, gateWorkflow, gateJob), ancestryStep)
	script := runScript(t, ancestryStep, block)
	if !strings.Contains(script, "merge-base --is-ancestor") {
		t.Fatalf("%s, step %q no longer asks merge-base --is-ancestor; reachability is a commit-graph question:\n%s",
			gateWorkflow, ancestryStep, script)
	}

	bash := bashPath(t)
	requireWorkingBash(t, bash)

	scriptFile := filepath.Join(t.TempDir(), "ancestry.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0o644); err != nil {
		t.Fatalf("write the ancestry script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	flags := workflowfile.BashStepFlags(t, gateWorkflow, ancestryStep, block)
	command := exec.CommandContext(ctx, bash, append(flags, filepath.ToSlash(scriptFile))...)
	command.Dir = r.clone
	command.Env = append(append([]string{}, r.env...),
		"GITHUB_SHA="+sha,
		"GITHUB_REF_NAME=v1.0.0",
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
