package publishorder

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// `latest` names the newest STABLE RELEASE. It used to follow `main`, through
// the metadata step's `is_default_branch`, so the floating name an operator
// pulls by default was whatever merged last. This file holds the rule that
// replaced it, at both of the places that can write the alias: the release
// tag's own publish run, and the manual promotion of an already signed release.

const (
	latestStep   = "Decide whether this run moves latest"
	metadataStep = "Extract Docker metadata"

	promoteWorkflow = ".github/workflows/promote-latest.yml"
	promoteJob      = "promote"

	promoteRefuseStep    = "Refuse anything but the highest existing stable release tag"
	promoteResolveStep   = "Resolve the release index digest on GHCR"
	promoteVerifyStep    = "Verify the signature and provenance of the release digest"
	promoteHubCopyStep   = "Copy the release onto Docker Hub when the mirror lacks it"
	promoteHubVerifyStep = "Verify the Docker Hub copy of the release digest"
	promoteRecheckStep   = "Check this tag is still the highest stable release"
	promoteGhcrMoveStep  = "Point latest at the signed digest on GHCR"
	promoteGhcrCheckStep = "Check GHCR latest anonymously and against the signed digest"
	promoteHubMoveStep   = "Point latest at the signed digest on Docker Hub"
	promoteHubCheckStep  = "Check Docker Hub latest anonymously and against the signed digest"
	promoteReportStep    = "Report where latest points on each registry"
)

// workflowFiles lists every workflow file under root, `*.yml` and `*.yaml`
// alike: GitHub runs both, so a sweep that globbed one extension would pass a
// workflow written in the other without ever reading it.
func workflowFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("list the workflows: %v", err)
		}
		files = append(files, matches...)
	}
	return files
}

// TestTheWorkflowSweepsReadBothExtensions is the control for the helper the
// directory sweeps below rely on: a `.yaml` workflow must be listed beside the
// `.yml` one, and a file of another kind must not.
func TestTheWorkflowSweepsReadBothExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.yml", "b.yaml", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	var got []string
	for _, file := range workflowFiles(t, dir) {
		got = append(got, filepath.Base(file))
	}
	slices.Sort(got)
	if want := []string{"a.yml", "b.yaml"}; !slices.Equal(got, want) {
		t.Errorf("the sweep listed %v, want %v", got, want)
	}
}

// runWorkflowStep runs one step's own script under the flags its `shell:`
// compiles to, with `preamble` (shadowing functions) in front of it, and returns
// the combined output, what the step wrote to `$GITHUB_OUTPUT`, and the exit
// error. The environment is the process's own plus env: a variable the runner
// sets (`GITHUB_REF`, `GITHUB_REPOSITORY`) is always handed in explicitly, so a
// CI run of this test never reads the real one.
func runWorkflowStep(t *testing.T, workflow, jobName, step, preamble string, env map[string]string) (string, string, error) {
	t.Helper()

	bash := requireBash(t)
	job := workflowfile.Job(t, workflow, jobName)
	block := workflowfile.Step(t, workflow, jobName, job, step)

	marker := "        run: |\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("%s, step %q: no `run: |` block, so this guard would run nothing", workflow, step)
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

	dir := t.TempDir()
	file := filepath.Join(dir, "step.sh")
	if err := os.WriteFile(file, []byte(preamble+"\n"+strings.Join(script, "\n")), 0o600); err != nil {
		t.Fatalf("write the step's script: %v", err)
	}
	outputs := filepath.Join(dir, "github_output")
	if err := os.WriteFile(outputs, nil, 0o600); err != nil {
		t.Fatalf("create the step output file: %v", err)
	}

	flags := workflowfile.BashStepFlags(t, workflow, step, block)
	command := exec.Command(bash, append(flags, filepath.ToSlash(file))...)
	command.Env = append(os.Environ(), "GITHUB_OUTPUT="+filepath.ToSlash(outputs))
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()

	// The curl shadows below print this marker when a bearer token reaches
	// them as an ARGUMENT. Judged here, for every step that runs, so a step
	// that put its token back on the command line fails whatever else it
	// was being asked.
	if strings.Contains(string(output), "ARGV-BEARER") {
		t.Errorf("%s, step %q passed a bearer token to curl on the command line, where the process table shows it; a header file carries it instead:\n%s", workflow, step, output)
	}

	written, readErr := os.ReadFile(outputs)
	if readErr != nil {
		t.Fatalf("read the step output file: %v", readErr)
	}
	return string(output), string(written), err
}

// TestLatestIsNeverEnabledByTheBranchPath holds the one line that decides it.
// The metadata step derives the tag list every later step promotes, verifies
// and mirrors, so `latest` is enabled there or nowhere: by the answer of the
// step above it and by nothing that can be true on a branch. The step is found
// by NAME and its id is checked against the expression that reads it, so a
// renamed step, or an expression reading an id that no step has, fails here
// rather than enabling nothing.
func TestLatestIsNeverEnabledByTheBranchPath(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	steps := stepNames(t, job)

	decide, meta := slices.Index(steps, latestStep), slices.Index(steps, metadataStep)
	if decide < 0 || meta < 0 || decide >= meta {
		t.Fatalf("%s, job %q: %q must run before %q (found at %d and %d), or the metadata step reads an answer that does not exist yet",
			publishWorkflow, publishJob, latestStep, metadataStep, decide, meta)
	}

	// A step block begins straight after the line naming the step, so its first
	// key has no newline in front of it; one is added.
	if block := withoutComments(stepBlock(t, job, latestStep)); !strings.Contains("\n"+block, "\n        id: latest\n") {
		t.Errorf("%s, step %q declares no `id: latest`, which the metadata step's expression reads", publishWorkflow, latestStep)
	}

	metaBlock := withoutComments(stepBlock(t, job, metadataStep))
	const enabled = "type=raw,value=latest,enable=${{ steps.latest.outputs.publish_latest == 'true' }}"
	if !strings.Contains(metaBlock, "\n            "+enabled+"\n") {
		t.Errorf("%s, step %q does not enable `latest` with exactly %q, so the alias is not tied to the release decision", publishWorkflow, metadataStep, enabled)
	}
	for _, forbidden := range []string{"is_default_branch", "{{is_default_branch}}"} {
		if strings.Contains(metaBlock, forbidden) {
			t.Errorf("%s, step %q mentions %q: the default branch is where `main` is published, and `latest` is not allowed to follow it", publishWorkflow, metadataStep, forbidden)
		}
	}
	if got := strings.Count(metaBlock, "value=latest"); got != 1 {
		t.Errorf("%s, step %q names `value=latest` %d times, want once (the line above)", publishWorkflow, metadataStep, got)
	}
}

