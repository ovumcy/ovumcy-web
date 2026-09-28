package releasegate

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	mainOld, mainTip, offMain                            string
	annotatedOnTip, annotatedBehindTip, annotatedOffMain string
}

// TestReleaseTagPassesOnlyWhenMainContainsIt runs the ancestry step's REAL
// script, under the flags its `shell:` compiles to, against a real repository.
// The acceptances pin the normal release shapes; each refusal pins one way the
// step could be weakened and still look like a check:
//
//   - `|| true`, a dropped `exit 1`, or the step deleted: the off-main tag
//     passes.
//   - merge-base's operands swapped: a tag behind `main`'s tip is refused.
//   - a string search through `git log` instead of the commit graph: `main`
//     here carries a revert whose message names the off-main commit, so the
//     search finds it and the off-main tag passes.
//   - trusting the checkout's `origin/main` instead of the explicit forced
//     fetch: the clone's `origin/main` is planted on the off-main commit.
func TestReleaseTagPassesOnlyWhenMainContainsIt(t *testing.T) {
	repo := newAncestryRepo(t)

	bash := bashPath(t)
	requireWorkingBash(t, bash)
	block := workflowfile.Step(t, gateWorkflow, gateJob, workflowfile.Job(t, gateWorkflow, gateJob), ancestryStep)
	script := runScript(t, ancestryStep, block)
	if !strings.Contains(script, "merge-base --is-ancestor") {
		t.Fatalf("%s, step %q no longer asks merge-base --is-ancestor; reachability is a commit-graph question:\n%s",
			gateWorkflow, ancestryStep, script)
	}

	for _, testCase := range []struct {
		name        string
		sha         string
		staleMain   string
		wantRefusal bool
	}{
		{name: "lightweight tag on main's tip", sha: repo.mainTip},
		{name: "annotated tag on main's tip", sha: repo.annotatedOnTip},
		{name: "lightweight tag behind main's tip", sha: repo.mainOld},
		{name: "annotated tag behind main's tip", sha: repo.annotatedBehindTip},
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

			environ := append(slices.Clone(repo.env), "GITHUB_SHA="+testCase.sha, "GITHUB_REF_NAME=v1.0.0")
			output, err := runStepScript(t, bash, ancestryStep, block, script, repo.clone, environ)
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
// exactly the weakening the step exists to prevent. Comments are skipped: the
// step's own rationale may quote the key it no longer carries.
func TestAncestryStepRunsOnAFullHistory(t *testing.T) {
	job := workflowfile.Job(t, gateWorkflow, gateJob)
	checkout := workflowfile.Step(t, gateWorkflow, gateJob, job, checkoutStep)

	fullHistory := false
	for _, line := range strings.Split(checkout, "\n") {
		if strings.TrimSpace(line) == "fetch-depth: 0" {
			fullHistory = true
		}
	}
	if !fullHistory {
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
		env: append(withoutGitEnv(os.Environ()),
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

	repo.git(t, author, "switch", "-q", "--create", "side")
	repo.offMain = commit("off-main")
	repo.git(t, author, "switch", "-q", "main")
	repo.mainTip = commit("Revert \"off-main\"\n\nThis reverts commit " + repo.offMain + ".")

	repo.git(t, author, "tag", "-a", "-m", "on main", "v1.0.0", repo.mainOld)
	repo.git(t, author, "tag", "-a", "-m", "off main", "v1.0.1", repo.offMain)
	repo.git(t, author, "tag", "-a", "-m", "tip", "v1.0.2", repo.mainTip)
	repo.annotatedBehindTip = repo.git(t, author, "rev-parse", "v1.0.0")
	repo.annotatedOffMain = repo.git(t, author, "rev-parse", "v1.0.1")
	repo.annotatedOnTip = repo.git(t, author, "rev-parse", "v1.0.2")
	for tag, commit := range map[string]string{
		repo.annotatedBehindTip: repo.mainOld,
		repo.annotatedOffMain:   repo.offMain,
		repo.annotatedOnTip:     repo.mainTip,
	} {
		if tag == commit {
			t.Fatal("an annotated tag resolves to its commit; the annotated cases would test nothing")
		}
	}

	repo.git(t, author, "push", "-q", "origin", "main", "side", "--tags")
	repo.git(t, root, "clone", "-q", filepath.ToSlash(origin), repo.clone)
	return repo
}

// withoutGitEnv drops every GIT_* variable. A git hook exports GIT_DIR and
// GIT_INDEX_FILE, and either one outranks the working directory: a suite run
// from a hook would otherwise commit, tag and push into the repository under
// test rather than the fixture.
func withoutGitEnv(environ []string) []string {
	return slices.DeleteFunc(slices.Clone(environ), func(entry string) bool {
		return strings.HasPrefix(strings.ToUpper(entry), "GIT_")
	})
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
