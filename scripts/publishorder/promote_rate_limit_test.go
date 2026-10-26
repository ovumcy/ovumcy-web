package publishorder

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// Docker Hub refuses with a rate limit now and then (runs 36512010639 and
// 36512597460 of docker-image.yml measured it). Every Docker Hub call the
// promotion makes asks again on that one answer, within a bounded budget, and
// on nothing else: any other refusal ends the step on its first attempt.

// TestTheDockerHubWriteAsksAgainOnlyOnARateLimit runs the Docker Hub write with
// 429s injected at each of its calls. A write that gave up on the first 429
// would leave the mirror unmoved for a limit that lifts in seconds; one that
// asked again on every refusal would repeat a refusal that is an answer.
func TestTheDockerHubWriteAsksAgainOnlyOnARateLimit(t *testing.T) {
	const latestPut = "CURL PUT https://registry-1.docker.io/v2/" + imagePath + "/manifests/latest\n"

	for _, testCase := range []struct {
		name        string
		env         map[string]string
		wantPuts    int
		wantRefusal string
	}{
		{name: "a 429 on the write lifts", env: map[string]string{"STUB_RATE_LIMITS": "2", "STUB_RATE_URL": "/manifests/latest"}, wantPuts: 3},
		{name: "a 429 on the write that never lifts", env: map[string]string{"STUB_RATE_LIMITS": "99", "STUB_RATE_URL": "/manifests/latest"}, wantPuts: 3, wantRefusal: "returned HTTP 429. GHCR latest was not touched"},
		{name: "a 500 on the write is an answer", env: map[string]string{"STUB_PUT_STATUS": "500"}, wantPuts: 1, wantRefusal: "returned HTTP 500. GHCR latest was not touched"},
		{name: "a 429 on the push token lifts", env: map[string]string{"STUB_RATE_LIMITS": "1", "STUB_RATE_URL": "auth.docker.io/token"}, wantPuts: 1},
		{name: "a 429 on the read of the manifest lifts", env: map[string]string{"STUB_RATE_LIMITS": "1", "STUB_RATE_URL": "/manifests/sha256:"}, wantPuts: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{
				"DIGEST": digest, "IMAGE_PATH": imagePath, "MANIFEST_ACCEPT": "x", "STUB_BODY": canonicalManifest,
				"REGISTRY_USER": "publisher", "REGISTRY_PASSWORD": "secret", "TAG": "v1.9.2", "TAG_ON_HUB": "present",
				"RATE_LIMIT_ATTEMPTS": "3", "RATE_LIMIT_DELAY_SECONDS": "0",
				"STUB_COUNTER": filepath.ToSlash(filepath.Join(t.TempDir(), "counter")),
			}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, _, err := runWorkflowStep(t, promoteWorkflow, promoteJob, promoteHubMoveStep, putCurl, env)

			if got := strings.Count(output, latestPut); got != testCase.wantPuts {
				t.Errorf("the step wrote `latest` %d time(s), want %d:\n%s", got, testCase.wantPuts, output)
			}
			if testCase.wantRefusal == "" {
				if err != nil {
					t.Fatalf("the step failed on a limit that lifts: %v\n%s", err, output)
				}
				return
			}
			if err == nil {
				t.Fatalf("the step went on.\n%s", output)
			}
			requireRefusalReason(t, output, testCase.wantRefusal)
		})
	}
}

// TestEveryDockerHubCurlCallGoesThroughTheRateLimitWrapper enumerates the
// promotion's steps from the workflow: any step that talks to Docker Hub
// calls curl only through its `hub_curl`, so a Docker Hub call added later
// without the retry fails here. The report step is the one named exception: it
// runs only after a failure, never fails, and asks each registry once, because
// a report that waited out a limit would hold a failed run open.
func TestEveryDockerHubCurlCallGoesThroughTheRateLimitWrapper(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)
	rawCurl := regexp.MustCompile(`(^|[^_A-Za-z0-9])curl\s`)

	seen := map[string]bool{}
	for _, name := range promoteStepNames(t) {
		if name == promoteReportStep {
			continue
		}
		block := stepBlockOf(t, job, name)
		if !strings.Contains(block, "docker.io") {
			continue
		}
		seen[name] = true

		if !strings.Contains(block, "hub_curl() {") {
			t.Errorf("%s, step %q talks to Docker Hub and defines no `hub_curl`", promoteWorkflow, name)
		}
		// The wrapper's own call is the one raw curl a step may hold.
		body := strings.ReplaceAll(block, `status="$(curl "$@")" || return $?`, "")
		if rawCurl.MatchString(body) {
			t.Errorf("%s, step %q calls curl directly where it talks to Docker Hub; the call goes through `hub_curl`", promoteWorkflow, name)
		}
	}
	for _, name := range []string{promoteHubCopyStep, promoteHubVerifyStep, promoteHubMoveStep, promoteHubCheckStep} {
		if !seen[name] {
			t.Errorf("%s: the enumeration did not find step %q among the Docker Hub steps, so this guard judged less than it claims", promoteWorkflow, name)
		}
	}
}

// TestTheShippedRateLimitBudgetIsLiteralAndBounded reads the numbers the
// workflow carries, which every fixture above overrides. A step that retries
// on a rate limit declares both constants as literals: a budget of one is the
// one-shot call that lost to the limit, and an unbounded one runs to the job's
// timeout, which GitHub reports as `cancelled`.
func TestTheShippedRateLimitBudgetIsLiteralAndBounded(t *testing.T) {
	job := workflowfile.Job(t, promoteWorkflow, promoteJob)

	seen := map[string]bool{}
	for _, name := range promoteStepNames(t) {
		block := stepBlockOf(t, job, name)
		if !strings.Contains(block, "hub_curl() {") && !strings.Contains(block, "rate_limited() {") {
			continue
		}
		seen[name] = true

		env := envConstants(workflowfile.Step(t, promoteWorkflow, promoteJob, job, name), "\n        env:\n", "          ")
		attempts, attemptsErr := strconv.Atoi(env["RATE_LIMIT_ATTEMPTS"])
		if attemptsErr != nil || attempts < 2 || attempts > 10 {
			t.Errorf("%s, step %q declares RATE_LIMIT_ATTEMPTS = %q; it must be a literal number from 2 to 10", promoteWorkflow, name, env["RATE_LIMIT_ATTEMPTS"])
		}
		delay, delayErr := strconv.Atoi(env["RATE_LIMIT_DELAY_SECONDS"])
		if delayErr != nil || delay < 1 || delay > 120 {
			t.Errorf("%s, step %q declares RATE_LIMIT_DELAY_SECONDS = %q; it must be a literal number from 1 to 120", promoteWorkflow, name, env["RATE_LIMIT_DELAY_SECONDS"])
		}
	}
	for _, name := range []string{promoteHubCopyStep, promoteHubVerifyStep, promoteHubMoveStep, promoteHubCheckStep} {
		if !seen[name] {
			t.Errorf("%s: step %q retries nothing on a rate limit", promoteWorkflow, name)
		}
	}
}