// TestNoWorkflowWritesLatestExceptTheTwoThatOwnIt enumerates every workflow in
// the directory rather than naming the ones known today, so a workflow added
// later that publishes `:latest` — a second image job, a nightly — is refused
// here until it is read for what it moves. The two owners are asserted BY NAME
// first: a directory read that found neither would otherwise pass for "nobody
// writes it".
func TestNoWorkflowWritesLatestExceptTheTwoThatOwnIt(t *testing.T) {
	owners := []string{publishWorkflow, promoteWorkflow}

	matches := workflowFiles(t, filepath.Join("..", "..", ".github", "workflows"))

	seen := map[string]bool{}
	for _, match := range matches {
		workflow := ".github/workflows/" + filepath.Base(match)
		seen[workflow] = true
		if slices.Contains(owners, workflow) {
			continue
		}

		text := withoutComments(workflowfile.Read(t, workflow))
		for _, needle := range []string{"value=latest", ":latest", "tags/latest", "manifests/latest"} {
			if strings.Contains(text, needle) {
				t.Errorf("%s writes or reads %q, and only %v may: `latest` is the newest stable release, moved by a release tag's run or by the manual promotion", workflow, needle, owners)
			}
		}
	}
	for _, owner := range owners {
		if !seen[owner] {
			t.Fatalf("the directory read found no %s, so this guard has judged nothing", owner)
		}
	}

	// docker-image.yml: the alias is the metadata step's one line and nothing
	// else spells it as a tag reference.
	if text := withoutComments(workflowfile.Read(t, publishWorkflow)); strings.Contains(text, ":latest") {
		t.Errorf("%s names a `:latest` reference outside a comment: the alias is written only through the metadata step's tag list", publishWorkflow)
	}
}

// ghStub shadows `gh` for the decision step: it prints GH_REFS as the tag
// listing, or fails when GH_FAIL is set, and logs its arguments so the listing
// the step asked for can be read.
const ghStub = `gh() { printf 'GH %s\n' "$*" >&2; if [ -n "${GH_FAIL:-}" ]; then return 1; fi; printf '%s\n' "$GH_REFS"; }
sleep() { :; }`

func refsOf(tags ...string) string {
	var lines []string
	for _, tag := range tags {
		lines = append(lines, "refs/tags/"+tag)
	}
	return strings.Join(lines, "\n")
}

// TestOnlyTheHighestStableReleaseTagMovesLatest runs the decision step for real,
// with only the tag listing supplied. Each case is one way `latest` could go
// wrong: onto a branch build, onto a pre-release, backwards onto a backport,
// onto the wrong tag because versions were compared as text, or — the
// positive controls — not onto the release it belongs to.
func TestOnlyTheHighestStableReleaseTagMovesLatest(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		refType string
		refName string
		refs    string
		want    string
	}{
		{name: "a push to main", refType: "branch", refName: "main", refs: refsOf("v1.9.2"), want: "false"},
		{name: "the highest stable tag", refType: "tag", refName: "v1.9.2", refs: refsOf("v1.8.0", "v1.9.1", "v1.9.2"), want: "true"},
		{name: "the first release", refType: "tag", refName: "v0.1.0", refs: refsOf("v0.1.0"), want: "true"},
		{name: "a tag above one whose text sorts higher", refType: "tag", refName: "v1.10.0", refs: refsOf("v1.9.2", "v1.10.0"), want: "true"},
		{name: "a tag below one whose text sorts lower", refType: "tag", refName: "v1.9.2", refs: refsOf("v1.9.2", "v1.10.0"), want: "false"},
		{name: "a backported patch below the newest release", refType: "tag", refName: "v1.9.3", refs: refsOf("v1.9.2", "v1.9.3", "v2.0.0"), want: "false"},
		{name: "an older patch pushed again", refType: "tag", refName: "v1.9.2", refs: refsOf("v1.9.2", "v1.9.10"), want: "false"},
		{name: "a pre-release", refType: "tag", refName: "v2.0.0-rc.1", refs: refsOf("v1.9.2", "v2.0.0-rc.1"), want: "false"},
		{name: "a stable tag beside a higher pre-release", refType: "tag", refName: "v2.1.0", refs: refsOf("v2.1.0", "v3.0.0-rc.1"), want: "true"},
		{name: "a stable tag whose pre-release came first", refType: "tag", refName: "v2.0.0", refs: refsOf("v2.0.0-rc.1", "v2.0.0", "v1.9.2"), want: "true"},
		{name: "a listing that lags this very tag", refType: "tag", refName: "v1.9.2", refs: refsOf("v1.9.1"), want: "true"},
		{name: "a leading zero", refType: "tag", refName: "v01.2.3", refs: refsOf("v01.2.3"), want: "false"},
		{name: "two components", refType: "tag", refName: "v1.2", refs: refsOf("v1.2"), want: "false"},
		{name: "four components", refType: "tag", refName: "v1.2.3.4", refs: refsOf("v1.2.3.4"), want: "false"},
		{name: "a build suffix", refType: "tag", refName: "v1.2.3+build", refs: refsOf("v1.2.3+build"), want: "false"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, written, err := runWorkflowStep(t, publishWorkflow, publishJob, latestStep, ghStub, map[string]string{
				"REF_TYPE":          testCase.refType,
				"REF_NAME":          testCase.refName,
				"GITHUB_REPOSITORY": "ovumcy/ovumcy-web",
				"GH_REFS":           testCase.refs,
			})
			if err != nil {
				t.Fatalf("the decision step failed: %v\n%s", err, output)
			}
			if want := "publish_latest=" + testCase.want + "\n"; written != want {
				t.Errorf("for %s %s among %q the step exported %q, want %q\n%s", testCase.refType, testCase.refName, testCase.refs, written, want, output)
			}
		})
	}
}

