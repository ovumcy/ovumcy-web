package publishorder

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The platforms the release must ship scanned, by name. Read off the job
// rather than restated would let a platform dropped from the job's list drop
// out of this check with it.
var requiredPlatforms = []string{"linux/amd64", "linux/arm64"}

const (
	platformsKey  = "IMAGE_PLATFORMS"
	scannedRefEnv = "IMAGE_REF: ${{ steps.image.outputs.name }}@${{ steps.build.outputs.digest }}"
	stubTrivy     = "stub/trivy@sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// TestTheScanCoversEveryPlatformThePushBuilds holds the scan to the bytes the
// signature covers. It used to judge a separate `load: true` rebuild of
// linux/amd64 alone: bytes that shared a GHA cache with the push, which is no
// evidence about the pushed digest, and no arm64 at all.
func TestTheScanCoversEveryPlatformThePushBuilds(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)

	declared := strings.Split(jobEnv(job)[platformsKey], ",")
	for _, platform := range requiredPlatforms {
		if !slices.Contains(declared, platform) {
			t.Errorf("%s, job %q: %s is %q, which lacks %s — a platform the release ships unscanned or does not ship at all",
				publishWorkflow, publishJob, platformsKey, jobEnv(job)[platformsKey], platform)
		}
	}

	push := withoutComments(stepBlock(t, job, pushStep))
	if !strings.Contains(push, "\n          platforms: ${{ env."+platformsKey+" }}\n") {
		t.Errorf("%s, step %q does not build `platforms: ${{ env.%s }}`. The scan walks that list; a push building from any other one can publish a platform nothing scanned",
			publishWorkflow, pushStep, platformsKey)
	}

	scan := withoutComments(stepBlock(t, job, scanStep))
	if !strings.Contains(scan, scannedRefEnv) {
		t.Errorf("%s, step %q does not declare `%s`. The signature covers that digest, and a scan of anything else is not a verdict on it",
			publishWorkflow, scanStep, scannedRefEnv)
	}

	for _, name := range stepNames(t, job) {
		if strings.Contains(withoutComments(stepBlock(t, job, name)), "load: true") {
			t.Errorf("%s, job %q: step %q loads an image locally. The only image this job may judge is the pushed digest",
				publishWorkflow, publishJob, name)
		}
	}
}

// TestTheScanRunsTrivyOnEveryPlatformOfThePushedDigest runs the step's real
// script with `docker` shadowed, so what is proven is which scans it asks for
// and how it treats their verdicts — not Trivy's own platform resolution,
// which answers `no child with platform` for a platform the index lacks.
func TestTheScanRunsTrivyOnEveryPlatformOfThePushedDigest(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	pushed := imageName + "@" + digest

	for _, testCase := range []struct {
		name         string
		ref          string
		platforms    *string
		failPlatform string
		wantScanned  []string
		wantRefusal  string
	}{
		{name: "every platform clean", ref: pushed, wantScanned: requiredPlatforms},
		{name: "arm64 carries a finding", ref: pushed, failPlatform: "linux/arm64", wantScanned: requiredPlatforms, wantRefusal: "exit"},
		{name: "amd64 carries a finding", ref: pushed, failPlatform: "linux/amd64", wantScanned: requiredPlatforms[:1], wantRefusal: "exit"},
		{name: "the push reported no digest", ref: imageName + "@", wantRefusal: "reported no digest"},
		{name: "a tag instead of a digest", ref: imageName + ":latest", wantRefusal: "reported no digest"},
		{name: "no platform declared", ref: pushed, platforms: new(string), wantRefusal: "names no platform"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			preamble := strings.Join([]string{
				`docker() {`,
				`  echo "DOCKER-RUN $*"`,
				`  case " $* " in *" --platform ${STUB_FAIL_PLATFORM:-none} "*) return 1 ;; esac`,
				`}`,
				"",
			}, "\n")
			command := runBashScript(t, bash, job, scanStep, preamble+stepScript(t, job, scanStep))
			command.Env = append(os.Environ(), "HOME=/stub-home", "TRIVY_IMAGE="+stubTrivy)
			for key, value := range jobEnv(job) {
				command.Env = append(command.Env, key+"="+value)
			}
			if testCase.platforms != nil {
				command.Env = append(command.Env, platformsKey+"="+*testCase.platforms)
			}
			command.Env = append(command.Env, "IMAGE_REF="+testCase.ref, "STUB_FAIL_PLATFORM="+testCase.failPlatform)

			out, err := command.CombinedOutput()
			output := string(out)

			var scanned []string
			for _, line := range strings.Split(output, "\n") {
				args, ok := strings.CutPrefix(strings.TrimSpace(line), "DOCKER-RUN ")
				if !ok {
					continue
				}
				scanned = append(scanned, requireTrivyScan(t, strings.Fields(args), testCase.ref))
			}

			if !slices.Equal(scanned, testCase.wantScanned) {
				t.Errorf("the step scanned platforms %v, want %v:\n%s", scanned, testCase.wantScanned, output)
			}
			switch {
			case testCase.wantRefusal == "" && err != nil:
				t.Fatalf("the step refused a digest every platform of which scanned clean: %v\n%s", err, output)
			case testCase.wantRefusal != "" && err == nil:
				t.Fatalf("the step passed where it owes a refusal (%s):\n%s", testCase.wantRefusal, output)
			case testCase.wantRefusal != "" && testCase.wantRefusal != "exit" && !strings.Contains(output, testCase.wantRefusal):
				t.Fatalf("the step failed, but not with its own refusal %q — a harness failure is not a verdict:\n%s", testCase.wantRefusal, output)
			}
		})
	}
}

// requireTrivyScan checks one `docker` call is the gate's scan of ref — the
// same scanner, threshold and exit code as the required `trivy-image` check,
// read from the registry rather than a local daemon — and returns its platform.
func requireTrivyScan(t *testing.T, args []string, ref string) string {
	t.Helper()

	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{
		" run --rm ",
		" " + stubTrivy + " image ",
		" --image-src remote ",
		" --scanners vuln ",
		" --severity HIGH,CRITICAL ",
		" --exit-code 1 ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("a scan was run without %q: %v", strings.TrimSpace(want), args)
		}
	}
	if strings.Contains(joined, "docker.sock") {
		t.Errorf("a scan was handed the Docker socket; it reads the registry, and the socket is root on the runner: %v", args)
	}
	if got := args[len(args)-1]; got != ref {
		t.Errorf("a scan judged %q, not the pushed %q: %v", got, ref, args)
	}

	platform := slices.Index(args, "--platform")
	if platform < 0 || platform+1 >= len(args) {
		t.Fatalf("a scan names no --platform, so Trivy picks one for itself: %v", args)
	}
	return args[platform+1]
}
