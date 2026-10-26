package publishorder

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The decision step answers "is this tag the highest stable release" before a
// build and a scan, and tag runs do not queue behind one another (a pending run
// in a shared concurrency group is cancelled, which a release must never be).
// So the answer is asked again immediately before each write of `latest` this
// workflow makes: the promotion on GHCR and the alias loop of the Docker Hub
// mirror. This file holds those two re-checks and the flavor line that keeps
// the metadata action from adding the alias behind the decision's back.

// TestTheMetadataStepDoesNotAddLatestOnItsOwn holds the line the decision step
// depends on. The metadata action's default flavor adds `latest` to every tag
// event by itself, whatever the raw line says, so a pre-release or a backport
// tag would move the alias that the decision step refused it. The step must say
// `latest=false` and leave the one raw line as the only way in.
func TestTheMetadataStepDoesNotAddLatestOnItsOwn(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	metaBlock := withoutComments(stepBlock(t, job, metadataStep))

	if !strings.Contains(metaBlock, "\n          flavor: latest=false\n") {
		t.Errorf("%s, step %q sets no `flavor: latest=false` under `with:`, so the action's default `latest=auto` adds the alias on every tag event", publishWorkflow, metadataStep)
	}
}

// promoteTagRefs is the list a release tag's metadata step derives when the
// decision said `latest` is this run's.
const promoteTagRefs = imageName + ":v2.0.0\n" + imageName + ":latest\n" + imageName + ":sha-5049126"

// TestTheGhcrPromotionAsksAgainBeforeItWritesLatest runs the promotion with the
// tag listing changed under it. A higher stable tag that appeared since the
// decision must keep `latest`; the run still publishes everything else it owes
// and says so in an output the checks that follow read.
func TestTheGhcrPromotionAsksAgainBeforeItWritesLatest(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)

	for _, testCase := range []struct {
		name        string
		tags        string
		ghRefs      string
		ghFails     bool
		wantWrites  []string
		wantDropped string
		wantAsked   bool
		wantError   string
	}{
		{name: "still the highest stable tag", tags: promoteTagRefs, ghRefs: refsOf("v1.9.2", "v2.0.0"), wantWrites: []string{"v2.0.0", "latest", "sha-5049126"}, wantDropped: "false", wantAsked: true},
		{name: "a higher stable tag appeared during the run", tags: promoteTagRefs, ghRefs: refsOf("v2.0.0", "v2.1.0"), wantWrites: []string{"v2.0.0", "sha-5049126"}, wantDropped: "true", wantAsked: true},
		{name: "a higher tag whose text sorts lower", tags: promoteTagRefs, ghRefs: refsOf("v2.0.0", "v10.0.0"), wantWrites: []string{"v2.0.0", "sha-5049126"}, wantDropped: "true", wantAsked: true},
		{name: "a higher pre-release does not take it", tags: promoteTagRefs, ghRefs: refsOf("v2.0.0", "v3.0.0-rc.1"), wantWrites: []string{"v2.0.0", "latest", "sha-5049126"}, wantDropped: "false", wantAsked: true},
		{name: "a listing that cannot be read", tags: promoteTagRefs, ghFails: true, wantAsked: true, wantError: "could not be listed"},
		{name: "a run that carries no latest never asks", tags: imageName + ":v2.0.0\n" + imageName + ":sha-5049126", ghRefs: refsOf("v2.0.0", "v9.0.0"), wantWrites: []string{"v2.0.0", "sha-5049126"}, wantDropped: "false"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outputs := filepath.ToSlash(filepath.Join(t.TempDir(), "github_output"))
			env := map[string]string{
				"DIGEST": digest, "TAG_REFS": testCase.tags, "IMAGE_NAME": imageName, "IMAGE_PATH": imagePath,
				"REGISTRY_USER": "github-actions", "REGISTRY_PASSWORD": "stub",
				"GITHUB_OUTPUT": outputs, "GH_REFS": testCase.ghRefs,
			}
			if testCase.ghFails {
				env["GH_FAIL"] = "1"
			}
			output, writes, err := runStep(t, bash, job, promoteStep, env, defaultRegistry())

			if asked := strings.Contains(output, "GH api --paginate repos/ovumcy/ovumcy-web/git/matching-refs/tags/v "); asked != testCase.wantAsked {
				t.Errorf("the step asked for the tag listing = %v, want %v:\n%s", asked, testCase.wantAsked, output)
			}
			if testCase.wantError != "" {
				if err == nil {
					t.Fatalf("the promotion went on without knowing whether its tag is still the highest.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantError)
				if len(writes) != 0 {
					t.Errorf("the promotion wrote %v before refusing, and a red run retracts no tag", writes)
				}
				return
			}
			if err != nil {
				t.Fatalf("the promotion failed: %v\n%s", err, output)
			}
			if !slices.Equal(writes, testCase.wantWrites) {
				t.Errorf("wrote tags %v, want %v\n%s", writes, testCase.wantWrites, output)
			}
			written, readErr := os.ReadFile(outputs)
			if readErr != nil {
				t.Fatalf("read the step output file: %v", readErr)
			}
			if want := "latest_dropped=" + testCase.wantDropped + "\n"; string(written) != want {
				t.Errorf("the step exported %q, want %q", written, want)
			}
		})
	}
}