// TestTheDecisionAsksForTheRepositorysVersionTags pins what the step lists. A
// step that compared against a listing of something else — branches, or a
// different repository — would still produce a verdict for every case above.
func TestTheDecisionAsksForTheRepositorysVersionTags(t *testing.T) {
	output, _, err := runWorkflowStep(t, publishWorkflow, publishJob, latestStep, ghStub, map[string]string{
		"REF_TYPE":          "tag",
		"REF_NAME":          "v1.9.2",
		"GITHUB_REPOSITORY": "ovumcy/ovumcy-web",
		"GH_REFS":           refsOf("v1.9.2"),
	})
	if err != nil {
		t.Fatalf("the decision step failed: %v\n%s", err, output)
	}
	if want := "GH api --paginate repos/ovumcy/ovumcy-web/git/matching-refs/tags/v "; !strings.Contains(output, want) {
		t.Errorf("the step did not list the repository's `v*` tags (wanted %q):\n%s", want, output)
	}
}

// TestAnUnreadableTagListPublishesNothing is the refusal. Guessing `true` could
// move `latest` backwards and guessing `false` would drop it from a new release
// in silence; the step fails before the push instead, with nothing written.
func TestAnUnreadableTagListPublishesNothing(t *testing.T) {
	output, written, err := runWorkflowStep(t, publishWorkflow, publishJob, latestStep, ghStub, map[string]string{
		"REF_TYPE":          "tag",
		"REF_NAME":          "v1.9.2",
		"GITHUB_REPOSITORY": "ovumcy/ovumcy-web",
		"GH_FAIL":           "1",
	})
	if err == nil {
		t.Fatalf("the step accepted a tag list it could not read.\n%s", output)
	}
	requireRefusalReason(t, output, "could not be listed")
	if written != "" {
		t.Errorf("the step exported %q although it could not tell, which the next step would read as an answer", written)
	}
}

// TestTheDecisionCarriesNoRunnerSideSkip: an `if:` on the step would let the
// runner skip it, and the metadata step's expression would then read an empty
// output, which is `false` — for a release as much as for a branch, silently.
func TestTheDecisionCarriesNoRunnerSideSkip(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	if found := workflowfile.StepFailOpenKeys(stepBlock(t, job, latestStep)); len(found) > 0 {
		t.Errorf("%s, step %q carries %q: the runner could skip or pass the decision", publishWorkflow, latestStep, found)
	}
}

// --- promote-latest.yml -----------------------------------------------------

func promoteStepNames(t *testing.T) []string {
	t.Helper()

	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	var names []string
	for _, match := range stepName.FindAllStringSubmatch(job, -1) {
		names = append(names, strings.TrimSpace(match[1]))
	}
	if len(names) == 0 {
		t.Fatalf("%s, job %q: no steps found", promoteWorkflow, promoteJob)
	}
	return names
}

// TestThePromotionVerifiesEverythingBeforeItWritesAnything is the order rule of
// this workflow, in the shape the publish job's is: the verdicts first, GHCR's
// write next and checked, the mirror's last. It is written against the step
// list, and the steps it orders are found by name.
func TestThePromotionVerifiesEverythingBeforeItWritesAnything(t *testing.T) {
	steps := promoteStepNames(t)
	index := func(name string) int {
		at := slices.Index(steps, name)
		if at < 0 {
			t.Fatalf("%s: no step named %q, and the order of this workflow is what is guarded — rename it here too, deliberately", promoteWorkflow, name)
		}
		return at
	}

	chainNames := []string{
		promoteRefuseStep, promoteResolveStep, promoteVerifyStep, promoteHubCopyStep, promoteHubVerifyStep, promoteRecheckStep,
		promoteGhcrMoveStep, promoteGhcrCheckStep, promoteHubMoveStep, promoteHubCheckStep, promoteReportStep,
	}
	for i := 1; i < len(chainNames); i++ {
		if index(chainNames[i-1]) >= index(chainNames[i]) {
			t.Errorf("%s: %q runs at or after %q. Every refusal and every signature check — the Docker Hub copy's included — comes before the first write of `latest`, and the Docker Hub write waits for GHCR's to be read back", promoteWorkflow, chainNames[i-1], chainNames[i])
		}
	}

	// The re-check of "still the highest stable tag" is the step right before
	// the first write of `latest`: anything between them is time in which a
	// higher tag can appear unseen.
	if index(promoteRecheckStep)+1 != index(promoteGhcrMoveStep) {
		t.Errorf("%s: %q is not immediately before %q", promoteWorkflow, promoteRecheckStep, promoteGhcrMoveStep)
	}

	// And nothing else may write: a step added between the verdicts and the
	// writes, or after them, has to be read for whether it can move a tag.
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	for _, name := range steps[:index(promoteGhcrMoveStep)] {
		if strings.Contains(withoutComments(workflowfile.Step(t, promoteWorkflow, promoteJob, job, name)), "-X PUT") {
			t.Errorf("%s: %q runs before the first write and writes a tag", promoteWorkflow, name)
		}
	}
	if report := withoutComments(workflowfile.Step(t, promoteWorkflow, promoteJob, job, promoteReportStep)); strings.Contains(report, "-X PUT") || strings.Contains(report, "oras ") {
		t.Errorf("%s: %q reports and must write nothing", promoteWorkflow, promoteReportStep)
	}
}

