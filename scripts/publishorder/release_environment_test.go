package publishorder

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// publishCredential matches a read of the Docker Hub publishing credential in
// any spelling of the secrets context.
var publishCredential = regexp.MustCompile(`secrets\s*\.\s*DOCKERHUB_PUBLISH_|secrets\s*\[\s*['"]DOCKERHUB_PUBLISH_`)

var (
	jobEnvironmentRelease = regexp.MustCompile(`(?m)^    environment:\s*["']?release["']?\s*$`)
	jobCallsWorkflow      = regexp.MustCompile(`(?m)^    uses:\s*\S`)
)

// TestEveryJobReadingThePublishCredentialRunsInTheReleaseEnvironment walks
// every workflow the repository has and every job in it. A job that reads
// DOCKERHUB_PUBLISH_* must run in the `release` environment, so the
// credential, once it lives there, is readable only from the refs that
// environment's deployment policy admits rather than from a workflow pushed to
// any branch. A job that CALLS a reusable workflow is the one shape GitHub
// gives no `environment:` key; it passes the secret on, and the called job is
// itself in this walk.
func TestEveryJobReadingThePublishCredentialRunsInTheReleaseEnvironment(t *testing.T) {
	root := workflowRoot(t)
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list .github/workflows: %v (%d files)", err, len(files))
	}

	var judged []string
	for _, file := range files {
		workflow := ".github/workflows/" + filepath.Base(file)
		content := workflowfile.Read(t, workflow)
		if !publishCredential.MatchString(content) {
			continue
		}
		for _, header := range workflowfile.JobHeaders(t, workflow, content) {
			name := strings.TrimSuffix(strings.TrimSpace(header), ":")
			block := workflowfile.Job(t, workflow, name)
			if !publishCredential.MatchString(block) {
				continue
			}
			if jobCallsWorkflow.MatchString(block) {
				continue
			}
			judged = append(judged, workflow+":"+name)
			if !jobEnvironmentRelease.MatchString(block) {
				t.Errorf("%s, job %q reads the Docker Hub publishing credential and declares no `environment: release`. Without it the credential stays readable to any workflow on any branch once it is moved into the environment", workflow, name)
			}
		}
	}

	// Anti-vacuity, by name: the two jobs that hold the credential today must
	// be among the ones judged, whatever else the walk finds.
	for _, want := range []string{".github/workflows/docker-image.yml:publish", ".github/workflows/promote-latest.yml:promote"} {
		if !slices.Contains(judged, want) {
			t.Errorf("the walk did not judge %s, which reads the publishing credential; judged %v", want, judged)
		}
	}
}

// workflowRoot finds the module root the way the workflow reader does: the
// nearest directory above the test holding go.mod.
func workflowRoot(t *testing.T) string {
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
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