// runMirrorStep runs the Docker Hub mirror step with cosign accepting every
// call and the tag listing supplied, and returns what it printed, what it
// exported and its exit error.
func runMirrorStep(t *testing.T, env map[string]string, ghRefs string, ghFails bool) (string, string, error) {
	t.Helper()

	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	preamble := `gh() { printf 'GH %s\n' "$*" >&2; if [ -n "${GH_FAIL:-}" ]; then return 1; fi; printf '%s\n' "$GH_REFS"; }` + "\n" +
		`sleep() { :; }` + "\n" +
		highestTagExports + "\n" +
		`cosign() { printf 'COSIGN %s\n' "$*" >&2; }`

	outputs := filepath.ToSlash(filepath.Join(t.TempDir(), "github_output"))
	command := runBashScript(t, bash, job, mirrorStep, preamble+"\n"+stepScript(t, job, mirrorStep))
	command.Env = append(os.Environ(),
		"DIGEST="+digest, "IMAGE_NAME="+imageName, "MIRROR_NAME="+mirrorName, "IDENTITY_REGEXP="+identityRegexp,
		"GITHUB_REPOSITORY=ovumcy/ovumcy-web", "GITHUB_OUTPUT="+outputs, "REF_TYPE=tag", "REF_NAME=v2.0.0",
		"VERIFY_ATTEMPTS=1", "VERIFY_DELAY_SECONDS=0", "RATE_LIMIT_ATTEMPTS=1", "RATE_LIMIT_DELAY_SECONDS=0",
		"GH_REFS="+ghRefs,
	)
	if ghFails {
		command.Env = append(command.Env, "GH_FAIL=1")
	}
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	written, _ := os.ReadFile(outputs)
	return string(output), string(written), err
}