// TestThePromotionHoldsLeastPrivilege reads the permissions off the parsed
// workflow: the file-level default and the one job's exact set. Anything added
// to the job — `id-token`, `attestations: write`, `contents: write` — is a
// capability a path that verifies and retags has no use for.
func TestThePromotionHoldsLeastPrivilege(t *testing.T) {
	text := workflowfile.Read(t, promoteWorkflow)

	if !strings.Contains(text, "\npermissions:\n  contents: read\n\n") {
		t.Errorf("%s: the file-level permissions are not exactly `contents: read`", promoteWorkflow)
	}

	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	const marker = "\n    permissions:\n"
	start := strings.Index(job, marker)
	if start < 0 {
		t.Fatalf("%s, job %q declares no `permissions:`, so it would inherit whatever the repository default is", promoteWorkflow, promoteJob)
	}
	var got []string
	for _, line := range strings.Split(job[start+len(marker):], "\n") {
		if !strings.HasPrefix(line, "      ") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			break
		}
		if entry := strings.TrimSpace(line); !strings.HasPrefix(entry, "#") {
			got = append(got, entry)
		}
	}
	want := []string{"attestations: read", "contents: read", "packages: write"}
	if !slices.Equal(got, want) {
		t.Errorf("%s, job %q holds %v, want exactly %v", promoteWorkflow, promoteJob, got, want)
	}

	if !strings.Contains(text, "\non:\n  workflow_dispatch:\n") || strings.Contains(text, "\n  push:") || strings.Contains(text, "pull_request") || strings.Contains(text, "workflow_call") {
		t.Errorf("%s: it must be reachable by a manual dispatch and by nothing else", promoteWorkflow)
	}
}

// TestThePromotionRefusesAnythingButAnExistingStableReleaseTag runs the refusal
// step for real. The `gh` stub answers for the tag lookup; a name that fails the
// shape never needs it.
func TestThePromotionRefusesAnythingButAnExistingStableReleaseTag(t *testing.T) {
	// The existence lookup answers a ref (or fails with GH_FAIL), the tag
	// listing answers GH_REFS (or fails with GH_LIST_FAIL).
	const ghExists = `gh() { printf 'GH %s\n' "$*" >&2
  case "$*" in
    *matching-refs*) if [ -n "${GH_LIST_FAIL:-}" ]; then return 1; fi; printf '%s\n' "$GH_REFS" ;;
    *) if [ -n "${GH_FAIL:-}" ]; then return 1; fi; printf 'refs/tags/x\n' ;;
  esac; }
sleep() { :; }`

	for _, testCase := range []struct {
		name        string
		tag         string
		ref         string
		ghFails     bool
		listFails   bool
		refs        string
		wantRefusal string
	}{
		{name: "the highest of several", tag: "v1.10.0", ref: "refs/heads/main", refs: refsOf("v1.9.2", "v1.10.0", "v1.2.0")},
		{name: "a stable tag below a higher one", tag: "v1.9.2", ref: "refs/heads/main", refs: refsOf("v1.9.2", "v1.10.0"), wantRefusal: "is below v1.10.0"},
		{name: "a stable tag below a major", tag: "v1.9.2", ref: "refs/heads/main", refs: refsOf("v1.9.2", "v2.0.0"), wantRefusal: "latest never moves backwards"},
		{name: "a higher pre-release does not outrank it", tag: "v1.9.2", ref: "refs/heads/main", refs: refsOf("v1.9.2", "v2.0.0-rc.1")},
		{name: "a tag listing that cannot be read", tag: "v1.9.2", ref: "refs/heads/main", listFails: true, wantRefusal: "could not be listed"},
		{name: "an existing stable release from main", tag: "v1.9.2", ref: "refs/heads/main"},
		{name: "a two-digit minor", tag: "v1.10.0", ref: "refs/heads/main"},
		{name: "a pre-release", tag: "v1.10.0-rc.1", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "no v prefix", tag: "1.9.2", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "two components", tag: "v1.9", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "a leading zero", tag: "v1.09.2", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "a branch name", tag: "main", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "latest itself", tag: "latest", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "a trailing newline", tag: "v1.9.2\nfoo", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "a shell-shaped tag", tag: "v1.9.2;id", ref: "refs/heads/main", wantRefusal: "is not a stable release tag"},
		{name: "a stable name no tag carries", tag: "v9.9.9", ref: "refs/heads/main", ghFails: true, wantRefusal: "there is no tag v9.9.9"},
		{name: "a dispatch from another branch", tag: "v1.9.2", ref: "refs/heads/feature", wantRefusal: "runs from refs/heads/main only"},
		{name: "a dispatch from a tag ref", tag: "v1.9.2", ref: "refs/tags/v1.9.2", wantRefusal: "runs from refs/heads/main only"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			refs := testCase.refs
			if refs == "" {
				refs = refsOf(testCase.tag)
			}
			env := map[string]string{
				"TAG":               testCase.tag,
				"GITHUB_REF":        testCase.ref,
				"GITHUB_REPOSITORY": "ovumcy/ovumcy-web",
				"GH_REFS":           refs,
			}
			if testCase.ghFails {
				env["GH_FAIL"] = "1"
			}
			if testCase.listFails {
				env["GH_LIST_FAIL"] = "1"
			}
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteRefuseStep, ghExists, env)

			if testCase.wantRefusal == "" {
				if err != nil {
					t.Fatalf("the step refused a release it owes: %v\n%s", err, output)
				}
				if want := "GH api repos/ovumcy/ovumcy-web/git/ref/tags/" + testCase.tag; !strings.Contains(output, want) {
					t.Errorf("the step did not look the tag up as %q:\n%s", want, output)
				}
				return
			}
			if err == nil {
				t.Fatalf("the step accepted %q from %s.\n%s", testCase.tag, testCase.ref, output)
			}
			requireRefusalReason(t, output, testCase.wantRefusal)
		})
	}
}

