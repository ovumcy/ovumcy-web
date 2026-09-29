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

// The platforms the release must ship scanned, by name. Read off the job
// rather than restated would let a platform dropped from the job's list drop
// out of this check with it.
var requiredPlatforms = []string{"linux/amd64", "linux/arm64"}

const (
	platformsKey  = "IMAGE_PLATFORMS"
	scannedRefEnv = "IMAGE_REF: ${{ steps.image.outputs.name }}@${{ steps.build.outputs.digest }}"
	stubTrivy     = "stub/trivy@sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

var (
	platformEntry = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+(/[a-z0-9]+)?$`)
	// A step-level `env:` entry for the list would narrow what one step reads
	// below what the job declares.
	platformsOverride = regexp.MustCompile(`(?m)^\s+` + platformsKey + `:`)
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
	for _, platform := range declared {
		if !platformEntry.MatchString(platform) {
			t.Errorf("%s, job %q: %s entry %q is not an os/arch[/variant] platform; the scan step refuses it, but only once a release is already pushed",
				publishWorkflow, publishJob, platformsKey, platform)
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
		block := withoutComments(stepBlock(t, job, name))
		if strings.Contains(block, "load: true") {
			t.Errorf("%s, job %q: step %q loads an image locally. The only image this job may judge is the pushed digest",
				publishWorkflow, publishJob, name)
		}
		if platformsOverride.MatchString(block) {
			t.Errorf("%s, job %q: step %q declares its own %s. The push and the scan must read the job's one list, or the scan can walk fewer platforms than the push built",
				publishWorkflow, publishJob, name, platformsKey)
		}
	}
}

// TestNoKeyLetsTheScanOrWhatFollowsItFailOpen covers what running the scan's
// script cannot see: the runner, not bash, honours `continue-on-error:` and
// `if:`. `continue-on-error:` on the scan, or `if: always()` on any step from
// the push through the promotion, lets a digest the scan refused be signed,
// attested and tagged while the order tests, which read step names, and the
// script cases stay green. The window runs on to the anonymous read-back of
// the public tags: skipped or non-blocking, it lets a tag that does not
// resolve to the signed digest stand under a green job.
func TestNoKeyLetsTheScanOrWhatFollowsItFailOpen(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	if found := workflowfile.JobFailOpenKeys(job); len(found) > 0 {
		t.Errorf("%s, job %q carries %q: a refused scan would no longer fail the release", publishWorkflow, publishJob, found)
	}

	steps := stepNames(t, job)
	from, through := slices.Index(steps, pushStep), slices.Index(steps, publicStep)
	if from < 0 || through < from || !slices.Contains(steps[from:through+1], scanStep) || !slices.Contains(steps[from:through+1], promoteStep) {
		t.Fatalf("%s, job %q: no %q … %q … %q … %q window in %v", publishWorkflow, publishJob, pushStep, scanStep, promoteStep, publicStep, steps)
	}
	for _, name := range steps[from : through+1] {
		if found := workflowfile.StepFailOpenKeys(stepBlock(t, job, name)); len(found) > 0 {
			t.Errorf("%s, job %q, step %q carries %q: a refused scan or a mismatched public tag would no longer fail the release", publishWorkflow, publishJob, name, found)
		}
	}
}

// mirrorEnabledIf is the one `if:` the Docker Hub mirror steps may carry: the
// switch that turns the mirror on. Anything else on them, or any
// `continue-on-error:`, lets the runner pass a mirror that was never signed or
// never read back.
const mirrorEnabledIf = "if: ${{ env.DOCKERHUB_PUBLISH_USERNAME != '' }}"

// TestNoKeyLetsTheDockerHubMirrorFailOpen holds the mirror's login, copy and
// read-back to the runner's own keys, which running their scripts cannot see.
func TestNoKeyLetsTheDockerHubMirrorFailOpen(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	for _, name := range []string{mirrorLoginStep, mirrorProbeStep, mirrorStep, mirrorVerifyStep} {
		found := workflowfile.StepFailOpenKeys(stepBlock(t, job, name))
		if !slices.Equal(found, []string{mirrorEnabledIf}) {
			t.Errorf("%s, job %q, step %q carries %q, want only %q: a mirror the runner passes after a failure is published unsigned or unverified",
				publishWorkflow, publishJob, name, found, mirrorEnabledIf)
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

	// The scan walks the job's list in order and stops at the first finding,
	// so what a failing platform leaves scanned is read off that same list.
	declared := strings.Split(jobEnv(job)[platformsKey], ",")
	scannedThrough := func(platform string) []string {
		index := slices.Index(declared, platform)
		if index < 0 {
			t.Fatalf("%s is %q, which lacks %s", platformsKey, jobEnv(job)[platformsKey], platform)
		}
		return declared[:index+1]
	}

	for _, testCase := range []struct {
		name         string
		ref          string
		platforms    *string
		noLogin      bool
		dockerConfig bool
		failPlatform string
		wantScanned  []string
		wantRefusal  string
	}{
		{name: "every platform clean", ref: pushed, wantScanned: declared},
		{name: "arm64 carries a finding", ref: pushed, failPlatform: "linux/arm64", wantScanned: scannedThrough("linux/arm64"), wantRefusal: "exit"},
		{name: "amd64 carries a finding", ref: pushed, failPlatform: "linux/amd64", wantScanned: scannedThrough("linux/amd64"), wantRefusal: "exit"},
		{name: "the push reported no digest", ref: imageName + "@", wantRefusal: "reported no digest"},
		{name: "a tag instead of a digest", ref: imageName + ":latest", wantRefusal: "reported no digest"},
		{name: "no platform declared", ref: pushed, platforms: new(string), wantRefusal: "names no platform"},
		{name: "an empty platform entry", ref: pushed, platforms: new("linux/amd64,,linux/arm64"), wantRefusal: "not a comma-separated list"},
		{name: "a trailing comma", ref: pushed, platforms: new("linux/amd64,linux/arm64,"), wantRefusal: "not a comma-separated list"},
		{name: "a malformed platform entry", ref: pushed, platforms: new("linux/amd64, linux/arm64"), wantRefusal: "not a comma-separated list"},
		{name: "no registry login", ref: pushed, noLogin: true, wantRefusal: "no registry login"},
		{name: "the login is where DOCKER_CONFIG points", ref: pushed, dockerConfig: true, wantScanned: declared},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runnerTemp := filepath.ToSlash(t.TempDir())
			home := filepath.ToSlash(t.TempDir())
			configDir, dockerConfigEnv := home+"/.docker", ""
			if testCase.dockerConfig {
				configDir = filepath.ToSlash(t.TempDir())
				dockerConfigEnv = configDir
			}
			if !testCase.noLogin {
				if err := os.MkdirAll(configDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configDir+"/config.json", []byte(`{"auths":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			preamble := strings.Join([]string{
				`docker() {`,
				`  echo "DOCKER-RUN $*"`,
				`  case " $* " in *" --platform ${STUB_FAIL_PLATFORM:-none} "*) return 1 ;; esac`,
				`}`,
				"",
			}, "\n")
			command := runBashScript(t, bash, job, scanStep, preamble+stepScript(t, job, scanStep))
			command.Env = append(os.Environ(), "HOME="+home, "DOCKER_CONFIG="+dockerConfigEnv, "RUNNER_TEMP="+runnerTemp, "TRIVY_IMAGE="+stubTrivy)
			for key, value := range jobEnv(job) {
				command.Env = append(command.Env, key+"="+value)
			}
			if testCase.platforms != nil {
				command.Env = append(command.Env, platformsKey+"="+*testCase.platforms)
			}
			command.Env = append(command.Env, "IMAGE_REF="+testCase.ref, "STUB_FAIL_PLATFORM="+testCase.failPlatform)

			out, err := command.CombinedOutput()
			output := string(out)

			// Git Bash rewrites HOME to its /d/… form, so the login mount is
			// matched without the drive; the tail still names this case's dir.
			configTail := strings.TrimPrefix(configDir, filepath.VolumeName(configDir))
			mounts := []string{
				configTail + "/config.json:/root/.docker/config.json:ro ",
				" -v " + runnerTemp + "/trivy-cache:/root/.cache/trivy ",
			}
			var scanned []string
			for _, line := range strings.Split(output, "\n") {
				args, ok := strings.CutPrefix(strings.TrimSpace(line), "DOCKER-RUN ")
				if !ok {
					continue
				}
				scanned = append(scanned, requireTrivyScan(t, strings.Fields(args), testCase.ref, mounts))
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
// read from the registry with the job's login rather than a local daemon, from
// the one cache every platform's run shares — and returns its platform.
func requireTrivyScan(t *testing.T, args []string, ref string, mounts []string) string {
	t.Helper()

	joined := " " + strings.Join(args, " ") + " "
	want := []string{
		" run --rm ",
		" " + stubTrivy + " image ",
		" --image-src remote ",
		" --scanners vuln ",
		" --severity HIGH,CRITICAL ",
		" --exit-code 1 ",
	}
	for _, want := range append(want, mounts...) {
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
