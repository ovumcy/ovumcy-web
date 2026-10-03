package publishorder

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const mirrorCredentialStep = "Require the Docker Hub mirror credential on a release tag"

// runMirrorCredentialStep runs the step's own script under the flags its
// `shell: bash` compiles to, with the job-level username and the ref type the
// step reads, and returns the combined output and the exit error.
func runMirrorCredentialStep(t *testing.T, refType, username string) (string, error) {
	t.Helper()

	job := workflowfile.Job(t, publishWorkflow, publishJob)
	block := stepBlock(t, job, mirrorCredentialStep)
	script := stepScript(t, job, mirrorCredentialStep)
	flags := workflowfile.BashStepFlags(t, publishWorkflow, mirrorCredentialStep, block)

	file := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		t.Fatalf("write the step's script: %v", err)
	}

	command := exec.Command(requireBash(t), append(flags, filepath.ToSlash(file))...)
	command.Env = append(os.Environ(), "REF_TYPE="+refType, "DOCKERHUB_PUBLISH_USERNAME="+username)
	output, err := command.CombinedOutput()
	return string(output), err
}

// TestAReleaseTagRefusesAMissingMirrorCredentialBeforeAnythingIsPushed holds the
// tag path to an explicit failure where the mirror steps would skip silently.
// The `:latest` path keeps its skip, which is the positive control: a step that
// refused every run would also pass the tag case.
func TestAReleaseTagRefusesAMissingMirrorCredentialBeforeAnythingIsPushed(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		refType     string
		username    string
		wantRefusal bool
	}{
		{name: "a release tag with no publishing username", refType: "tag", username: "", wantRefusal: true},
		{name: "a release tag with the publishing username set", refType: "tag", username: "publisher"},
		{name: "the :latest path with no publishing username", refType: "branch", username: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runMirrorCredentialStep(t, testCase.refType, testCase.username)

			if testCase.wantRefusal {
				if err == nil {
					t.Fatalf("a release tag with an empty DOCKERHUB_PUBLISH_USERNAME went on to publish.\n%s", output)
				}
				if !strings.Contains(output, "DOCKERHUB_PUBLISH_USERNAME is empty on a release tag") {
					t.Fatalf("the refusal does not name the missing credential.\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the step refused a run it owes: %v\n%s", err, output)
			}
		})
	}
}

// TestTheMirrorCredentialRefusalPrecedesThePushAndCarriesNoRunnerSideSkip pins
// the two facts a green script run cannot show: the step runs before the first
// registry write, and no `if:` or `continue-on-error:` lets the runner pass or
// skip it.
func TestTheMirrorCredentialRefusalPrecedesThePushAndCarriesNoRunnerSideSkip(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	steps := stepNames(t, job)

	at, pushAt := slices.Index(steps, mirrorCredentialStep), slices.Index(steps, pushStep)
	if at < 0 || pushAt < 0 || at > pushAt {
		t.Fatalf("%s, job %q: %q must run before %q (found at %d and %d), or a refused mirror leaves a pushed digest behind",
			publishWorkflow, publishJob, mirrorCredentialStep, pushStep, at, pushAt)
	}
	if found := workflowfile.StepFailOpenKeys(stepBlock(t, job, mirrorCredentialStep)); len(found) > 0 {
		t.Errorf("%s, step %q carries %q: the runner could pass or skip the refusal", publishWorkflow, mirrorCredentialStep, found)
	}
}