// TestThePromotionPinsTheReleasesOwnSigner runs the verification step with
// `cosign` and `gh` shadowed and reads the identity it hands cosign. The
// pattern is then compiled and tried against identities: the one the release
// run signs under must match, and the ones that must not — a branch run of the
// same workflow, another tag, a longer tag that merely starts the same, an
// unescaped dot standing in for any character — must not.
func TestThePromotionPinsTheReleasesOwnSigner(t *testing.T) {
	const stubs = `cosign() { printf 'COSIGN %s\n' "$*" >&2; if [ -n "${COSIGN_FAIL:-}" ]; then return 1; fi; }
gh() { printf 'GH %s\n' "$*" >&2; }`
	const digestRef = imageName + "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const identityPrefix = "https://github.com/ovumcy/ovumcy-web/.github/workflows/docker-image.yml@"

	env := func(extra map[string]string) map[string]string {
		base := map[string]string{
			"TAG":               "v1.9.2",
			"GITHUB_REPOSITORY": "ovumcy/ovumcy-web",
			"IMAGE_DIGEST":      digestRef,
		}
		for key, value := range extra {
			base[key] = value
		}
		return base
	}

	output, written, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteVerifyStep, stubs, env(nil))
	if err != nil {
		t.Fatalf("the verification step failed: %v\n%s", err, output)
	}

	const wantPattern = `^https://github\.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@refs/tags/v1[.]9[.]2$`
	if want := "identity_regexp=" + wantPattern + "\n"; written != want {
		t.Fatalf("the step exported %q, want %q — the Docker Hub check reads this output", written, want)
	}
	for _, want := range []string{
		"COSIGN verify --certificate-identity-regexp " + wantPattern + " --certificate-oidc-issuer https://token.actions.githubusercontent.com " + digestRef,
		// The attestation is held to the same signer as the signature: the
		// workflow file, and the release's own tag ref.
		"GH attestation verify oci://" + digestRef + " --repo ovumcy/ovumcy-web --signer-workflow ovumcy/ovumcy-web/.github/workflows/docker-image.yml --source-ref refs/tags/v1.9.2",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("the step never ran %q:\n%s", want, output)
		}
	}

	pattern := regexp.MustCompile(wantPattern)
	const otherWorkflow = "https://github.com/ovumcy/ovumcy-web/.github/workflows/other.yml@refs/tags/v1.9.2"
	for _, identity := range []struct {
		value     string
		wantMatch bool
	}{
		{identityPrefix + "refs/tags/v1.9.2", true},
		{identityPrefix + "refs/heads/main", false},
		{identityPrefix + "refs/tags/v1.9.3", false},
		{identityPrefix + "refs/tags/v1.9.20", false},
		{identityPrefix + "refs/tags/v1x9y2", false},
		{identityPrefix + "refs/tags/v1.9.2-rc", false},
		{otherWorkflow, false},
	} {
		if got := pattern.MatchString(identity.value); got != identity.wantMatch {
			t.Errorf("the identity pattern matches %q = %v, want %v", identity.value, got, identity.wantMatch)
		}
	}

	// A signature that does not verify ends the step before the attestation is
	// asked about, and exports a pattern only because it was built first.
	output, _, err = runWorkflowStep(t, promoteWorkflow, promoteJob, promoteVerifyStep, stubs, env(map[string]string{"COSIGN_FAIL": "1"}))
	if err == nil {
		t.Fatalf("the step passed a digest cosign refused.\n%s", output)
	}
	if strings.Contains(output, "GH attestation verify") || strings.Contains(output, "signature and provenance verified") {
		t.Errorf("the step went on past a signature cosign refused:\n%s", output)
	}

	// A repository name the escape cannot reach is refused rather than matched loosely.
	output, _, err = runWorkflowStep(t, promoteWorkflow, promoteJob, promoteVerifyStep, stubs, env(map[string]string{"GITHUB_REPOSITORY": "ovumcy/ovumcy web"}))
	if err == nil {
		t.Fatalf("the step accepted a repository name it cannot escape.\n%s", output)
	}
	requireRefusalReason(t, output, "cannot escape")
}

// registryCurl shadows `curl` and `python3` for the promotion's registry reads:
// a token request answers 200 with a token, and a manifest HEAD answers
// STUB_HEAD_STATUS with a docker-content-digest header carrying STUB_DIGEST.
// The URL read is logged, so the tests can say WHICH tag was asked about.
const registryCurl = bearerArgument + "\n" + `curl() {
  local out="" hdr="" url=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -o) out="$2"; shift ;;
      -D) hdr="$2"; shift ;;
      -H) bearerArgument "$2"; shift ;;
      -w|--connect-timeout|--max-time|--retry|--retry-delay) shift ;;
      -*) ;;
      *) url="$1" ;;
    esac
    shift
  done
  printf 'CURL %s\n' "$url" >&2
  case "$url" in
    *token*)
      [ -n "$out" ] && printf '{"token":"t"}' > "$out"
      printf '%s' "${STUB_TOKEN_STATUS:-200}" ;;
    *manifests/*)
      # A reference the run named by its release tag may answer another digest
      # than the one asked by digest, which is how a mirror's tag disagrees.
      local answer="${STUB_DIGEST-}"
      case "$url" in */manifests/v*) answer="${STUB_TAG_DIGEST-$answer}" ;; esac
      [ -n "$hdr" ] && printf 'HTTP/2 200\r\nDocker-Content-Digest: %s\r\n' "$answer" > "$hdr"
      printf '%s' "${STUB_HEAD_STATUS:-200}" ;;
  esac
}
python3() { printf 'bearer-secret'; }`

