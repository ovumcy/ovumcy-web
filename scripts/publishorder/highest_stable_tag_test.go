package publishorder

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// "Is this tag the highest stable release?" is asked in five places — the
// publish run's decision step, its two re-checks before a write of `latest`,
// and the promotion's refusal and re-check — and all five run one checked-in
// script. This file drives that script itself, and holds that the five sites
// (and any site added later) use it and have the repository checked out when
// they do.

const highestTagScript = "scripts/ci/highest-stable-tag.sh"

// highestTagExports makes the functions a test shadows `gh` and `sleep` with
// visible to the script, which a step runs in a child bash.
const highestTagExports = `export -f gh sleep`

// The steps run with the repository as their working tree, as on a runner;
// they reach the script through the variable the runner sets for it.
func init() {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("GITHUB_WORKSPACE", filepath.ToSlash(root)); err != nil {
		panic(err)
	}
}

// runHighestStableTag runs the script itself with `gh` and `sleep` shadowed
// (ghStub) and `preamble` after them, and returns its stdout, its stderr and
// its exit status.
func runHighestStableTag(t *testing.T, args []string, preamble string, env map[string]string) (string, string, int) {
	t.Helper()

	bash := requireBash(t)
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "wrapper.sh")
	if err := os.WriteFile(wrapper, []byte(ghStub+"\n"+preamble+"\nbash \"$HIGHEST_TAG_SCRIPT\" \"$@\"\n"), 0o600); err != nil {
		t.Fatalf("write the wrapper: %v", err)
	}

	command := exec.Command(bash, append([]string{filepath.ToSlash(wrapper)}, args...)...)
	command.Env = append(os.Environ(),
		"HIGHEST_TAG_SCRIPT="+os.Getenv("GITHUB_WORKSPACE")+"/"+highestTagScript,
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web",
		"STUB_COUNTER="+filepath.ToSlash(filepath.Join(dir, "counter")),
	)
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr

	code := 0
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run the script: %v", err)
		}
		code = exit.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestTheHighestStableTagScriptAnswersWithItsExitStatus drives the script on