// TestTheMirrorAsksAgainBeforeItWritesLatest is the same question at the second
// registry's write. The signing and the read-back before the alias loop can
// wait minutes on a rate limit, so the answer is asked again right before the
// first alias, and a `latest` the promotion already dropped is not asked about.
func TestTheMirrorAsksAgainBeforeItWritesLatest(t *testing.T) {
	versionCopy := "COSIGN copy --force " + imageName + "@" + digest + " " + mirrorName + ":v2.0.0\n"
	latestCopy := "COSIGN copy --force " + imageName + "@" + digest + " " + mirrorName + ":latest\n"

	for _, testCase := range []struct {
		name        string
		env         map[string]string
		ghRefs      string
		ghFails     bool
		wantLatest  bool
		wantVersion bool
		wantAsked   bool
		wantDropped string
		wantRefusal string
	}{
		{name: "still the highest stable tag", ghRefs: refsOf("v1.9.2", "v2.0.0"), wantLatest: true, wantVersion: true, wantAsked: true, wantDropped: "false"},
		{name: "a higher stable tag appeared while the mirror was signed", ghRefs: refsOf("v2.0.0", "v2.0.1"), wantVersion: true, wantAsked: true, wantDropped: "true"},
		{name: "the promotion already left latest to a higher tag", env: map[string]string{"PROMOTE_LATEST_DROPPED": "true"}, ghRefs: refsOf("v2.0.0"), wantVersion: true, wantDropped: "true"},
		{name: "a listing that cannot be read", ghFails: true, wantAsked: true, wantRefusal: "could not be listed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{"TAG_REFS": imageName + ":v2.0.0\n" + imageName + ":latest"}
			for key, value := range testCase.env {
				env[key] = value
			}
			output, written, err := runMirrorStep(t, env, testCase.ghRefs, testCase.ghFails)

			if asked := strings.Contains(output, "GH api --paginate repos/ovumcy/ovumcy-web/git/matching-refs/tags/v "); asked != testCase.wantAsked {
				t.Errorf("the step asked for the tag listing = %v, want %v:\n%s", asked, testCase.wantAsked, output)
			}
			if testCase.wantRefusal != "" {
				if err == nil {
					t.Fatalf("the mirror went on without knowing whether its tag is still the highest.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
				if strings.Contains(output, versionCopy) || strings.Contains(output, latestCopy) {
					t.Errorf("the mirror wrote an alias before refusing, and a red run retracts no tag:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("the mirror failed: %v\n%s", err, output)
			}
			if got := strings.Contains(output, latestCopy); got != testCase.wantLatest {
				t.Errorf("the mirror copied latest = %v, want %v:\n%s", got, testCase.wantLatest, output)
			}
			if got := strings.Contains(output, versionCopy); got != testCase.wantVersion {
				t.Errorf("the mirror copied the version tag = %v, want %v:\n%s", got, testCase.wantVersion, output)
			}
			if want := "latest_dropped=" + testCase.wantDropped + "\n"; written != want {
				t.Errorf("the step exported %q, want %q", written, want)
			}
		})
	}
}

// TestTheChecksStopAskingAboutALatestTheRunDropped: when a step left `latest` to
// a higher release, the anonymous checks that follow must not demand it; and
// without that word they still demand it, so a lost write is not waved through.
func TestTheChecksStopAskingAboutALatestTheRunDropped(t *testing.T) {
	job := workflowfile.Job(t, publishWorkflow, publishJob)
	bash := requireBash(t)
	refs := imageName + ":v2.0.0\n" + imageName + ":latest"

	for _, step := range []string{publicStep, mirrorVerifyStep} {
		for _, testCase := range []struct {
			name        string
			dropped     string
			wantRefusal string
		}{
			{name: "latest was dropped and points elsewhere", dropped: "true"},
			{name: "latest was not dropped and points elsewhere", dropped: "false", wantRefusal: "not to the signed digest"},
			{name: "the word never arrived", dropped: "", wantRefusal: "not to the signed digest"},
		} {
			t.Run(step+": "+testCase.name, func(t *testing.T) {
				reg := defaultRegistry()
				reg.resolves = map[string]string{"v2.0.0": canonicalManifest, "latest": otherManifest}
				output, _, err := runStep(t, bash, job, step, map[string]string{
					"DIGEST": digest, "TAG_REFS": refs, "IMAGE_NAME": imageName, "IMAGE_PATH": imagePath,
					"MIRROR_NAME": mirrorName, "LATEST_DROPPED": testCase.dropped,
				}, reg)

				if testCase.wantRefusal == "" {
					if err != nil {
						t.Fatalf("the check refused a run that left latest alone: %v\n%s", err, output)
					}
					if !strings.Contains(output, "v2.0.0 is anonymously pullable") {
						t.Errorf("the check skipped the version tag too:\n%s", output)
					}
					return
				}
				if err == nil {
					t.Fatalf("the check passed a latest that is not the signed digest.\n%s", output)
				}
				requireRefusalReason(t, output, testCase.wantRefusal)
			})
		}
	}
}