// bearerArgument is the part every curl shadow of this file shares: a header
// given as an argument is checked for a bearer token, which must come from a
// header FILE (`-H @file`) and never from the command line. The file's content
// is logged, so a test can see the token did reach curl by the allowed route.
const bearerArgument = `bearerArgument() {
  case "$1" in
    "Authorization: Bearer"*) printf 'ARGV-BEARER\n' >&2 ;;
    @*) printf 'HEADER-FILE %s\n' "$(cat "${1#@}")" >&2 ;;
  esac
}`

const signedDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// TestThePromotionReadsAndChecksDigests runs the resolve step and the two
// anonymous checks, which are the same comparison on two registries.
func TestThePromotionReadsAndChecksDigests(t *testing.T) {
	t.Run("the release digest is what GHCR names for the tag", func(t *testing.T) {
		output, written, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteResolveStep, registryCurl, map[string]string{
			"TAG": "v1.9.2", "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x", "STUB_DIGEST": signedDigest,
		})
		if err != nil {
			t.Fatalf("the step failed: %v\n%s", err, output)
		}
		if want := "digest=" + signedDigest + "\n"; written != want {
			t.Errorf("the step exported %q, want %q", written, want)
		}
		if want := "CURL https://ghcr.io/v2/" + imagePath + "/manifests/v1.9.2"; !strings.Contains(output, want) {
			t.Errorf("the step read no %q — the release is resolved by its own tag:\n%s", want, output)
		}
	})

	for _, testCase := range []struct {
		name        string
		env         map[string]string
		wantRefusal string
	}{
		{name: "a digest that is not a digest", env: map[string]string{"STUB_DIGEST": "latest"}, wantRefusal: "not a sha256 digest"},
		{name: "no digest header", env: map[string]string{"STUB_DIGEST": ""}, wantRefusal: "not a sha256 digest"},
		{name: "a release GHCR does not hold", env: map[string]string{"STUB_DIGEST": signedDigest, "STUB_HEAD_STATUS": "404"}, wantRefusal: "is not published there"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{"TAG": "v1.9.2", "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x"}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, written, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteResolveStep, registryCurl, env)
			if err == nil {
				t.Fatalf("the step accepted it.\n%s", output)
			}
			requireRefusalReason(t, output, testCase.wantRefusal)
			if written != "" {
				t.Errorf("the step exported %q although it refused", written)
			}
		})
	}

	for _, check := range []struct {
		step string
		host string
	}{
		{promoteGhcrCheckStep, "https://ghcr.io/v2/" + imagePath + "/manifests/latest"},
		{promoteHubCheckStep, "https://registry-1.docker.io/v2/" + imagePath + "/manifests/latest"},
	} {
		for _, testCase := range []struct {
			name        string
			stubDigest  string
			headStatus  string
			wantRefusal string
		}{
			{name: "latest is the signed digest", stubDigest: signedDigest},
			{name: "latest is another digest", stubDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantRefusal: "not to the signed digest"},
			{name: "latest does not resolve", stubDigest: signedDigest, headStatus: "401", wantRefusal: "returned HTTP 401"},
		} {
			t.Run(check.step+": "+testCase.name, func(t *testing.T) {
				env := map[string]string{
					"DIGEST": signedDigest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x", "TAG": "v1.9.2",
					"STUB_DIGEST": testCase.stubDigest, "STUB_HEAD_STATUS": testCase.headStatus,
				}
				output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, check.step, registryCurl, env)

				if testCase.wantRefusal == "" {
					if err != nil {
						t.Fatalf("the check refused the alias it owes: %v\n%s", err, output)
					}
					if !strings.Contains(output, "CURL "+check.host) {
						t.Errorf("the check never read %s:\n%s", check.host, output)
					}
					return
				}
				if err == nil {
					t.Fatalf("the check accepted it.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
			})
		}
	}
}