// its own. 0 is "highest", 1 is "a higher one exists", and every other status
// is a question left unanswered — never to be read as either.
func TestTheHighestStableTagScriptAnswersWithItsExitStatus(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		args       []string
		env        map[string]string
		wantCode   int
		wantStdout string
		wantAsked  int
	}{
		{name: "the highest of several", args: []string{"v1.10.0"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2", "v1.10.0", "v1.2.0")}, wantCode: 0, wantStdout: "v1.10.0\n", wantAsked: 1},
		{name: "below one whose text sorts lower", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2", "v1.10.0")}, wantCode: 1, wantStdout: "v1.10.0\n", wantAsked: 1},
		{name: "a backport below the next major", args: []string{"v1.9.3"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2", "v1.9.3", "v2.0.0")}, wantCode: 1, wantStdout: "v2.0.0\n", wantAsked: 1},
		{name: "a higher pre-release does not count", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2", "v2.0.0-rc.1")}, wantCode: 0, wantStdout: "v1.9.2\n", wantAsked: 1},
		{name: "a build suffix does not count", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2", "v1.9.3+build")}, wantCode: 0, wantStdout: "v1.9.2\n", wantAsked: 1},
		{name: "a listing that lags the tag asked about", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": refsOf("v1.9.1")}, wantCode: 0, wantStdout: "v1.9.2\n", wantAsked: 1},
		{name: "a listing that cannot be read", args: []string{"v1.9.2"}, env: map[string]string{"GH_FAIL": "1"}, wantCode: 2, wantAsked: 3},
		{name: "a listing with no tag in it", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": ""}, wantCode: 2, wantAsked: 1},
		{name: "a pre-release is no candidate", args: []string{"v1.10.0-rc.1"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 3},
		{name: "a leading zero is no candidate", args: []string{"v01.2.3"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 3},
		{name: "two components are no candidate", args: []string{"v1.9"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 3},
		{name: "a name that is not a version is no candidate", args: []string{"main"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 3},
		{name: "no argument", args: nil, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 64},
		{name: "two arguments", args: []string{"v1.9.2", "v1.9.3"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2")}, wantCode: 64},
		{name: "no repository to ask about", args: []string{"v1.9.2"}, env: map[string]string{"GH_REFS": refsOf("v1.9.2"), "GITHUB_REPOSITORY": ""}, wantCode: 64},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr, code := runHighestStableTag(t, testCase.args, "", testCase.env)

			if code != testCase.wantCode {
				t.Errorf("the script exited %d, want %d:\n%s", code, testCase.wantCode, stderr)
			}
			if stdout != testCase.wantStdout {
				t.Errorf("the script printed %q, want %q", stdout, testCase.wantStdout)
			}
			if got := strings.Count(stderr, "GH api --paginate repos/ovumcy/ovumcy-web/git/matching-refs/tags/v "); got != testCase.wantAsked {
				t.Errorf("the script asked for the tag listing %d time(s), want %d:\n%s", got, testCase.wantAsked, stderr)
			}
		})
	}
}

// TestTheHighestStableTagScriptAsksAgainWhenTheListingFailsAtFirst: a listing
// that fails twice and then answers is an answer, not an unknown.
func TestTheHighestStableTagScriptAsksAgainWhenTheListingFailsAtFirst(t *testing.T) {
	const flaky = `gh() { printf 'GH %s\n' "$*" >&2; n="$(cat "$STUB_COUNTER" 2>/dev/null || echo 0)"; echo $(( n + 1 )) > "$STUB_COUNTER"; if [ "$n" -lt "${GH_FAILS_FIRST:-0}" ]; then return 1; fi; printf '%s\n' "$GH_REFS"; }
export -f gh`

	stdout, stderr, code := runHighestStableTag(t, []string{"v1.9.2"}, flaky, map[string]string{"GH_REFS": refsOf("v1.9.2"), "GH_FAILS_FIRST": "2"})
	if code != 0 || stdout != "v1.9.2\n" {
		t.Errorf("the script exited %d and printed %q after a listing that failed twice, want 0 and the tag:\n%s", code, stdout, stderr)
	}
	if got := strings.Count(stderr, "GH api "); got != 3 {
		t.Errorf("the script asked %d time(s), want 3:\n%s", got, stderr)
	}
}

// TestTheHighestStableTagScriptNeverReportsAFailureAsAHigherTag: a command
// inside it that breaks ends in status 70. Left to errexit alone the status
// would be the failing command's, and a 1 means "a higher tag exists" to every
// caller — `latest` dropped from a release because `sort` was missing.
func TestTheHighestStableTagScriptNeverReportsAFailureAsAHigherTag(t *testing.T) {
	_, stderr, code := runHighestStableTag(t, []string{"v1.9.2"}, "sort() { return 1; }\nexport -f sort", map[string]string{"GH_REFS": refsOf("v1.9.2")})
	if code != 70 {
		t.Errorf("the script exited %d when `sort` failed, want 70:\n%s", code, stderr)
	}
}

// TestEveryAskOfTheHighestStableTagRunsTheCheckedInScript holds the five sites
// by name and then enumerates every workflow from the directory, so a sixth
// site that copies the listing back in, or calls the script from a job that
// never checked the repository out, fails here without being listed.
func TestEveryAskOfTheHighestStableTagRunsTheCheckedInScript(t *testing.T) {
	for _, site := range []struct{ workflow, job, step string }{
		{publishWorkflow, publishJob, latestStep},
		{publishWorkflow, publishJob, promoteStep},
		{publishWorkflow, publishJob, mirrorStep},
		{promoteWorkflow, promoteJob, promoteRefuseStep},
		{promoteWorkflow, promoteJob, promoteRecheckStep},
	} {
		job := workflowfile.Job(t, site.workflow, site.job)
		block := withoutComments(workflowfile.Step(t, site.workflow, site.job, job, site.step))
		if !strings.Contains(block, `bash "${GITHUB_WORKSPACE}/`+highestTagScript+`"`) {
			t.Errorf("%s, step %q does not run %s", site.workflow, site.step, highestTagScript)
		}
	}

	checkout := regexp.MustCompile(`(?s)uses: actions/checkout@[0-9a-f]{40}[^\n]*\n\s+with:\n(?:\s+[a-z-]+: [^\n]*\n)*?\s+persist-credentials: false`)
	matches := workflowFiles(t, filepath.Join("..", "..", ".github", "workflows"))
	if len(matches) == 0 {
		t.Fatalf("list the workflows: none found")
	}
	calls := 0
	for _, match := range matches {
		workflow := ".github/workflows/" + filepath.Base(match)
		text := withoutComments(workflowfile.Read(t, workflow))

		for _, owned := range []string{"matching-refs", "sort -V"} {
			if strings.Contains(text, owned) {
				t.Errorf("%s spells %q outside a comment: the listing and the version order belong to %s alone", workflow, owned, highestTagScript)
			}
		}

		for offset := 0; ; {
			at := strings.Index(text[offset:], highestTagScript)
			if at < 0 {
				break
			}
			at += offset
			offset = at + len(highestTagScript)
			calls++

			start := strings.LastIndex(text[:at], "\n    steps:\n")
			if start < 0 || !checkout.MatchString(text[start:at]) {
				t.Errorf("%s calls %s from a job with no `actions/checkout` (persist-credentials: false) before it, so the file is not on the runner", workflow, highestTagScript)
			}
		}
	}
	if calls < 5 {
		t.Errorf("the workflows call %s %d time(s), want at least the five named sites", highestTagScript, calls)
	}
}

// TestTheHighestStableTagCheckIsTheFirstQuestionTheRefusalAsks: the dispatch
// input reaches the tag lookup only after the script has judged its shape, so a
// name that is not a stable tag is refused before it is put in a URL.
func TestTheHighestStableTagCheckIsTheFirstQuestionTheRefusalAsks(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	block := withoutComments(workflowfile.Step(t, promoteWorkflow, promoteJob, job, promoteRefuseStep))

	script := strings.Index(block, highestTagScript)
	lookup := strings.Index(block, "git/ref/tags/")
	if script < 0 || lookup < 0 || script > lookup {
		t.Errorf("%s, step %q: the script is at %d and the tag lookup at %d; the shape must be judged first", promoteWorkflow, promoteRefuseStep, script, lookup)
	}
	if got := strings.Count(block, `[ "$rc" = "3" ]`); got != 1 {
		t.Errorf("%s, step %q refuses a status-3 answer %d time(s), want once", promoteWorkflow, promoteRefuseStep, got)
	}
}