// hubTagDigest is what the mirror's release tag resolves to in the cases below
// that give it a digest of its own.
const hubTagDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// TestTheMirrorCopyIsVerifiedBeforeLatestMovesAnywhere runs the verification of
// the Docker Hub copy: the release digest must be there, the release tag must
// not name anything else, and the digest must verify there under the identity
// the GHCR step produced — asked again while the registry's referrers listing
// catches up, and only about that.
func TestTheMirrorCopyIsVerifiedBeforeLatestMovesAnywhere(t *testing.T) {
	// Counting in the function body, not a subshell: the step calls cosign in
	// an `if`, so the counter survives from one attempt to the next.
	const cosignStub = `n=0
sleep() { :; }
cosign() { printf 'COSIGN %s\n' "$*" >&2; n=$(( n + 1 )); if [ "$n" -le "${COSIGN_FAILS:-0}" ]; then printf '%s\n' "${COSIGN_MESSAGE:-no signatures found}" >&2; return 1; fi; }`
	const identity = `^stub-identity-the-mirror-must-pass-through@`
	const verifyCall = "COSIGN verify --certificate-identity-regexp " + identity + " --certificate-oidc-issuer https://token.actions.githubusercontent.com docker.io/" + imagePath + "@" + signedDigest

	for _, testCase := range []struct {
		name        string
		env         map[string]string
		wantCalls   int
		wantTagWord string
		wantRefusal string
	}{
		{name: "the mirror holds the digest, the tag is absent, and it verifies", env: map[string]string{"STUB_DIGEST": signedDigest, "STUB_TAG_STATUS": "404"}, wantCalls: 1, wantTagWord: "absent"},
		{name: "the mirror already names the release", env: map[string]string{"STUB_DIGEST": signedDigest}, wantCalls: 1, wantTagWord: "present"},
		{name: "the mirror's release tag is another digest", env: map[string]string{"STUB_DIGEST": signedDigest, "STUB_TAG_DIGEST": hubTagDigest}, wantRefusal: "A release tag is never rewritten"},
		{name: "the mirror never received the digest", env: map[string]string{"STUB_DIGEST": signedDigest, "STUB_DIGEST_STATUS": "404"}, wantRefusal: "does not hold"},
		{name: "the mirror's digest is another one", env: map[string]string{"STUB_DIGEST": hubTagDigest}, wantRefusal: "does not hold"},
		{name: "the referrers listing catches up before the attempts run out", env: map[string]string{"STUB_DIGEST": signedDigest, "COSIGN_FAILS": "2"}, wantCalls: 3, wantTagWord: "present"},
		{name: "the signature never appears", env: map[string]string{"STUB_DIGEST": signedDigest, "COSIGN_FAILS": "99"}, wantCalls: 3, wantRefusal: "carries no signature of the release over 3 attempts"},
		{name: "a signer that is not the release is an answer, asked once", env: map[string]string{"STUB_DIGEST": signedDigest, "COSIGN_FAILS": "99", "COSIGN_MESSAGE": "none of the signatures were verified against the certificate identity"}, wantCalls: 1, wantRefusal: "did not verify against the release's signer identity"},
		{name: "an empty identity", env: map[string]string{"STUB_DIGEST": signedDigest, "IDENTITY_REGEXP": ""}, wantRefusal: "accepts any signer"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{
				"TAG": "v1.9.2", "DIGEST": signedDigest, "IMAGE_PATH": imagePath,
				"IDENTITY_REGEXP": identity, "MANIFEST_ACCEPT": "x",
				// The budget is the fixture's, as the publish job's mirror
				// fixtures use theirs: what the step owes is to wait out a late
				// listing and refuse one that never comes.
				"VERIFY_ATTEMPTS": "3", "VERIFY_DELAY_SECONDS": "0",
			}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, written, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubVerifyStep, hubCurl+"\n"+cosignStub, env)

			if testCase.wantCalls > 0 {
				if got := strings.Count(output, "COSIGN verify "); got != testCase.wantCalls {
					t.Errorf("the step ran `cosign verify` %d time(s), want %d:\n%s", got, testCase.wantCalls, output)
				}
			}
			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the step passed it.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
				if written != "" {
					t.Errorf("the step exported %q although it refused, and the Docker Hub write reads it", written)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step refused a mirror it owes: %v\n%s", err, output)
			}
			if !strings.Contains(output, verifyCall) {
				t.Errorf("the mirror was not verified as %q:\n%s", verifyCall, output)
			}
			if want := "tag_on_hub=" + testCase.wantTagWord + "\n"; written != want {
				t.Errorf("the step exported %q, want %q", written, want)
			}
		})
	}
}

// TestTheShippedHubVerifyBudgetActuallyWaits judges the numbers the workflow
// carries, which every fixture above overrides: `VERIFY_ATTEMPTS` trimmed to one
// restores the one-shot read-back that lost the race against Docker Hub's
// referrers listing, and the whole package would stay green.
func TestTheShippedHubVerifyBudgetActuallyWaits(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	env := envConstants(workflowfile.Step(t, promoteWorkflow, promoteJob, job, promoteHubVerifyStep), "\n        env:\n", "          ")

	for name, atLeast := range map[string]int{"VERIFY_ATTEMPTS": 2, "VERIFY_DELAY_SECONDS": 1} {
		got, err := strconv.Atoi(env[name])
		if err != nil || got < atLeast {
			t.Errorf("%s, step %q declares %s = %q; it must be a number of at least %d, or the step asks once and does not wait for the listing", promoteWorkflow, promoteHubVerifyStep, name, env[name], atLeast)
		}
	}
}

// hubCurl shadows `curl` and `python3` for the verification of the Docker Hub
// copy. A manifest HEAD by digest answers STUB_DIGEST with STUB_DIGEST_STATUS
// (200 by default); one by the release tag answers STUB_TAG_DIGEST, which is
// STUB_DIGEST unless a case gives the tag a digest of its own, with
// STUB_TAG_STATUS.
const hubCurl = bearerArgument + "\n" + `curl() {
  local hdr="" url="" out=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -o) out="$2"; shift ;;
      -D) hdr="$2"; shift ;;
      -H) bearerArgument "$2"; shift ;;
      -w|--connect-timeout|--max-time|--retry|--retry-delay) shift ;;
      -*) ;;
      *) url="$1" ;;
    esac
    shift
  done
  printf 'CURL %s\n' "$url" >&2
  case "$url" in
    *token*)
      [ -n "$out" ] && printf '{"token":"t"}' > "$out"
      printf '200' ;;
    */manifests/sha256:*)
      [ -n "$hdr" ] && printf 'HTTP/2 200\r\nDocker-Content-Digest: %s\r\n' "${STUB_DIGEST-}" > "$hdr"
      printf '%s' "${STUB_DIGEST_STATUS:-200}" ;;
    */manifests/*)
      [ -n "$hdr" ] && printf 'HTTP/2 200\r\nDocker-Content-Digest: %s\r\n' "${STUB_TAG_DIGEST-${STUB_DIGEST-}}" > "$hdr"
      printf '%s' "${STUB_TAG_STATUS:-200}" ;;
  esac
}
python3() { printf 'bearer-secret'; }`

// putCurl shadows `curl` and `python3` for the two tag writes. A token request
// answers 200; a manifest GET by digest answers STUB_BODY with a Content-Type
// carrying a parameter; a PUT is logged with the media type it declares and the
// exact bytes it sends, and answers STUB_PUT_STATUS (201 by default).
const putCurl = bearerArgument + "\n" + `curl() {
  local out="" hdr="" url="" method="GET" data=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -o) out="$2"; shift ;;
      -D) hdr="$2"; shift ;;
      -X) method="$2"; shift ;;
      --data-binary) data="${2#@}"; shift ;;
      -H) bearerArgument "$2"; case "$2" in Content-Type:*) printf 'PUT-TYPE %s\n' "${2#Content-Type: }" >&2 ;; esac; shift ;;
      -w|-K|--connect-timeout|--max-time|--retry|--retry-delay) shift ;;
      -*) ;;
      *) url="$1" ;;
    esac
    shift
  done
  printf 'CURL %s %s\n' "$method" "$url" >&2
  case "$method $url" in
    "GET "*token*)
      printf '{"token":"t"}' > "$out"
      printf '200' ;;
    "GET "*manifests/sha256:*)
      printf '%s' "$STUB_BODY" > "$out"
      printf 'HTTP/2 200\r\nContent-Type: application/vnd.oci.image.index.v1+json; charset=utf-8\r\n' > "$hdr"
      printf '200' ;;
    "PUT "*)
      printf 'PUT-BODY %s\n' "$(cat "$data")" >&2
      printf '%s' "${STUB_PUT_STATUS:-201}" ;;
  esac
}
python3() { printf 'bearer-secret'; }`

// TestLatestIsWrittenAsTheSignedBytesPutUnderTheTag runs the two writes. Each
// fetches the manifest by DIGEST, refuses bytes that do not hash to it, and PUTs
// those bytes under `latest` with the media type they were stored under. A tag
// is a mutable pointer and the signature covers the bytes it named when it was
// signed, so a write that re-created the index would name something else.
func TestLatestIsWrittenAsTheSignedBytesPutUnderTheTag(t *testing.T) {
	const wrongBytes = otherManifest

	for _, step := range []struct {
		name string
		env  map[string]string
		api  string
	}{
		{promoteGhcrMoveStep, map[string]string{"REGISTRY_USER": "actor", "REGISTRY_PASSWORD": "secret"}, "https://ghcr.io/v2/" + imagePath},
		{promoteHubMoveStep, map[string]string{"REGISTRY_USER": "publisher", "REGISTRY_PASSWORD": "secret", "TAG": "v1.9.2", "TAG_ON_HUB": "present"}, "https://registry-1.docker.io/v2/" + imagePath},
	} {
		for _, testCase := range []struct {
			name        string
			body        string
			putStatus   string
			wantRefusal string
		}{
			{name: "the signed bytes", body: canonicalManifest},
			{name: "bytes that do not hash to the digest", body: wrongBytes, wantRefusal: "hashes to"},
			{name: "a refused write", body: canonicalManifest, putStatus: "401", wantRefusal: "returned HTTP 401"},
		} {
			t.Run(step.name+": "+testCase.name, func(t *testing.T) {
				env := map[string]string{
					"DIGEST": digest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x",
					"STUB_BODY": testCase.body, "STUB_PUT_STATUS": testCase.putStatus,
				}
				for key, value := range step.env {
					env[key] = value
				}
				output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, step.name, putCurl, env)

				if testCase.wantRefusal != "" {
					if err == nil {
						t.Fatalf("the step went on.\n%s", output)
					}
					requireRefusalReason(t, output, testCase.wantRefusal)
					if testCase.putStatus == "" && strings.Contains(output, "CURL PUT") {
						t.Errorf("the step wrote a tag from bytes that are not the signed digest:\n%s", output)
					}
					return
				}
				if err != nil {
					t.Fatalf("the step failed: %v\n%s", err, output)
				}
				for _, want := range []string{
					"CURL GET " + step.api + "/manifests/" + digest + "\n",
					"CURL PUT " + step.api + "/manifests/latest\n",
					// The parameter on the stored Content-Type is dropped.
					"PUT-TYPE application/vnd.oci.image.index.v1+json\n",
					"PUT-BODY " + canonicalManifest + "\n",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("the step never did %q:\n%s", want, output)
					}
				}
			})
		}
	}
}

// TestNoStepRecreatesTheIndexUnderATag enumerates the workflows from the
// directory and refuses `imagetools create` anywhere outside a comment: it
// builds a new index that need not hash to the digest the signature covers, so
// it is not a promotion. The two promote-latest writes are then found by name
// and held to the PUT, so the guard cannot pass by there being no writer.
func TestNoStepRecreatesTheIndexUnderATag(t *testing.T) {
	matches := workflowFiles(t, filepath.Join("..", "..", ".github", "workflows"))
	if len(matches) == 0 {
		t.Fatalf("list the workflows: none found")
	}
	for _, match := range matches {
		workflow := ".github/workflows/" + filepath.Base(match)
		if text := withoutComments(workflowfile.Read(t, workflow)); strings.Contains(text, "imagetools create") {
			t.Errorf("%s runs `imagetools create`: a tag is written by PUTting the signed manifest bytes back unchanged, not by re-creating the index", workflow)
		}
	}

	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	for name, wantTarget := range map[string]string{
		promoteGhcrMoveStep: "/manifests/latest",
		promoteHubMoveStep:  "/manifests/${name}",
	} {
		block := withoutComments(workflowfile.Step(t, promoteWorkflow, promoteJob, job, name))
		if !strings.Contains(block, "-X PUT") || !strings.Contains(block, wantTarget) {
			t.Errorf("%s, step %q does not PUT the manifest under %s", promoteWorkflow, name, wantTarget)
		}
	}
}
